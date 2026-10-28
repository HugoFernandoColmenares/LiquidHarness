// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

// stream implements ports.Stream over one SSE response body.
type stream struct {
	body      io.ReadCloser
	decoder   *sseDecoder
	filter    markerFilter
	raw       strings.Builder
	reasoning strings.Builder
	calls     callAccumulator

	usage             ports.Usage
	finishReason      string
	reasoningStreamed bool
	sawDone           bool
	finalized         bool
	done              bool
	pending           []ports.StreamEvent

	closeOnce sync.Once
	closeErr  error
}

var _ ports.Stream = (*stream)(nil)

func newStream(body io.ReadCloser) *stream {
	return &stream{
		body:    body,
		decoder: newSSEDecoder(body),
	}
}

// Next returns the next event. It returns io.EOF after the final done event.
func (s *stream) Next(ctx context.Context) (ports.StreamEvent, error) {
	if len(s.pending) > 0 {
		return s.pop(), nil
	}
	if s.done {
		return ports.StreamEvent{}, io.EOF
	}

	for {
		if err := ctx.Err(); err != nil {
			return ports.StreamEvent{}, s.fail(core.Wrap(core.CodeCanceled, err, "model stream canceled"))
		}

		event, err := s.decoder.Next()
		if errors.Is(err, io.EOF) {
			if !s.sawDone && s.finishReason == "" {
				return ports.StreamEvent{}, s.fail(truncatedStream("connection closed before the stream finished"))
			}
			if err := s.finalize(); err != nil {
				return ports.StreamEvent{}, s.fail(err)
			}
			return s.nextPending()
		}
		if err != nil {
			return ports.StreamEvent{}, s.fail(core.Wrap(core.CodeModelUnavailable, err, "read model stream"))
		}

		if event.Data == "" {
			continue
		}
		if strings.TrimSpace(event.Data) == "[DONE]" {
			s.sawDone = true
			if err := s.finalize(); err != nil {
				return ports.StreamEvent{}, s.fail(err)
			}
			return s.nextPending()
		}

		var chunk chatChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return ports.StreamEvent{}, s.fail(core.Wrap(core.CodeModelUnavailable, err, "malformed SSE event"))
		}
		if chunk.Error != nil {
			return ports.StreamEvent{}, s.fail(
				core.E(core.CodeModelUnavailable, "model stream error: %s", chunk.Error.Message))
		}
		if chunk.Usage != nil {
			s.usage = toPortsUsage(chunk.Usage)
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			s.finishReason = *choice.FinishReason
		}

		delta := choice.Delta
		if delta.Content != "" {
			s.raw.WriteString(delta.Content)
			if visible := s.filter.Write(delta.Content); visible != "" {
				s.pending = append(s.pending, ports.StreamEvent{Type: ports.StreamText, Text: visible})
			}
		}
		if reasoning := firstNonEmpty(delta.ReasoningContent, delta.Reasoning); reasoning != "" {
			s.reasoning.WriteString(reasoning)
			s.reasoningStreamed = true
			s.pending = append(s.pending, ports.StreamEvent{Type: ports.StreamReasoning, Reasoning: reasoning})
		}
		for _, call := range delta.ToolCalls {
			s.calls.add(call)
		}

		if len(s.pending) > 0 {
			return s.pop(), nil
		}
	}
}

// Close releases the response body. It is safe to call more than once.
func (s *stream) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.body.Close()
	})
	return s.closeErr
}

func (s *stream) pop() ports.StreamEvent {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}

func (s *stream) nextPending() (ports.StreamEvent, error) {
	if len(s.pending) > 0 {
		return s.pop(), nil
	}
	return ports.StreamEvent{}, io.EOF
}

func (s *stream) finalize() error {
	if s.finalized {
		return nil
	}
	s.finalized = true

	rest, err := s.filter.Flush()
	if err != nil {
		return truncatedStream(err.Error())
	}
	if rest != "" {
		s.pending = append(s.pending, ports.StreamEvent{Type: ports.StreamText, Text: rest})
	}

	parsed, err := Parse(s.raw.String())
	if err != nil {
		return core.Wrap(core.CodeModelUnavailable, err, "parse model response")
	}

	reasoning := parsed.Reasoning
	if reasoning == "" {
		reasoning = s.reasoning.String()
	}
	if reasoning != "" && !s.reasoningStreamed {
		s.pending = append(s.pending, ports.StreamEvent{Type: ports.StreamReasoning, Reasoning: reasoning})
	}

	calls, err := s.calls.finish()
	if err != nil {
		return core.Wrap(core.CodeModelUnavailable, err, "parse streamed tool calls")
	}
	if len(calls) == 0 {
		calls = parsed.ToolCalls
	}
	if len(calls) > MaxToolCalls {
		return core.E(core.CodeModelUnavailable,
			"model returned %d tool calls; the limit is %d", len(calls), MaxToolCalls)
	}
	if len(calls) > 0 {
		s.pending = append(s.pending, ports.StreamEvent{Type: ports.StreamToolCalls, ToolCalls: calls})
	}

	finish := s.finishReason
	if finish == "" {
		finish = "stop"
	}
	usage := s.usage
	s.pending = append(s.pending, ports.StreamEvent{Type: ports.StreamDone, FinishReason: finish, Usage: &usage})
	s.done = true
	return nil
}

func (s *stream) fail(err error) error {
	s.done = true
	_ = s.Close()

	var coded *core.Error
	if errors.As(err, &coded) {
		return err
	}
	return core.Wrap(core.CodeModelUnavailable, err, "model stream failed")
}

func truncatedStream(reason string) *core.Error {
	coded := core.E(core.CodeModelUnavailable, "model stream truncated: %s", reason)
	coded.Retry = true
	return coded
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type callAccumulator struct {
	order []int
	calls map[int]*callAccum
}

type callAccum struct {
	id   string
	name string
	args strings.Builder
}

func (a *callAccumulator) add(call wireToolCall) {
	if a.calls == nil {
		a.calls = make(map[int]*callAccum)
	}
	acc, ok := a.calls[call.Index]
	if !ok {
		acc = &callAccum{}
		a.calls[call.Index] = acc
		a.order = append(a.order, call.Index)
	}

	if call.ID != "" {
		acc.id = call.ID
	}
	if name := call.Function.Name; name != "" {
		switch {
		case acc.name == "":
			acc.name = name
		case !strings.HasSuffix(acc.name, name):
			acc.name += name
		}
	}
	acc.args.WriteString(string(call.Function.Arguments))
}

func (a *callAccumulator) finish() ([]core.ToolCall, error) {
	if len(a.calls) == 0 {
		return nil, nil
	}

	out := make([]core.ToolCall, 0, len(a.calls))
	for _, index := range a.order {
		acc := a.calls[index]
		name := strings.TrimSpace(acc.name)
		if name == "" {
			return nil, fmt.Errorf("streamed tool call %d has no name", index)
		}
		if !toolNamePattern.MatchString(name) {
			return nil, fmt.Errorf("streamed tool call %d has an invalid name %q", index, name)
		}

		arguments, err := normalizeArguments(acc.args.String())
		if err != nil {
			return nil, fmt.Errorf("streamed tool call %d (%s): %w", index, name, err)
		}

		id := acc.id
		if id == "" {
			id = core.NewID()
		}
		out = append(out, core.ToolCall{
			ID:        id,
			Name:      name,
			Arguments: arguments,
			Raw:       acc.args.String(),
		})
	}
	return out, nil
}

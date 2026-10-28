// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"context"
	"io"
	"strings"
	"sync"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

// Fake is a deterministic model for tests and pull-request evals. It replays a fixed list of
// responses in order, so a test never depends on a real backend or on sampling.
type Fake struct {
	model   string
	replies []string

	mu   sync.Mutex
	next int
}

var (
	_ ports.Model   = (*Fake)(nil)
	_ ports.Catalog = (*Fake)(nil)
)

// NewFake builds a fake model. With no replies it answers "OK".
func NewFake(model string, replies ...string) *Fake {
	if strings.TrimSpace(model) == "" {
		model = "fake"
	}
	if len(replies) == 0 {
		replies = []string{"OK"}
	}
	return &Fake{model: model, replies: replies}
}

// Name returns the fake model identifier.
func (f *Fake) Name() string { return f.model }

// Chat returns the next scripted reply.
func (f *Fake) Chat(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return ports.ChatResponse{}, core.Wrap(core.CodeCanceled, err, "fake model canceled")
	}

	reply := f.reply()
	parsed, err := Parse(reply)
	if err != nil {
		return ports.ChatResponse{}, core.Wrap(core.CodeModelUnavailable, err, "parse fake response")
	}

	finish := "stop"
	if len(parsed.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	return ports.ChatResponse{
		Message: core.Message{
			Role:      core.RoleAssistant,
			Content:   parsed.Content,
			Reasoning: parsed.Reasoning,
			ToolCalls: parsed.ToolCalls,
		},
		Model:        f.model,
		FinishReason: finish,
		Usage:        estimateUsage(req, reply),
	}, nil
}

// Stream replays the next scripted reply as small text deltas, followed by the tool calls and
// the done event. It is deterministic: the same reply always produces the same event sequence.
func (f *Fake) Stream(ctx context.Context, req ports.ChatRequest) (ports.Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, core.Wrap(core.CodeCanceled, err, "fake model canceled")
	}

	reply := f.reply()
	parsed, err := Parse(reply)
	if err != nil {
		return nil, core.Wrap(core.CodeModelUnavailable, err, "parse fake response")
	}

	finish := "stop"
	if len(parsed.ToolCalls) > 0 {
		finish = "tool_calls"
	}

	events := make([]ports.StreamEvent, 0, 4)
	if parsed.Content != "" {
		for _, chunk := range chunks(parsed.Content, 8) {
			events = append(events, ports.StreamEvent{Type: ports.StreamText, Text: chunk})
		}
	}
	if parsed.Reasoning != "" {
		events = append(events, ports.StreamEvent{Type: ports.StreamReasoning, Reasoning: parsed.Reasoning})
	}
	if len(parsed.ToolCalls) > 0 {
		events = append(events, ports.StreamEvent{Type: ports.StreamToolCalls, ToolCalls: parsed.ToolCalls})
	}
	usage := estimateUsage(req, reply)
	events = append(events, ports.StreamEvent{Type: ports.StreamDone, FinishReason: finish, Usage: &usage})

	return &fakeStream{events: events}, nil
}

// Models returns the single fake model.
func (f *Fake) Models(context.Context) ([]ports.ModelInfo, error) {
	return []ports.ModelInfo{{ID: f.model, OwnedBy: "liquidharness"}}, nil
}

func (f *Fake) reply() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	reply := f.replies[f.next%len(f.replies)]
	f.next++
	return reply
}

type fakeStream struct {
	events []ports.StreamEvent
	index  int
}

var _ ports.Stream = (*fakeStream)(nil)

func (s *fakeStream) Next(ctx context.Context) (ports.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return ports.StreamEvent{}, core.Wrap(core.CodeCanceled, err, "fake stream canceled")
	}
	if s.index >= len(s.events) {
		return ports.StreamEvent{}, io.EOF
	}
	event := s.events[s.index]
	s.index++
	return event, nil
}

func (s *fakeStream) Close() error { return nil }

func chunks(text string, size int) []string {
	if text == "" {
		return nil
	}
	runes := []rune(text)
	out := make([]string, 0, (len(runes)+size-1)/size)
	for start := 0; start < len(runes); start += size {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[start:end]))
	}
	return out
}

func estimateUsage(req ports.ChatRequest, reply string) ports.Usage {
	prompt := 0
	for _, message := range req.Messages {
		prompt += len([]rune(message.Content))/4 + 1
	}
	completion := len([]rune(reply))/4 + 1
	return ports.Usage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      prompt + completion,
	}
}

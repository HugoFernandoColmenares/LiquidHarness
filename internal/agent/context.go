// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"strings"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

const (
	defaultReserveTokens = 512
	minHistoryTokens     = 256
	safetyTokens         = 64
	softTrimHeadRatio    = 0.40
	softTrimTailRatio    = 0.40
	softTrimMinChars     = 240
	elidedToolResult     = "[tool result elided to fit the context window]"
)

// ContextConfig describes the budget split of one model call.
type ContextConfig struct {
	// Window is the total context window reported by the backend.
	Window int
	// ReserveOutput is the number of tokens kept for the answer. Zero means 512 or 10% of
	// Window, whichever is larger.
	ReserveOutput int
	// Tools are the specs sent with every request; their schemas consume context too.
	Tools []core.ToolSpec
}

// ContextManager builds the message list for each model call. It keeps the prefix stable
// (system prompt, then the compaction summary, then history) so llama-server can reuse its KV
// cache, and it only rewrites the parts that must shrink.
type ContextManager struct {
	cfg       ContextConfig
	compactor Compactor

	compacted []core.Message
	covered   int
}

// NewContextManager validates cfg and returns a manager. A nil compactor disables compaction:
// the manager then trims tool results and fails with CONTEXT_OVERFLOW when nothing else fits.
func NewContextManager(cfg ContextConfig, compactor Compactor) (*ContextManager, error) {
	if cfg.Window <= 0 {
		return nil, core.E(core.CodeConfigInvalid, "context window must be positive")
	}
	if cfg.ReserveOutput <= 0 {
		cfg.ReserveOutput = max(defaultReserveTokens, cfg.Window/10)
	}
	if cfg.ReserveOutput >= cfg.Window {
		return nil, core.E(core.CodeConfigInvalid,
			"output reserve (%d) must be smaller than the context window (%d)",
			cfg.ReserveOutput, cfg.Window)
	}
	return &ContextManager{cfg: cfg, compactor: compactor}, nil
}

// Build returns the messages for the next model call.
func (m *ContextManager) Build(ctx context.Context, turn *Turn) ([]core.Message, error) {
	messages := turn.Messages()
	if len(messages) == 0 {
		return nil, core.E(core.CodeInternal, "turn has no messages")
	}

	system := messages[0]
	history := messages[1:]

	available := m.cfg.Window - m.cfg.ReserveOutput -
		EstimateMessage(system) - estimateSpecs(m.cfg.Tools) - safetyTokens
	if available < minHistoryTokens {
		return nil, core.E(core.CodeContextOverflow,
			"context window of %d tokens leaves only %d for history", m.cfg.Window, available)
	}

	if m.compacted != nil && m.covered <= len(history) {
		history = append(append([]core.Message{}, m.compacted...), history[m.covered:]...)
	}

	if estimateMessages(history) <= available {
		return append([]core.Message{system}, history...), nil
	}

	history = trimToolResults(history, 2)
	if estimateMessages(history) <= available {
		return append([]core.Message{system}, history...), nil
	}

	history = elideToolResults(history)
	if estimateMessages(history) <= available {
		return append([]core.Message{system}, history...), nil
	}

	if m.compactor == nil {
		return nil, core.E(core.CodeContextOverflow,
			"history needs %d tokens but only %d are available and compaction is disabled",
			estimateMessages(history), available)
	}

	lastUser := lastUserIndex(history)
	if lastUser <= 0 {
		return nil, core.E(core.CodeContextOverflow,
			"history needs %d tokens but only %d are available and nothing can be compacted",
			estimateMessages(history), available)
	}

	compacted, err := m.compactor.Compact(ctx, history, available)
	if err != nil {
		return nil, core.Wrap(core.CodeContextOverflow, err, "compact conversation")
	}
	// Compact returns the summary plus the preserved tail, so it covers the whole history
	// seen so far; later messages are appended as usual.
	m.compacted = compacted
	m.covered = len(history)

	history = append([]core.Message{}, compacted...)
	history = trimToolResults(history, 2)
	if estimateMessages(history) > available {
		return nil, core.E(core.CodeContextOverflow,
			"history still needs %d tokens after compaction but only %d are available",
			estimateMessages(history), available)
	}
	return append([]core.Message{system}, history...), nil
}

// EstimateTokens approximates the token count of text. It is deliberately conservative: four
// bytes per token fits English and Spanish prose, and the backend's usage numbers remain the
// authoritative source for accounting.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + 3) / 4
}

// EstimateMessage approximates the tokens of one message, including tool calls.
func EstimateMessage(message core.Message) int {
	total := 4 + EstimateTokens(message.Content) + EstimateTokens(message.Reasoning)
	for _, call := range message.ToolCalls {
		total += 4 + EstimateTokens(call.Name) + EstimateTokens(string(call.Arguments))
	}
	return total
}

func estimateMessages(messages []core.Message) int {
	total := 0
	for _, message := range messages {
		total += EstimateMessage(message)
	}
	return total
}

func estimateSpecs(specs []core.ToolSpec) int {
	total := 0
	for _, spec := range specs {
		total += 4 + EstimateTokens(spec.Name) + EstimateTokens(spec.Description) +
			EstimateTokens(string(spec.InputSchema))
	}
	return total
}

func lastUserIndex(messages []core.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == core.RoleUser {
			return i
		}
	}
	return -1
}

// trimToolResults soft-trims old tool results: the newest keepLast results stay complete, the
// rest keep their head and tail so the model still sees the shape of the data.
func trimToolResults(messages []core.Message, keepLast int) []core.Message {
	out := append([]core.Message{}, messages...)
	seen := 0
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role != core.RoleTool {
			continue
		}
		seen++
		if seen <= keepLast || len(out[i].Content) <= softTrimMinChars {
			continue
		}
		out[i].Content = softTrim(out[i].Content)
	}
	return out
}

func softTrim(content string) string {
	head := int(float64(len(content)) * softTrimHeadRatio)
	tail := int(float64(len(content)) * softTrimTailRatio)
	if head+tail >= len(content) {
		return content
	}
	return content[:head] + "\n…\n" + content[len(content)-tail:]
}

// elideToolResults replaces every old tool result with a short marker.
func elideToolResults(messages []core.Message) []core.Message {
	out := append([]core.Message{}, messages...)
	seen := 0
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role != core.RoleTool {
			continue
		}
		seen++
		if seen <= 1 {
			continue
		}
		out[i].Content = elidedToolResult
	}
	return out
}

// summaryMessage builds the stable message that replaces the compacted prefix.
func summaryMessage(lines []string) core.Message {
	content := "Summary of earlier conversation (extractive, no model call):\n" +
		strings.Join(lines, "\n")
	return core.Message{Role: core.RoleSystem, Content: content}
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

const (
	summaryUserRunes      = 200
	summaryAssistantRunes = 200
	summaryToolRunes      = 120
	minSummaryLines       = 3
)

// Compactor replaces the part of the history that precedes the current user message with a
// deterministic summary. It never calls the model, so compaction cannot fail because of a
// backend problem and cannot itself overflow the context.
type Compactor interface {
	Compact(ctx context.Context, messages []core.Message, targetTokens int) ([]core.Message, error)
}

// ExtractiveCompactor keeps the current user message and the assistant/tool tail intact and
// summarizes the earlier turns line by line.
type ExtractiveCompactor struct{}

// Compact implements Compactor.
func (ExtractiveCompactor) Compact(
	ctx context.Context,
	messages []core.Message,
	targetTokens int,
) ([]core.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lastUser := lastUserIndex(messages)
	if lastUser < 0 {
		return nil, fmt.Errorf("no user message to preserve")
	}

	head := messages[:lastUser]
	tail := messages[lastUser:]

	lines := make([]string, 0, len(head))
	for _, message := range head {
		if line := summaryLine(message); line != "" {
			lines = append(lines, line)
		}
	}

	summary := summaryMessage(lines)
	targetTokens -= estimateMessages(tail)
	if targetTokens < 0 {
		targetTokens = 0
	}
	for len(lines) > minSummaryLines && EstimateMessage(summary) > targetTokens {
		lines = lines[1:]
		summary = summaryMessage(lines)
	}

	out := make([]core.Message, 0, len(tail)+1)
	out = append(out, summary)
	out = append(out, tail...)
	return out, nil
}

// summaryLine describes one message in a single bounded line.
func summaryLine(message core.Message) string {
	switch message.Role {
	case core.RoleUser:
		return "- user: " + truncateRunes(collapseSpace(message.Content), summaryUserRunes)
	case core.RoleAssistant:
		if len(message.ToolCalls) > 0 {
			names := make([]string, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				names = append(names, call.Name)
			}
			line := "- assistant requested tools: " + strings.Join(names, ", ")
			if text := collapseSpace(message.Content); text != "" {
				line += " — " + truncateRunes(text, summaryAssistantRunes)
			}
			return line
		}
		if text := collapseSpace(message.Content); text != "" {
			return "- assistant: " + truncateRunes(text, summaryAssistantRunes)
		}
		return ""
	case core.RoleTool:
		name := message.ToolName
		if name == "" {
			name = "tool"
		}
		return fmt.Sprintf("- %s returned %d bytes: %s",
			name, len(message.Content), truncateRunes(collapseSpace(message.Content), summaryToolRunes))
	default:
		return ""
	}
}

// collapseSpace turns newlines and runs of spaces into single spaces so each summary line stays
// on one line.
func collapseSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

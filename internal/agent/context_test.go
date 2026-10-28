// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

func testTurn(messages ...core.Message) *Turn {
	turn := newTurn("session", "current instruction", "id", time.Unix(0, 0))
	for _, message := range messages {
		turn.Append(message)
	}
	return turn
}

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("empty = %d", got)
	}
	if got := EstimateTokens("12345678"); got != 2 {
		t.Errorf("8 bytes = %d, want 2", got)
	}
	message := core.Message{
		Role:      core.RoleAssistant,
		Content:   "12345678",
		ToolCalls: []core.ToolCall{{Name: "fs_read", Arguments: []byte(`{"path":"a"}`)}},
	}
	if got := EstimateMessage(message); got < 6 {
		t.Errorf("message = %d, want at least 6", got)
	}
}

func TestBuildKeepsSmallHistory(t *testing.T) {
	manager, err := NewContextManager(ContextConfig{Window: 8192, ReserveOutput: 512}, ExtractiveCompactor{})
	if err != nil {
		t.Fatal(err)
	}
	turn := testTurn(
		core.Message{Role: core.RoleSystem, Content: "system"},
		core.Message{Role: core.RoleUser, Content: "current instruction"},
	)

	messages, err := manager.Build(context.Background(), turn)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if messages[0].Content != "system" || messages[1].Content != "current instruction" {
		t.Errorf("unexpected messages: %+v", messages)
	}
}

func TestBuildTrimsOldToolResults(t *testing.T) {
	manager, err := NewContextManager(ContextConfig{Window: 1200, ReserveOutput: 200}, ExtractiveCompactor{})
	if err != nil {
		t.Fatal(err)
	}

	big := strings.Repeat("x", 1600)
	turn := testTurn(
		core.Message{Role: core.RoleSystem, Content: "system"},
		core.Message{Role: core.RoleAssistant, Content: "calling"},
		core.Message{Role: core.RoleTool, ToolName: "fs_read", ToolCallID: "1", Content: big},
		core.Message{Role: core.RoleTool, ToolName: "fs_read", ToolCallID: "2", Content: big},
		core.Message{Role: core.RoleTool, ToolName: "fs_read", ToolCallID: "3", Content: big},
		core.Message{Role: core.RoleUser, Content: "current instruction"},
	)

	messages, err := manager.Build(context.Background(), turn)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The two newest tool results stay intact; the oldest is soft-trimmed.
	if len(messages) < 6 {
		t.Fatalf("messages = %d", len(messages))
	}
	if messages[2].Content == big {
		t.Error("the oldest tool result was not trimmed")
	}
	if messages[4].Content != big {
		t.Error("the newest tool result must stay intact")
	}
	if messages[len(messages)-1].Content != "current instruction" {
		t.Error("the current instruction was modified")
	}
}

func TestBuildCompactsLongHistory(t *testing.T) {
	manager, err := NewContextManager(ContextConfig{Window: 1500, ReserveOutput: 200}, ExtractiveCompactor{})
	if err != nil {
		t.Fatal(err)
	}

	turn := testTurn(core.Message{Role: core.RoleSystem, Content: "system"})
	for i := 0; i < 12; i++ {
		turn.Append(core.Message{
			Role:    core.RoleUser,
			Content: strings.Repeat("user detail ", 40),
		})
		turn.Append(core.Message{
			Role:    core.RoleAssistant,
			Content: strings.Repeat("assistant detail ", 40),
		})
		turn.Append(core.Message{
			Role:       core.RoleTool,
			ToolName:   "fs_read",
			ToolCallID: "call",
			Content:    strings.Repeat("tool output ", 60),
		})
	}
	turn.Append(core.Message{Role: core.RoleUser, Content: "current instruction"})

	before := estimateMessages(turn.Messages()[1:])
	messages, err := manager.Build(context.Background(), turn)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	after := estimateMessages(messages[1:])

	if after >= before {
		t.Fatalf("compaction did not reduce tokens: %d -> %d", before, after)
	}
	if reduction := 1 - float64(after)/float64(before); reduction < 0.40 {
		t.Errorf("reduction = %.2f, want at least 0.40", reduction)
	}
	if messages[0].Content != "system" {
		t.Error("the system prompt must stay first")
	}
	if messages[1].Role != core.RoleSystem ||
		!strings.Contains(messages[1].Content, "Summary of earlier conversation") {
		t.Errorf("second message is not the summary: %+v", messages[1])
	}
	if messages[len(messages)-1].Content != "current instruction" {
		t.Error("the current instruction was lost")
	}

	// A second build with the same turn reuses the cached summary and keeps the prefix stable.
	again, err := manager.Build(context.Background(), turn)
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if len(again) < 2 || again[1].Content != messages[1].Content {
		t.Error("the summary changed between builds")
	}
}

func TestBuildOverflowsWhenNothingFits(t *testing.T) {
	manager, err := NewContextManager(ContextConfig{Window: 400, ReserveOutput: 200}, nil)
	if err != nil {
		t.Fatal(err)
	}
	turn := testTurn(
		core.Message{Role: core.RoleSystem, Content: strings.Repeat("system ", 200)},
		core.Message{Role: core.RoleUser, Content: strings.Repeat("history ", 200)},
	)

	if _, err := manager.Build(context.Background(), turn); !core.Is(err, core.CodeContextOverflow) {
		t.Fatalf("error = %v, want CONTEXT_OVERFLOW", err)
	}
}

func TestNewContextManagerValidates(t *testing.T) {
	if _, err := NewContextManager(ContextConfig{Window: 0}, nil); err == nil {
		t.Error("zero window should fail")
	}
	if _, err := NewContextManager(ContextConfig{Window: 100, ReserveOutput: 100}, nil); err == nil {
		t.Error("reserve equal to the window should fail")
	}
}

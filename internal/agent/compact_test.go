// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

func longHistory(turns int) []core.Message {
	messages := make([]core.Message, 0, turns*3+1)
	for i := 0; i < turns; i++ {
		messages = append(messages,
			core.Message{Role: core.RoleUser, Content: strings.Repeat("user question ", 30)},
			core.Message{Role: core.RoleAssistant, Content: strings.Repeat("assistant answer ", 30)},
			core.Message{
				Role:       core.RoleTool,
				ToolName:   "fs_read",
				ToolCallID: "call",
				Content:    strings.Repeat("tool output ", 40),
			},
		)
	}
	return append(messages, core.Message{Role: core.RoleUser, Content: "keep this instruction"})
}

func testTime() time.Time { return time.Unix(0, 0) }

func TestExtractiveCompactorPreservesLastUser(t *testing.T) {
	messages := longHistory(8)
	before := estimateMessages(messages)

	out, err := (ExtractiveCompactor{}).Compact(context.Background(), messages, 200)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("messages = %d, want summary + tail", len(out))
	}
	if out[0].Role != core.RoleSystem ||
		!strings.Contains(out[0].Content, "Summary of earlier conversation") {
		t.Errorf("first message is not the summary: %+v", out[0])
	}
	if out[1].Content != "keep this instruction" {
		t.Errorf("last user message = %q", out[1].Content)
	}
	if !strings.Contains(out[0].Content, "- user:") ||
		!strings.Contains(out[0].Content, "- assistant:") ||
		!strings.Contains(out[0].Content, "fs_read returned") {
		t.Errorf("summary misses message kinds:\n%s", out[0].Content)
	}

	after := estimateMessages(out)
	if reduction := 1 - float64(after)/float64(before); reduction < 0.40 {
		t.Errorf("reduction = %.2f, want at least 0.40", reduction)
	}
}

func TestExtractiveCompactorRequiresUser(t *testing.T) {
	messages := []core.Message{
		{Role: core.RoleAssistant, Content: "hello"},
	}
	if _, err := (ExtractiveCompactor{}).Compact(context.Background(), messages, 100); err == nil {
		t.Fatal("expected an error without a user message")
	}
}

func TestExtractiveCompactorHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (ExtractiveCompactor{}).Compact(ctx, longHistory(2), 100); err == nil {
		t.Fatal("expected a cancellation error")
	}
}

func TestBudgetCheck(t *testing.T) {
	budget := Budget{MaxSteps: 3, MaxToolCalls: 2}
	if err := budget.Check(testTime(), 0, 0); err != nil {
		t.Errorf("initial check: %v", err)
	}
	if err := budget.Check(testTime(), 3, 0); !errors.Is(err, ErrBudgetSteps) {
		t.Errorf("steps error = %v", err)
	}
	if err := budget.Check(testTime(), 0, 2); !errors.Is(err, ErrBudgetTools) {
		t.Errorf("tools error = %v", err)
	}
	deadline := Budget{Deadline: testTime().Add(-1)}
	if err := deadline.Check(testTime(), 0, 0); !errors.Is(err, ErrBudgetTime) {
		t.Errorf("deadline error = %v", err)
	}
}

func TestBudgetWarning(t *testing.T) {
	budget := Budget{MaxSteps: 20}
	turn := newTurn("", "", "id", testTime())

	if _, ok := budget.Warning(turn, 10); ok {
		t.Error("50% must not warn")
	}
	message, ok := budget.Warning(turn, 15)
	if !ok || !strings.Contains(message, "15/20") {
		t.Fatalf("75%% warning = %q, %v", message, ok)
	}
	if _, ok := budget.Warning(turn, 16); ok {
		t.Error("75% must warn only once")
	}
	if message, ok := budget.Warning(turn, 18); !ok || !strings.Contains(message, "18/20") {
		t.Fatalf("90%% warning = %q, %v", message, ok)
	}
	if _, ok := (Budget{}).Warning(turn, 1); ok {
		t.Error("no step limit must not warn")
	}
}

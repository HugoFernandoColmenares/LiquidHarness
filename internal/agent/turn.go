// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

// StopReason explains why a turn ended. It is part of the JSON contract of `run --json`.
type StopReason string

const (
	// StopAnswer means the model produced a final answer.
	StopAnswer StopReason = "answer"
	// StopBudget means a step, tool-call or time budget ran out.
	StopBudget StopReason = "budget"
	// StopLoop means the loop detector stopped repeated identical tool calls.
	StopLoop StopReason = "loop_detected"
	// StopCanceled means the user or a signal canceled the turn.
	StopCanceled StopReason = "canceled"
	// StopFailed means the turn failed with an error.
	StopFailed StopReason = "failed"
)

// Result is the outcome of one turn.
type Result struct {
	TurnID     string      `json:"turn_id"`
	SessionID  string      `json:"session_id,omitempty"`
	Answer     string      `json:"answer"`
	StopReason StopReason  `json:"stop_reason"`
	Steps      int         `json:"steps"`
	ToolCalls  int         `json:"tool_calls"`
	Usage      ports.Usage `json:"usage"`
	Events     uint64      `json:"events"`
	Err        error       `json:"-"`
}

// Turn is the mutable state of one user request. It is owned by the loop goroutine; tool
// results are collected before they are observed, so no field needs a lock.
type Turn struct {
	ID        string
	SessionID string
	Input     string

	messages []core.Message
	results  []core.ToolResult

	steps     int
	toolCalls int
	usage     ports.Usage

	answer    strings.Builder
	reasoning strings.Builder

	seq        uint64
	loopCounts map[string]int
	loopWarned map[string]bool
	warnedAt   map[int]bool

	started time.Time
}

func newTurn(sessionID, input, id string, now time.Time) *Turn {
	return &Turn{
		ID:         id,
		SessionID:  sessionID,
		Input:      input,
		loopCounts: make(map[string]int),
		loopWarned: make(map[string]bool),
		warnedAt:   make(map[int]bool),
		started:    now,
	}
}

// Append adds a canonical message to the conversation.
func (t *Turn) Append(message core.Message) {
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now()
	}
	t.messages = append(t.messages, message)
}

// Messages returns the conversation so far, system prompt included.
func (t *Turn) Messages() []core.Message { return t.messages }

// Observe records a tool result.
func (t *Turn) Observe(result core.ToolResult) {
	t.results = append(t.results, result)
}

// Results returns the tool results observed so far.
func (t *Turn) Results() []core.ToolResult { return t.results }

// Steps returns the number of model calls made in this turn.
func (t *Turn) Steps() int { return t.steps }

// ToolCallCount returns the number of tool calls executed in this turn.
func (t *Turn) ToolCallCount() int { return t.toolCalls }

// Usage returns the token accounting of the turn.
func (t *Turn) Usage() ports.Usage { return t.usage }

// Answer returns the visible text produced by the model.
func (t *Turn) Answer() string { return t.answer.String() }

// nextSeq returns the next event sequence number, starting at 1.
func (t *Turn) nextSeq() uint64 {
	t.seq++
	return t.seq
}

// addUsage merges backend usage. The prompt size of the latest call wins because the context
// grows between steps; completion tokens accumulate across steps.
func (t *Turn) addUsage(usage ports.Usage) {
	if usage.PromptTokens > t.usage.PromptTokens {
		t.usage.PromptTokens = usage.PromptTokens
	}
	t.usage.CompletionTokens += usage.CompletionTokens
	t.usage.TotalTokens = t.usage.PromptTokens + t.usage.CompletionTokens
}

// toolMessage renders a tool result as the message the model reads on the next step.
func toolMessage(result core.ToolResult) core.Message {
	content := string(result.Data)
	if !result.OK || result.Error != nil {
		payload := struct {
			Error *core.ToolError `json:"error"`
		}{Error: result.Error}
		if encoded, err := json.Marshal(payload); err == nil {
			content = string(encoded)
		}
	}
	return core.Message{
		Role:       core.RoleTool,
		ToolCallID: result.CallID,
		Content:    content,
	}
}

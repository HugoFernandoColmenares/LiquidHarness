// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"time"
)

// Role identifies who produced a message.
type Role string

const (
	// RoleSystem is the harness-provided system prompt.
	RoleSystem Role = "system"
	// RoleUser is a human turn.
	RoleUser Role = "user"
	// RoleAssistant is a model turn.
	RoleAssistant Role = "assistant"
	// RoleTool is a tool result injected into the conversation.
	RoleTool Role = "tool"
)

// Message is the canonical, provider-independent representation of one conversation turn.
type Message struct {
	ID         string     `json:"id"`
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

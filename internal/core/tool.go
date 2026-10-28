// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package core

import "encoding/json"

// ToolSpec is the static description of a tool exposed to the model.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Risk        RiskClass       `json:"risk"`
}

// ToolCall is a model request to run a tool. Arguments are already parsed and validated before
// execution; Raw keeps the original block for auditing.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Raw       string          `json:"raw,omitempty"`
}

// ToolMeta is the non-functional metadata attached to every tool result.
type ToolMeta struct {
	DurationMS int64 `json:"duration_ms"`
	Bytes      int   `json:"bytes"`
	Truncated  bool  `json:"truncated"`
}

// ToolError is the machine-readable failure of a tool call.
type ToolError struct {
	Code      string          `json:"code"`
	Message   string          `json:"message"`
	Hint      string          `json:"hint,omitempty"`
	Retryable bool            `json:"retryable"`
	Details   json.RawMessage `json:"details,omitempty"`
}

// ToolResult is the outcome of a tool call.
type ToolResult struct {
	CallID string          `json:"call_id"`
	OK     bool            `json:"ok"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  *ToolError      `json:"error,omitempty"`
	Meta   ToolMeta        `json:"meta"`
}

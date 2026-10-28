// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package ports

import (
	"context"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

// Usage is the token accounting reported by the backend for one model call. Backends that do
// not report usage leave every field at zero; callers must treat zero as "unknown", not as an
// exact count.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatRequest is one model invocation. The canonical messages and tool specs are the only input:
// adapters translate them to their wire format and never the other way around.
type ChatRequest struct {
	Messages    []core.Message
	Tools       []core.ToolSpec
	Temperature float64
	TopK        int
	MaxTokens   int
}

// ChatResponse is a complete, non-streamed answer.
type ChatResponse struct {
	Message      core.Message
	Model        string
	FinishReason string
	Usage        Usage
}

// StreamEventType identifies the kind of event produced by a streaming call.
type StreamEventType string

const (
	// StreamText is a visible answer delta. Tool-call markers and thinking blocks are removed.
	StreamText StreamEventType = "text"
	// StreamReasoning is a reasoning delta, when the backend exposes it separately.
	StreamReasoning StreamEventType = "reasoning"
	// StreamToolCalls carries the complete, validated tool calls of the answer. It is emitted
	// once, after the stream is complete, so an incomplete call is never executed.
	StreamToolCalls StreamEventType = "tool_calls"
	// StreamDone marks the end of the answer and carries the finish reason and usage.
	StreamDone StreamEventType = "done"
)

// StreamEvent is one item of a streaming answer.
type StreamEvent struct {
	Type         StreamEventType
	Text         string
	Reasoning    string
	ToolCalls    []core.ToolCall
	FinishReason string
	Usage        *Usage
}

// Stream is a pull-based iterator. Next returns io.EOF after the final StreamDone event.
// Close is idempotent and must be called even when Next fails.
type Stream interface {
	Next(ctx context.Context) (StreamEvent, error)
	Close() error
}

// Model is the port the agent loop uses to talk to a model backend.
type Model interface {
	// Name returns the model identifier sent to the backend.
	Name() string
	// Chat returns a complete answer.
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	// Stream returns an incremental answer.
	Stream(ctx context.Context, req ChatRequest) (Stream, error)
}

// ModelInfo describes one model advertised by the backend.
type ModelInfo struct {
	ID            string `json:"id"`
	OwnedBy       string `json:"owned_by,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
}

// Catalog is implemented by backends that can list their models. It is separate from Model so
// the agent loop never depends on discovery.
type Catalog interface {
	Models(ctx context.Context) ([]ModelInfo, error)
}

// Health is implemented by backends that expose a liveness probe.
type Health interface {
	Health(ctx context.Context) error
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"encoding/json"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

// Wire types mirror the OpenAI-compatible HTTP contract. They are deliberately private: only
// this package knows the JSON field names, and the rest of the harness sees core types.

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []wireMessage `json:"messages"`
	Tools       []wireTool    `json:"tools,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	TopK        int           `json:"top_k,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type wireMessage struct {
	Role             string         `json:"role"`
	Content          string         `json:"content,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	Name             string         `json:"name,omitempty"`
}

type wireTool struct {
	Type     string           `json:"type"`
	Function wireFunctionSpec `json:"function"`
}

type wireFunctionSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type wireToolCall struct {
	Index    int              `json:"index,omitempty"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function wireFunctionCall `json:"function"`
}

type wireFunctionCall struct {
	Name      string        `json:"name,omitempty"`
	Arguments wireArguments `json:"arguments,omitempty"`
}

// wireArguments accepts both encodings seen in the wild: a JSON string (the OpenAI contract,
// and what llama.cpp streams) and a JSON object (some gateways serialize the object directly).
type wireArguments string

// UnmarshalJSON implements json.Unmarshaler.
func (a *wireArguments) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*a = wireArguments(text)
		return nil
	}
	*a = wireArguments(data)
	return nil
}

type chatResponse struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *wireUsage   `json:"usage,omitempty"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      wireMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type chatChunk struct {
	ID      string        `json:"id"`
	Model   string        `json:"model"`
	Choices []chunkChoice `json:"choices"`
	Usage   *wireUsage    `json:"usage,omitempty"`
	Error   *wireError    `json:"error,omitempty"`
}

type chunkChoice struct {
	Index        int       `json:"index"`
	Delta        wireDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type wireDelta struct {
	Role             string         `json:"role,omitempty"`
	Content          string         `json:"content,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	Reasoning        string         `json:"reasoning,omitempty"`
	ToolCalls        []wireToolCall `json:"tool_calls,omitempty"`
}

type wireUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type wireError struct {
	Message string          `json:"message"`
	Type    string          `json:"type,omitempty"`
	Code    json.RawMessage `json:"code,omitempty"`
}

type wireErrorResponse struct {
	Error *wireError `json:"error,omitempty"`
}

type modelsResponse struct {
	Data []wireModel `json:"data"`
}

type wireModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by,omitempty"`
}

func toWireMessages(messages []core.Message) []wireMessage {
	out := make([]wireMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, wireMessage{
			Role:             string(message.Role),
			Content:          message.Content,
			ReasoningContent: message.Reasoning,
			ToolCallID:       message.ToolCallID,
			Name:             message.ToolName,
			ToolCalls:        toWireToolCalls(message.ToolCalls),
		})
	}
	return out
}

func toWireToolCalls(calls []core.ToolCall) []wireToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]wireToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, wireToolCall{
			ID:   call.ID,
			Type: "function",
			Function: wireFunctionCall{
				Name:      call.Name,
				Arguments: wireArguments(call.Arguments),
			},
		})
	}
	return out
}

func toWireTools(specs []core.ToolSpec) []wireTool {
	if len(specs) == 0 {
		return nil
	}
	out := make([]wireTool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, wireTool{
			Type: "function",
			Function: wireFunctionSpec{
				Name:        spec.Name,
				Description: spec.Description,
				Parameters:  spec.InputSchema,
			},
		})
	}
	return out
}

func toPortsUsage(usage *wireUsage) ports.Usage {
	if usage == nil {
		return ports.Usage{}
	}
	return ports.Usage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/agent"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/config"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/llm"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/version"
)

// AskOptions controls a one-shot model call.
type AskOptions struct {
	Prompt string
	// Stream prints text deltas as they arrive. JSON mode always buffers the answer.
	Stream bool
}

// AskResult is the stable result of `ask` and `run --print`.
type AskResult struct {
	Answer       string          `json:"answer"`
	Reasoning    string          `json:"reasoning,omitempty"`
	Model        string          `json:"model"`
	PromptHash   string          `json:"prompt_hash"`
	FinishReason string          `json:"finish_reason,omitempty"`
	Usage        ports.Usage     `json:"usage"`
	ToolCalls    []core.ToolCall `json:"tool_calls,omitempty"`
}

// Ask sends one prompt to the configured model. It is the F1 one-shot path: no tools are
// registered, no session is persisted and no approval is possible.
func (a *App) Ask(ctx context.Context, opts AskOptions) (AskResult, error) {
	if !a.configured {
		return AskResult{}, core.E(core.CodeInternal, "configuration was not loaded")
	}

	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		return AskResult{}, core.E(core.CodeUsage, "prompt must not be empty")
	}

	model, err := a.newModel(a.Config.Model)
	if err != nil {
		return AskResult{}, err
	}

	cleanup, err := a.ensureModel(ctx)
	if err != nil {
		return AskResult{}, err
	}
	defer cleanup()

	system := agent.SystemPrompt(agent.PromptOptions{
		Workspace: a.Config.General.Workspace,
		Policy:    a.Config.Policy.Mode.String(),
		Language:  a.Config.General.Language,
	})
	hash := agent.PromptHash(system)

	request := ports.ChatRequest{
		Messages: []core.Message{
			{Role: core.RoleSystem, Content: system},
			{Role: core.RoleUser, Content: prompt},
		},
		Temperature: a.Config.Model.Temperature,
		TopK:        a.Config.Model.TopK,
		MaxTokens:   a.Config.Model.MaxOutputTokens,
	}

	result := AskResult{Model: model.Name(), PromptHash: hash}
	if opts.Stream && !a.JSON {
		err = a.askStream(ctx, model, request, &result)
	} else {
		err = a.askChat(ctx, model, request, &result)
	}
	if err != nil {
		return result, err
	}

	if result.Answer == "" && len(result.ToolCalls) == 0 {
		return result, core.E(core.CodeModelUnavailable, "model returned an empty response")
	}
	if len(result.ToolCalls) > 0 {
		return result, core.E(core.CodeModelUnavailable,
			"model requested %d tool call(s) but no tools are available", len(result.ToolCalls))
	}
	return result, nil
}

func (a *App) askStream(
	ctx context.Context,
	model ports.Model,
	request ports.ChatRequest,
	result *AskResult,
) error {
	stream, err := model.Stream(ctx, request)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()

	for {
		event, nextErr := stream.Next(ctx)
		if errors.Is(nextErr, io.EOF) {
			return nil
		}
		if nextErr != nil {
			return nextErr
		}

		switch event.Type {
		case ports.StreamText:
			result.Answer += event.Text
			if _, err := io.WriteString(a.Stdout, event.Text); err != nil {
				return core.Wrap(core.CodeInternal, err, "write answer")
			}
		case ports.StreamReasoning:
			result.Reasoning += event.Reasoning
		case ports.StreamToolCalls:
			result.ToolCalls = event.ToolCalls
		case ports.StreamDone:
			result.FinishReason = event.FinishReason
			if event.Usage != nil {
				result.Usage = *event.Usage
			}
		}
	}
}

func (a *App) askChat(
	ctx context.Context,
	model ports.Model,
	request ports.ChatRequest,
	result *AskResult,
) error {
	response, err := model.Chat(ctx, request)
	if err != nil {
		return err
	}

	result.Answer = response.Message.Content
	result.Reasoning = response.Message.Reasoning
	result.ToolCalls = response.Message.ToolCalls
	result.FinishReason = response.FinishReason
	result.Usage = response.Usage
	if response.Model != "" {
		result.Model = response.Model
	}

	if a.JSON {
		return nil
	}
	if _, err := io.WriteString(a.Stdout, result.Answer); err != nil {
		return core.Wrap(core.CodeInternal, err, "write answer")
	}
	return nil
}

func defaultModelFactory(cfg config.Model) (ports.Model, error) {
	return llm.New(llm.Config{
		BaseURL:    cfg.BaseURL,
		Model:      cfg.Name,
		APIKey:     cfg.APIKey,
		Timeout:    cfg.Timeout.Std(),
		MaxRetries: 2,
		Backoff:    250 * time.Millisecond,
		UserAgent:  "liquidharness/" + version.Version,
	})
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package porttest contains reusable conformance suites for the ports interfaces. Every
// adapter runs the same suite, so a new backend cannot quietly break a contract that the agent
// loop relies on.
package porttest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

// Model runs the ports.Model conformance suite. factory must return a ready model; the test
// never reuses it across subtests because scripted fakes are stateful.
func Model(t *testing.T, factory func(t *testing.T) ports.Model) {
	t.Helper()

	t.Run("name is not empty", func(t *testing.T) {
		model := factory(t)
		if strings.TrimSpace(model.Name()) == "" {
			t.Fatal("Name() is empty")
		}
	})

	t.Run("chat returns an assistant message", func(t *testing.T) {
		model := factory(t)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		response, err := model.Chat(ctx, ports.ChatRequest{
			Messages:  []core.Message{{Role: core.RoleUser, Content: "Reply with the single word: OK"}},
			MaxTokens: 16,
		})
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		if response.Message.Role != "" && response.Message.Role != core.RoleAssistant {
			t.Errorf("role = %q, want assistant", response.Message.Role)
		}
		if response.Message.Content == "" && len(response.Message.ToolCalls) == 0 &&
			response.Message.Reasoning == "" {
			t.Error("Chat returned an empty message")
		}
	})

	t.Run("stream ends with done", func(t *testing.T) {
		model := factory(t)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		stream, err := model.Stream(ctx, ports.ChatRequest{
			Messages:  []core.Message{{Role: core.RoleUser, Content: "Reply with the single word: OK"}},
			MaxTokens: 16,
		})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		defer func() { _ = stream.Close() }()

		var last ports.StreamEvent
		for {
			event, nextErr := stream.Next(ctx)
			if errors.Is(nextErr, io.EOF) {
				break
			}
			if nextErr != nil {
				t.Fatalf("Next: %v", nextErr)
			}
			last = event
		}
		if last.Type != ports.StreamDone {
			t.Errorf("last event = %q, want done", last.Type)
		}
	})

	t.Run("close is idempotent", func(t *testing.T) {
		model := factory(t)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		stream, err := model.Stream(ctx, ports.ChatRequest{
			Messages: []core.Message{{Role: core.RoleUser, Content: "OK"}},
		})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		if err := stream.Close(); err != nil {
			t.Errorf("first Close: %v", err)
		}
		if err := stream.Close(); err != nil {
			t.Errorf("second Close: %v", err)
		}
	})

	t.Run("canceled context is reported as CANCELED", func(t *testing.T) {
		model := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		stream, err := model.Stream(ctx, ports.ChatRequest{
			Messages: []core.Message{{Role: core.RoleUser, Content: "Write a long essay"}},
		})
		if err != nil {
			cancel()
			t.Fatalf("Stream: %v", err)
		}
		defer func() { _ = stream.Close() }()

		cancel()
		done := make(chan error, 1)
		go func() {
			_, nextErr := stream.Next(ctx)
			done <- nextErr
		}()

		select {
		case nextErr := <-done:
			if nextErr == nil || !core.Is(nextErr, core.CodeCanceled) {
				t.Errorf("error = %v, want CANCELED", nextErr)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cancellation did not stop the stream")
		}
	})
}

// Tool runs the ports.Tool conformance suite. factory must return a tool and a JSON argument
// object that succeeds inside its workspace.
func Tool(t *testing.T, factory func(t *testing.T) (ports.Tool, json.RawMessage)) {
	t.Helper()

	t.Run("spec is valid", func(t *testing.T) {
		tool, _ := factory(t)
		spec := tool.Spec()
		if strings.TrimSpace(spec.Name) == "" {
			t.Fatal("Spec().Name is empty")
		}
		if strings.TrimSpace(spec.Description) == "" {
			t.Fatal("Spec().Description is empty")
		}
		if !spec.Risk.Valid() {
			t.Fatalf("Spec().Risk = %d", spec.Risk)
		}
		if !json.Valid(spec.InputSchema) {
			t.Fatal("Spec().InputSchema is not valid JSON")
		}
	})

	t.Run("malformed arguments never panic", func(t *testing.T) {
		tool, _ := factory(t)
		result, err := tool.Execute(context.Background(), core.ToolCall{
			ID:        "call-1",
			Name:      tool.Spec().Name,
			Arguments: json.RawMessage(`{`),
		})
		if err == nil && (result.Error == nil || result.Error.Code == "") {
			t.Fatal("malformed arguments were accepted")
		}
	})

	t.Run("empty arguments never panic", func(t *testing.T) {
		tool, _ := factory(t)
		result, err := tool.Execute(context.Background(), core.ToolCall{
			ID:        "call-1",
			Name:      tool.Spec().Name,
			Arguments: json.RawMessage(`{}`),
		})
		if err != nil {
			return
		}
		if result.Error == nil && result.CallID != "call-1" {
			t.Fatal("empty arguments produced neither a typed error nor a result")
		}
	})

	t.Run("valid call keeps the call id", func(t *testing.T) {
		tool, arguments := factory(t)
		result, err := tool.Execute(context.Background(), core.ToolCall{
			ID:        "call-42",
			Name:      tool.Spec().Name,
			Arguments: arguments,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.CallID != "call-42" {
			t.Errorf("CallID = %q, want call-42", result.CallID)
		}
	})
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

func TestFakeChatIsDeterministic(t *testing.T) {
	fake := NewFake("fake", "one")
	ctx := context.Background()

	for range 2 {
		response, err := fake.Chat(ctx, ports.ChatRequest{})
		if err != nil {
			t.Fatalf("chat: %v", err)
		}
		if response.Message.Content != "one" {
			t.Errorf("content = %q", response.Message.Content)
		}
		if response.Model != "fake" {
			t.Errorf("model = %q", response.Model)
		}
	}
}

func TestFakeStream(t *testing.T) {
	fake := NewFake("fake", "hello world")
	stream, err := fake.Stream(context.Background(), ports.ChatRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var text strings.Builder
	var done bool
	for {
		event, err := stream.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		switch event.Type {
		case ports.StreamText:
			text.WriteString(event.Text)
		case ports.StreamReasoning, ports.StreamToolCalls:
			// Not relevant for this assertion.
		case ports.StreamDone:
			done = true
			if event.FinishReason != "stop" {
				t.Errorf("finish = %q", event.FinishReason)
			}
		}
	}

	if text.String() != "hello world" {
		t.Errorf("text = %q", text.String())
	}
	if !done {
		t.Error("missing done event")
	}
}

func TestFakeToolCalls(t *testing.T) {
	reply := ToolCallStart + `[fs_read(path="x")]` + ToolCallEnd
	fake := NewFake("fake", reply)

	response, err := fake.Chat(context.Background(), ports.ChatRequest{})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if response.FinishReason != "tool_calls" || len(response.Message.ToolCalls) != 1 {
		t.Errorf("response = %+v", response)
	}

	stream, err := fake.Stream(context.Background(), ports.ChatRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var calls int
	for {
		event, err := stream.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		if event.Type == ports.StreamToolCalls {
			calls = len(event.ToolCalls)
		}
	}
	if calls != 1 {
		t.Errorf("streamed calls = %d", calls)
	}
}

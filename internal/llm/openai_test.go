// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

func TestChatSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}

		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Model != "test" || request.Stream {
			t.Errorf("unexpected request: %+v", request)
		}

		w.Header().Set(contentTypeHeader, jsonAccept)
		_, _ = io.WriteString(w, `{
			"model":"test",
			"choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
		}`)
	}))
	defer server.Close()

	client := newTestClient(t, server, 0)
	response, err := client.Chat(context.Background(), ports.ChatRequest{
		Messages: []core.Message{{Role: core.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if response.Message.Content != "hello" || response.FinishReason != "stop" {
		t.Errorf("response = %+v", response)
	}
	if response.Usage.TotalTokens != 5 {
		t.Errorf("usage = %+v", response.Usage)
	}
}

func TestChatParsesLFMMarkers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, jsonAccept)
		_, _ = fmt.Fprintf(w, `{"model":"test","choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`,
			ToolCallStart+`[fs_read(path="x")]`+ToolCallEnd+"ok")
	}))
	defer server.Close()

	client := newTestClient(t, server, 0)
	response, err := client.Chat(context.Background(), ports.ChatRequest{
		Messages: []core.Message{{Role: core.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if response.Message.Content != "ok" {
		t.Errorf("content = %q", response.Message.Content)
	}
	if len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Name != "fs_read" {
		t.Errorf("calls = %+v", response.Message.ToolCalls)
	}
}

func TestChatParsesWireToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, jsonAccept)
		_, _ = io.WriteString(w, `{
			"model":"test",
			"choices":[{"message":{"role":"assistant","tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"fs_read","arguments":{"path":"x"}}}
			]},"finish_reason":"tool_calls"}]
		}`)
	}))
	defer server.Close()

	client := newTestClient(t, server, 0)
	response, err := client.Chat(context.Background(), ports.ChatRequest{
		Messages: []core.Message{{Role: core.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if len(response.Message.ToolCalls) != 1 {
		t.Fatalf("calls = %+v", response.Message.ToolCalls)
	}
	call := response.Message.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "fs_read" || !strings.Contains(string(call.Arguments), `"path":"x"`) {
		t.Errorf("call = %+v", call)
	}
}

func TestChatHTTP400IsNotRetried(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set(contentTypeHeader, jsonAccept)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad request"}}`)
	}))
	defer server.Close()

	client := newTestClient(t, server, 2)
	_, err := client.Chat(context.Background(), ports.ChatRequest{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !core.Is(err, core.CodeModelUnavailable) {
		t.Errorf("error = %v", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1", attempts.Load())
	}
}

func TestChatRetries429(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"slow down"}}`)
			return
		}
		w.Header().Set(contentTypeHeader, jsonAccept)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := newTestClient(t, server, 1)
	response, err := client.Chat(context.Background(), ports.ChatRequest{})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if response.Message.Content != "ok" {
		t.Errorf("content = %q", response.Message.Content)
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
}

func TestChatRetries500ThenFails(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	defer server.Close()

	client := newTestClient(t, server, 1)
	_, err := client.Chat(context.Background(), ports.ChatRequest{})
	if err == nil {
		t.Fatal("expected an error")
	}
	var coded *core.Error
	if !errors.As(err, &coded) || !coded.Retryable() {
		t.Errorf("error = %v, want a retryable coded error", err)
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
}

func TestChatTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set(contentTypeHeader, jsonAccept)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"late"}}]}`)
	}))
	defer server.Close()

	client, err := New(Config{
		BaseURL:    server.URL + "/v1",
		Model:      "test",
		HTTPClient: &http.Client{Timeout: 20 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), ports.ChatRequest{})
	if err == nil {
		t.Fatal("expected a timeout")
	}
	if !core.Is(err, core.CodeModelUnavailable) {
		t.Errorf("error = %v", err)
	}
}

func TestChatTruncatedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, jsonAccept)
		_, _ = io.WriteString(w, `{"choices":[{"message":`)
	}))
	defer server.Close()

	client := newTestClient(t, server, 0)
	_, err := client.Chat(context.Background(), ports.ChatRequest{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !core.Is(err, core.CodeModelUnavailable) {
		t.Errorf("error = %v", err)
	}
}

func TestStreamSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, streamAccept)
		flusher := w.(http.Flusher)
		events := []any{
			map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "Hel"}}}},
			map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "lo"}}}},
			map[string]any{
				"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}},
				"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3},
			},
		}
		for _, event := range events {
			writeSSE(t, w, event)
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := newTestClient(t, server, 0)
	stream, err := client.Stream(context.Background(), ports.ChatRequest{
		Messages: []core.Message{{Role: core.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var text strings.Builder
	var done ports.StreamEvent
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
			done = event
		}
	}

	if text.String() != "Hello" {
		t.Errorf("text = %q", text.String())
	}
	if done.FinishReason != "stop" {
		t.Errorf("finish = %q", done.FinishReason)
	}
	if done.Usage == nil || done.Usage.TotalTokens != 3 {
		t.Errorf("usage = %+v", done.Usage)
	}
}

func TestStreamToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, streamAccept)
		flusher := w.(http.Flusher)
		chunks := []chatChunk{
			{Choices: []chunkChoice{{Delta: wireDelta{ToolCalls: []wireToolCall{{
				Index: 0, ID: "call_1",
				Function: wireFunctionCall{Name: "fs_read", Arguments: wireArguments(`{"path":`)},
			}}}}}},
			{Choices: []chunkChoice{{Delta: wireDelta{ToolCalls: []wireToolCall{{
				Index:    0,
				Function: wireFunctionCall{Arguments: wireArguments(`"x"}`)},
			}}}}}},
			{Choices: []chunkChoice{{Delta: wireDelta{}, FinishReason: stringPtr("tool_calls")}}},
		}
		for _, chunk := range chunks {
			writeSSE(t, w, chunk)
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := newTestClient(t, server, 0)
	stream, err := client.Stream(context.Background(), ports.ChatRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var calls []core.ToolCall
	for {
		event, err := stream.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		if event.Type == ports.StreamToolCalls {
			calls = event.ToolCalls
		}
	}

	if len(calls) != 1 || calls[0].Name != "fs_read" {
		t.Fatalf("calls = %+v", calls)
	}
	if string(calls[0].Arguments) != `{"path":"x"}` {
		t.Errorf("arguments = %s", calls[0].Arguments)
	}
}

func TestStreamTruncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, streamAccept)
		writeSSE(t, w, map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{"content": "partial"}}},
		})
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	client := newTestClient(t, server, 0)
	stream, err := client.Stream(context.Background(), ports.ChatRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	_, _ = stream.Next(context.Background())
	_, err = stream.Next(context.Background())
	if err == nil {
		t.Fatal("expected a truncated stream error")
	}
	if !core.Is(err, core.CodeModelUnavailable) || !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error = %v", err)
	}
}

func TestStreamMalformedSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, streamAccept)
		_, _ = io.WriteString(w, "data: {not json}\n\n")
	}))
	defer server.Close()

	client := newTestClient(t, server, 0)
	stream, err := client.Stream(context.Background(), ports.ChatRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	if _, err := stream.Next(context.Background()); err == nil {
		t.Fatal("expected a malformed SSE error")
	}
}

func TestStreamCancel(t *testing.T) {
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(contentTypeHeader, streamAccept)
		writeSSE(t, w, map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{"content": "first"}}},
		})
		w.(http.Flusher).Flush()
		// Block until the client drops the connection. This is the same signal a real
		// llama-server sees when the user presses Ctrl+C.
		<-r.Context().Done()
		close(serverDone)
	}))
	defer server.Close()

	client := newTestClient(t, server, 0)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.Stream(ctx, ports.ChatRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	if _, err := stream.Next(ctx); err != nil {
		t.Fatalf("first next: %v", err)
	}

	started := time.Now()
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := stream.Next(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !core.Is(err, core.CodeCanceled) {
			t.Errorf("error = %v, want CANCELED", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop the stream")
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Errorf("cancellation took %s, want < 200ms", elapsed)
	}

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("the server did not observe the closed connection")
	}
}

func TestModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set(contentTypeHeader, jsonAccept)
		_, _ = io.WriteString(w, `{"data":[{"id":"lfm2.5-2.6b","owned_by":"local"}]}`)
	}))
	defer server.Close()

	client := newTestClient(t, server, 0)
	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "lfm2.5-2.6b" {
		t.Errorf("models = %+v", models)
	}
}

func newTestClient(t *testing.T, server *httptest.Server, retries int) *Client {
	t.Helper()
	client, err := New(Config{
		BaseURL:    server.URL + "/v1",
		Model:      "test",
		HTTPClient: server.Client(),
		MaxRetries: retries,
		Backoff:    time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func writeSSE(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode SSE: %v", err)
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func stringPtr(value string) *string { return &value }

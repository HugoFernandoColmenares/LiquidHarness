// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/llm"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports/porttest"
)

func TestFakeModelConformance(t *testing.T) {
	porttest.Model(t, func(t *testing.T) ports.Model {
		return llm.NewFake("fake", "OK")
	})
}

func TestClientModelConformance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Stream bool `json:"stream"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)

		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{
  "model": "lfm2.5",
  "choices": [{"message": {"role": "assistant", "content": "OK"}, "finish_reason": "stop"}],
  "usage": {"prompt_tokens": 4, "completion_tokens": 1, "total_tokens": 5}
}`)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n")
		flusher.Flush()
		// Wait for the client to close the connection, which is what the conformance
		// cancellation test does.
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":1,\"total_tokens\":5}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	porttest.Model(t, func(t *testing.T) ports.Model {
		client, err := llm.New(llm.Config{
			BaseURL: server.URL + "/v1",
			Model:   "lfm2.5",
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("llm.New: %v", err)
		}
		return client
	})
}

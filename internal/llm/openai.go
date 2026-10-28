// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

const (
	defaultTimeout    = 120 * time.Second
	defaultBackoff    = 250 * time.Millisecond
	maxBackoff        = 5 * time.Second
	maxErrorBody      = 8 << 10
	maxResponseBody   = 16 << 20
	defaultUserAgent  = "liquidharness"
	streamAccept      = "text/event-stream"
	jsonAccept        = "application/json"
	contentTypeHeader = "Content-Type"
)

// Config configures the OpenAI-compatible client.
type Config struct {
	// BaseURL is the versioned API root, for example http://127.0.0.1:8080/v1.
	BaseURL string
	// Model is the identifier sent in every request.
	Model string
	// APIKey is optional; local llama-server instances usually do not need one.
	APIKey string
	// Timeout bounds one complete request, including the body of a stream.
	Timeout time.Duration
	// MaxRetries is the number of retries after the first attempt. Zero disables retries.
	MaxRetries int
	// Backoff is the delay before the first retry; it doubles on every attempt.
	Backoff time.Duration
	// HTTPClient overrides the default client. Its Timeout is left untouched.
	HTTPClient *http.Client
	// UserAgent identifies the harness to the backend.
	UserAgent string
}

// Client is the OpenAI-compatible adapter. It is safe for concurrent use.
type Client struct {
	baseURL    string
	model      string
	apiKey     string
	httpClient *http.Client
	maxRetries int
	backoff    time.Duration
	userAgent  string
}

var (
	_ ports.Model   = (*Client)(nil)
	_ ports.Catalog = (*Client)(nil)
	_ ports.Health  = (*Client)(nil)
)

// New validates the configuration and builds a client.
func New(cfg Config) (*Client, error) {
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, core.Wrap(core.CodeConfigInvalid, err, "invalid model base URL %q", cfg.BaseURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, core.E(core.CodeConfigInvalid,
			"model base URL must use http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, core.E(core.CodeConfigInvalid, "model base URL must include a host")
	}
	if parsed.User != nil {
		return nil, core.E(core.CodeConfigInvalid,
			"model base URL must not embed credentials; use LIQUIDHARNESS_MODEL_API_KEY")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, core.E(core.CodeConfigInvalid, "model name must not be empty")
	}
	if cfg.MaxRetries < 0 {
		return nil, core.E(core.CodeConfigInvalid, "max retries must not be negative")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	backoff := cfg.Backoff
	if backoff <= 0 {
		backoff = defaultBackoff
	}
	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}

	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		model:      strings.TrimSpace(cfg.Model),
		apiKey:     cfg.APIKey,
		httpClient: httpClient,
		maxRetries: cfg.MaxRetries,
		backoff:    backoff,
		userAgent:  userAgent,
	}, nil
}

// Name returns the configured model identifier.
func (c *Client) Name() string { return c.model }

// Chat sends a non-streamed request and returns the canonical answer.
func (c *Client) Chat(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, error) {
	body, err := c.buildRequest(req, false)
	if err != nil {
		return ports.ChatResponse{}, core.Wrap(core.CodeInternal, err, "encode model request")
	}

	resp, err := c.do(ctx, http.MethodPost, c.endpoint("chat/completions"), body)
	if err != nil {
		return ports.ChatResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var payload chatResponse
	if err := decodeJSON(resp.Body, &payload); err != nil {
		return ports.ChatResponse{}, core.Wrap(core.CodeModelUnavailable, err, "decode model response")
	}
	if len(payload.Choices) == 0 {
		return ports.ChatResponse{}, core.E(core.CodeModelUnavailable, "model returned no choices")
	}

	choice := payload.Choices[0]
	parsed, err := Parse(choice.Message.Content)
	if err != nil {
		return ports.ChatResponse{}, core.Wrap(core.CodeModelUnavailable, err, "parse model response")
	}

	calls, err := toolCallsFromWire(choice.Message.ToolCalls)
	if err != nil {
		return ports.ChatResponse{}, core.Wrap(core.CodeModelUnavailable, err, "parse model tool calls")
	}
	if len(calls) == 0 {
		calls = parsed.ToolCalls
	}

	reasoning := choice.Message.ReasoningContent
	if reasoning == "" {
		reasoning = parsed.Reasoning
	}
	message := core.Message{
		Role:      core.RoleAssistant,
		Content:   parsed.Content,
		Reasoning: reasoning,
		ToolCalls: calls,
	}
	if message.Content == "" && strings.TrimSpace(message.Reasoning) == "" && len(calls) == 0 {
		return ports.ChatResponse{}, core.E(core.CodeModelUnavailable, "model returned an empty response")
	}

	model := payload.Model
	if model == "" {
		model = c.model
	}
	return ports.ChatResponse{
		Message:      message,
		Model:        model,
		FinishReason: choice.FinishReason,
		Usage:        toPortsUsage(payload.Usage),
	}, nil
}

// Stream opens an SSE stream. The response is validated before the stream is returned, so a
// caller never receives a stream that already failed with an HTTP error.
func (c *Client) Stream(ctx context.Context, req ports.ChatRequest) (ports.Stream, error) {
	body, err := c.buildRequest(req, true)
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "encode model request")
	}

	resp, err := c.do(ctx, http.MethodPost, c.endpoint("chat/completions"), body)
	if err != nil {
		return nil, err
	}

	if contentType := resp.Header.Get(contentTypeHeader); strings.Contains(contentType, jsonAccept) {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		_ = resp.Body.Close()
		return nil, core.E(core.CodeModelUnavailable, "model did not stream: %s", responseMessage(data))
	}

	return newStream(resp.Body), nil
}

// Models lists the models advertised by the backend.
func (c *Client) Models(ctx context.Context) ([]ports.ModelInfo, error) {
	resp, err := c.do(ctx, http.MethodGet, c.endpoint("models"), nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var payload modelsResponse
	if err := decodeJSON(resp.Body, &payload); err != nil {
		return nil, core.Wrap(core.CodeModelUnavailable, err, "decode model list")
	}

	models := make([]ports.ModelInfo, 0, len(payload.Data))
	for _, model := range payload.Data {
		models = append(models, ports.ModelInfo{ID: model.ID, OwnedBy: model.OwnedBy})
	}
	return models, nil
}

// Health probes the llama.cpp /health endpoint at the root of the base URL.
func (c *Client) Health(ctx context.Context) error {
	root := strings.TrimSuffix(c.baseURL, "/v1")
	resp, err := c.do(ctx, http.MethodGet, root+"/health", nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	return nil
}

func (c *Client) buildRequest(req ports.ChatRequest, stream bool) ([]byte, error) {
	payload := chatRequest{
		Model:       c.model,
		Messages:    toWireMessages(req.Messages),
		Tools:       toWireTools(req.Tools),
		Temperature: req.Temperature,
		TopK:        req.TopK,
		MaxTokens:   req.MaxTokens,
		Stream:      stream,
	}
	return json.Marshal(payload)
}

func (c *Client) endpoint(path string) string {
	return c.baseURL + "/" + path
}

func (c *Client) do(ctx context.Context, method, target string, body []byte) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, c.delay(attempt)); err != nil {
				return nil, err
			}
		}

		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, reader)
		if err != nil {
			return nil, core.Wrap(core.CodeInternal, err, "build model request")
		}
		req.Header.Set("Accept", jsonAccept+", "+streamAccept)
		if body != nil {
			req.Header.Set(contentTypeHeader, jsonAccept)
		}
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		req.Header.Set("User-Agent", c.userAgent)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = transportError(err)
			if ctx.Err() != nil {
				return nil, lastErr
			}
			continue
		}

		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			return resp, nil
		}

		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		_ = resp.Body.Close()
		lastErr = statusError(resp.StatusCode, errBody)
		if !retryableStatus(resp.StatusCode) {
			return nil, lastErr
		}
	}

	if lastErr == nil {
		lastErr = core.E(core.CodeModelUnavailable, "model request failed")
	}
	return nil, lastErr
}

func (c *Client) delay(attempt int) time.Duration {
	delay := c.backoff
	for index := 1; index < attempt; index++ {
		delay *= 2
		if delay >= maxBackoff {
			return maxBackoff
		}
	}
	return delay
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return core.Wrap(core.CodeCanceled, ctx.Err(), "model request canceled")
	case <-timer.C:
		return nil
	}
}

func transportError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return core.Wrap(core.CodeCanceled, err, "model request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		coded := core.Wrap(core.CodeModelUnavailable, err, "model request timed out")
		coded.Retry = true
		return coded
	default:
		coded := core.Wrap(core.CodeModelUnavailable, err, "model request failed")
		coded.Retry = true
		return coded
	}
}

func statusError(status int, body []byte) *core.Error {
	message := responseMessage(body)

	var coded *core.Error
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		coded = core.E(core.CodeConfigInvalid, "model authentication failed (HTTP %d): %s", status, message)
	case status == http.StatusNotFound:
		coded = core.E(core.CodeNotFound, "model endpoint not found (HTTP %d): %s", status, message)
	case status == http.StatusTooManyRequests:
		coded = core.E(core.CodeModelUnavailable, "model is rate limited (HTTP %d): %s", status, message)
		coded.Retry = true
	case status >= http.StatusInternalServerError:
		coded = core.E(core.CodeModelUnavailable, "model backend failed (HTTP %d): %s", status, message)
		coded.Retry = true
	case status >= http.StatusBadRequest:
		coded = core.E(core.CodeModelUnavailable, "model rejected the request (HTTP %d): %s", status, message)
	default:
		coded = core.E(core.CodeModelUnavailable, "model request failed (HTTP %d): %s", status, message)
	}
	return coded
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func responseMessage(body []byte) string {
	var payload wireErrorResponse
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil {
		if payload.Error.Message != "" {
			return payload.Error.Message
		}
	}
	return snippet(string(body))
}

func decodeJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maxResponseBody))
	return decoder.Decode(target)
}

func toolCallsFromWire(calls []wireToolCall) ([]core.ToolCall, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	if len(calls) > MaxToolCalls {
		return nil, fmt.Errorf("model returned %d tool calls; the limit is %d", len(calls), MaxToolCalls)
	}

	out := make([]core.ToolCall, 0, len(calls))
	for index, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			return nil, fmt.Errorf("tool call %d has no name", index)
		}
		if !toolNamePattern.MatchString(name) {
			return nil, fmt.Errorf("tool call %d has an invalid name %q", index, name)
		}

		arguments, err := normalizeArguments(string(call.Function.Arguments))
		if err != nil {
			return nil, fmt.Errorf("tool call %d (%s): %w", index, name, err)
		}

		id := call.ID
		if id == "" {
			id = core.NewID()
		}
		out = append(out, core.ToolCall{
			ID:        id,
			Name:      name,
			Arguments: arguments,
			Raw:       string(call.Function.Arguments),
		})
	}
	return out, nil
}

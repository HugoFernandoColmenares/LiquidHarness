// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/liquidharness/liquidharness/internal/core"
)

// Validate checks every invariant and returns a single error listing all problems, so the user
// can fix the file in one pass.
func (c Config) Validate() error {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if c.ConfigVersion < 1 || c.ConfigVersion > CurrentVersion {
		add("config_version must be between 1 and %d, got %d", CurrentVersion, c.ConfigVersion)
	}

	switch c.General.Profile {
	case "fast", "balanced", "quality":
	default:
		add("general.profile %q is not one of fast, balanced, quality", c.General.Profile)
	}
	if c.General.Workspace == "" {
		add("general.workspace must not be empty")
	} else if !filepath.IsAbs(c.General.Workspace) {
		add("general.workspace must be absolute after resolution, got %q", c.General.Workspace)
	}
	if c.General.Language == "" {
		add("general.language must not be empty")
	}
	switch strings.ToLower(c.General.LogLevel) {
	case "debug", "info", "warn", "warning", "error":
	default:
		add("general.log_level %q is not one of debug, info, warn, error", c.General.LogLevel)
	}
	switch strings.ToLower(c.General.LogFormat) {
	case "text", "json":
	default:
		add("general.log_format %q is not one of text, json", c.General.LogFormat)
	}
	switch c.General.Telemetry {
	case "off", "local", "anonymous":
	default:
		add("general.telemetry %q is not one of off, local, anonymous", c.General.Telemetry)
	}

	switch c.Model.Provider {
	case "openai-compatible", "embedded":
	default:
		add("model.provider %q is not one of openai-compatible, embedded", c.Model.Provider)
	}
	if err := validateBaseURL(c.Model.BaseURL); err != nil {
		add("model.base_url %v", err)
	}
	if c.Model.Name == "" {
		add("model.name must not be empty")
	}
	if c.Model.ContextWindow < 1024 || c.Model.ContextWindow > 262144 {
		add("model.context_window must be between 1024 and 262144, got %d", c.Model.ContextWindow)
	}
	if c.Model.MaxOutputTokens < 1 || c.Model.MaxOutputTokens > c.Model.ContextWindow {
		add("model.max_output_tokens must be between 1 and context_window (%d), got %d",
			c.Model.ContextWindow, c.Model.MaxOutputTokens)
	}
	if c.Model.Temperature < 0 || c.Model.Temperature > 2 {
		add("model.temperature must be between 0 and 2, got %g", c.Model.Temperature)
	}
	if c.Model.TopK < 0 || c.Model.TopK > 1000 {
		add("model.top_k must be between 0 and 1000, got %d", c.Model.TopK)
	}
	if c.Model.RepetitionPenalty <= 0 {
		add("model.repetition_penalty must be positive, got %g", c.Model.RepetitionPenalty)
	}
	if c.Model.Timeout <= 0 {
		add("model.timeout must be positive, got %s", c.Model.Timeout)
	}

	if c.Agent.MaxSteps < 1 || c.Agent.MaxSteps > 200 {
		add("agent.max_steps must be between 1 and 200, got %d", c.Agent.MaxSteps)
	}
	if c.Agent.MaxToolCalls < 1 || c.Agent.MaxToolCalls > 500 {
		add("agent.max_tool_calls must be between 1 and 500, got %d", c.Agent.MaxToolCalls)
	}
	if c.Agent.MaxTurnDuration <= 0 {
		add("agent.max_turn_duration must be positive, got %s", c.Agent.MaxTurnDuration)
	}
	if c.Agent.ParallelReadOnlyTools < 1 || c.Agent.ParallelReadOnlyTools > 32 {
		add("agent.parallel_readonly_tools must be between 1 and 32, got %d",
			c.Agent.ParallelReadOnlyTools)
	}

	if !c.Policy.Mode.Valid() {
		add("policy.mode %q is not one of read-only, ask, allow", string(c.Policy.Mode))
	}
	if c.Policy.MaxWriteBytes < 0 {
		add("policy.max_write_bytes must be zero or positive, got %d", c.Policy.MaxWriteBytes)
	}

	if c.RAG.ChunkTokens < 64 || c.RAG.ChunkTokens > 8192 {
		add("rag.chunk_tokens must be between 64 and 8192, got %d", c.RAG.ChunkTokens)
	}
	if c.RAG.ChunkOverlap < 0 || c.RAG.ChunkOverlap >= c.RAG.ChunkTokens {
		add("rag.chunk_overlap must be between 0 and chunk_tokens-1, got %d", c.RAG.ChunkOverlap)
	}
	if c.RAG.TopK < 1 || c.RAG.TopK > 50 {
		add("rag.top_k must be between 1 and 50, got %d", c.RAG.TopK)
	}
	if c.RAG.MaxTokens < 256 || c.RAG.MaxTokens > 32768 {
		add("rag.max_tokens must be between 256 and 32768, got %d", c.RAG.MaxTokens)
	}
	if c.RAG.Enabled && c.RAG.EmbeddingModel == "" {
		add("rag.embedding_model must not be empty when rag.enabled is true")
	}

	if c.Storage.RetentionDays < 0 {
		add("storage.retention_days must be zero or positive, got %d", c.Storage.RetentionDays)
	}
	if c.Storage.DataDir == "" {
		add("storage.data_dir must not be empty after resolution")
	}
	if c.Storage.DBPath == "" {
		add("storage.db_path must not be empty after resolution")
	}

	if c.UI.Theme == "" {
		add("ui.theme must not be empty")
	}

	if err := validateServer(c.Server); err != nil {
		add("%v", err)
	}

	if len(problems) == 0 {
		return nil
	}
	return core.E(core.CodeConfigInvalid, "invalid configuration: %s", strings.Join(problems, "; "))
}

func validateBaseURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("is not a valid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("must use http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("must include a host")
	}
	if parsed.User != nil {
		return fmt.Errorf("must not embed credentials; use LIQUIDHARNESS_MODEL_API_KEY")
	}
	return nil
}

func validateServer(server Server) error {
	host, port, err := net.SplitHostPort(server.Addr)
	if err != nil {
		return fmt.Errorf("server.addr %q must be host:port: %w", server.Addr, err)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("server.addr %q has an invalid port", server.Addr)
	}
	if !isLoopback(host) && server.Token == "" {
		return fmt.Errorf("server.addr %q is not loopback: server.token must be set", server.Addr)
	}
	return nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

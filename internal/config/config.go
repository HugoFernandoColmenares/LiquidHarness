// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package config loads, merges, validates and exposes the harness configuration.
//
// Precedence, highest first: flags, process environment, optional .env file, project config
// (<workspace>/.liquidharness/config.toml), user config (OS config dir) and compiled defaults.
// Secrets are never read from TOML: the model API key and the server token come from the
// environment or the OS keyring.
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/liquidharness/liquidharness/internal/policy"
)

// CurrentVersion is the configuration schema version understood by this binary.
const CurrentVersion = 1

// Config is the fully merged and validated configuration.
type Config struct {
	ConfigVersion int     `toml:"config_version" json:"config_version"`
	General       General `toml:"general" json:"general"`
	Model         Model   `toml:"model" json:"model"`
	Agent         Agent   `toml:"agent" json:"agent"`
	Policy        Policy  `toml:"policy" json:"policy"`
	Tools         Tools   `toml:"tools" json:"tools"`
	RAG           RAG     `toml:"rag" json:"rag"`
	Memory        Memory  `toml:"memory" json:"memory"`
	MCP           MCP     `toml:"mcp" json:"mcp"`
	Storage       Storage `toml:"storage" json:"storage"`
	UI            UI      `toml:"ui" json:"ui"`
	Server        Server  `toml:"server" json:"server"`
}

// General holds process-wide settings.
type General struct {
	Profile   string `toml:"profile" json:"profile"`
	Workspace string `toml:"workspace" json:"workspace"`
	Language  string `toml:"language" json:"language"`
	LogLevel  string `toml:"log_level" json:"log_level"`
	LogFormat string `toml:"log_format" json:"log_format"`
	Telemetry string `toml:"telemetry" json:"telemetry"`
}

// Model describes the OpenAI-compatible backend that serves LFM2.5.
type Model struct {
	Provider          string   `toml:"provider" json:"provider"`
	BaseURL           string   `toml:"base_url" json:"base_url"`
	Name              string   `toml:"name" json:"name"`
	APIKey            string   `toml:"-" json:"api_key,omitempty"`
	ContextWindow     int      `toml:"context_window" json:"context_window"`
	MaxOutputTokens   int      `toml:"max_output_tokens" json:"max_output_tokens"`
	Temperature       float64  `toml:"temperature" json:"temperature"`
	TopK              int      `toml:"top_k" json:"top_k"`
	RepetitionPenalty float64  `toml:"repetition_penalty" json:"repetition_penalty"`
	Timeout           Duration `toml:"timeout" json:"timeout"`
	Autostart         bool     `toml:"autostart" json:"autostart"`
}

// Agent bounds a single turn.
type Agent struct {
	MaxSteps              int      `toml:"max_steps" json:"max_steps"`
	MaxToolCalls          int      `toml:"max_tool_calls" json:"max_tool_calls"`
	MaxTurnDuration       Duration `toml:"max_turn_duration" json:"max_turn_duration"`
	ParallelReadOnlyTools int      `toml:"parallel_readonly_tools" json:"parallel_readonly_tools"`
}

// Policy is the security posture. It is intentionally small: every new field is a new
// capability the model can exercise.
type Policy struct {
	Mode          policy.Mode `toml:"mode" json:"mode"`
	AllowShell    bool        `toml:"allow_shell" json:"allow_shell"`
	AllowNetwork  bool        `toml:"allow_network" json:"allow_network"`
	MaxWriteBytes int64       `toml:"max_write_bytes" json:"max_write_bytes"`
}

// Tools restricts which tools are registered.
type Tools struct {
	Allowlist []string `toml:"allowlist" json:"allowlist"`
}

// RAG configures local retrieval.
type RAG struct {
	Enabled        bool   `toml:"enabled" json:"enabled"`
	ChunkTokens    int    `toml:"chunk_tokens" json:"chunk_tokens"`
	ChunkOverlap   int    `toml:"chunk_overlap" json:"chunk_overlap"`
	TopK           int    `toml:"top_k" json:"top_k"`
	MaxTokens      int    `toml:"max_tokens" json:"max_tokens"`
	EmbeddingModel string `toml:"embedding_model" json:"embedding_model"`
}

// Memory configures long-term memory.
type Memory struct {
	Enabled bool `toml:"enabled" json:"enabled"`
}

// MCP configures external Model Context Protocol servers.
type MCP struct {
	Enabled    bool   `toml:"enabled" json:"enabled"`
	ConfigPath string `toml:"config" json:"config"`
}

// Storage configures the local database.
type Storage struct {
	DataDir       string `toml:"data_dir" json:"data_dir"`
	DBPath        string `toml:"db_path" json:"db_path"`
	RetentionDays int    `toml:"retention_days" json:"retention_days"`
}

// UI configures the terminal interface.
type UI struct {
	Theme      string `toml:"theme" json:"theme"`
	Animations bool   `toml:"animations" json:"animations"`
	Mouse      bool   `toml:"mouse" json:"mouse"`
}

// Server configures the local HTTP API and MCP server.
type Server struct {
	Addr  string `toml:"addr" json:"addr"`
	Token string `toml:"token" json:"token,omitempty"`
}

// Duration is a time.Duration that encodes as a TOML/JSON string such as "120s".
type Duration time.Duration

// Std returns the standard library duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// String implements fmt.Stringer.
func (d Duration) String() string { return time.Duration(d).String() }

// MarshalText implements encoding.TextMarshaler.
func (d Duration) MarshalText() ([]byte, error) { return []byte(time.Duration(d).String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", string(text), err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalJSON renders the duration as a string.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// UnmarshalJSON accepts a duration string.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	return d.UnmarshalText([]byte(text))
}

// Redacted returns a copy safe to print: secrets are replaced by a marker.
func (c Config) Redacted() Config {
	if c.Model.APIKey != "" {
		c.Model.APIKey = "***"
	}
	if c.Server.Token != "" {
		c.Server.Token = "***"
	}
	return c
}

// EncodeTOML writes the configuration as TOML.
func (c Config) EncodeTOML(w io.Writer) error {
	encoder := toml.NewEncoder(w)
	encoder.Indent = "  "
	return encoder.Encode(c.Redacted())
}

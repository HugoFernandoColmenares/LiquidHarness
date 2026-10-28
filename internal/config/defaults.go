// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/policy"
)

// Defaults returns the compiled defaults. They are safe: no shell, no network, ask before
// writes, telemetry off.
func Defaults() Config {
	return Config{
		ConfigVersion: CurrentVersion,
		General: General{
			Profile:   "balanced",
			Workspace: ".",
			Language:  "en",
			LogLevel:  "info",
			LogFormat: "text",
			Telemetry: "off",
		},
		Model: Model{
			Provider:          "openai-compatible",
			BaseURL:           "http://127.0.0.1:8080/v1",
			Name:              "lfm2.5-2.6b",
			ContextWindow:     32768,
			MaxOutputTokens:   2048,
			Temperature:       0.1,
			TopK:              50,
			RepetitionPenalty: 1.1,
			Timeout:           Duration(120 * time.Second),
			Autostart:         true,
		},
		Agent: Agent{
			MaxSteps:              24,
			MaxToolCalls:          32,
			MaxTurnDuration:       Duration(10 * time.Minute),
			ParallelReadOnlyTools: 4,
		},
		Policy: Policy{
			Mode:          policy.ModeAsk,
			AllowShell:    false,
			AllowNetwork:  false,
			MaxWriteBytes: 1 << 20,
		},
		Tools: Tools{
			Allowlist: []string{},
		},
		RAG: RAG{
			Enabled:        true,
			ChunkTokens:    512,
			ChunkOverlap:   64,
			TopK:           6,
			MaxTokens:      3000,
			EmbeddingModel: "bge-m3-Q5_K_M.gguf",
		},
		Memory: Memory{Enabled: true},
		MCP:    MCP{Enabled: false},
		Storage: Storage{
			RetentionDays: 180,
		},
		UI: UI{
			Theme:      "liquid-dark",
			Animations: true,
			Mouse:      false,
		},
		Server: Server{
			Addr: "127.0.0.1:8787",
		},
	}
}

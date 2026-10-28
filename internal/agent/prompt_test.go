// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

func TestPromptHashIsStableAndSensitive(t *testing.T) {
	options := PromptOptions{Workspace: "/workspace", Policy: "ask", Language: "en"}
	prompt := SystemPrompt(options)
	hash := PromptHash(prompt)

	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("hash = %q", hash)
	}
	if PromptHash(SystemPrompt(options)) != hash {
		t.Error("the same prompt produced a different hash")
	}

	changed := SystemPrompt(PromptOptions{Workspace: "/workspace", Policy: "allow", Language: "en"})
	if PromptHash(changed) == hash {
		t.Error("a changed prompt produced the same hash")
	}
}

func TestSystemPromptRulesAndTools(t *testing.T) {
	prompt := SystemPrompt(PromptOptions{})
	if !strings.Contains(prompt, "untrusted") {
		t.Error("missing the untrusted-content rule")
	}
	if strings.Contains(prompt, "<|tool_call_start|>") {
		t.Error("tool protocol leaked into a prompt without tools")
	}

	prompt = SystemPrompt(PromptOptions{
		Tools: []core.ToolSpec{{Name: "fs_read", Description: "Read a file"}},
	})
	if !strings.Contains(prompt, "List of tools:") {
		t.Error("missing the tool list")
	}
	if !strings.Contains(prompt, "<|tool_call_start|>") || !strings.Contains(prompt, "<|tool_call_end|>") {
		t.Error("missing the tool-call protocol")
	}
	if !strings.Contains(prompt, "fs_read") {
		t.Error("missing the tool name")
	}
}

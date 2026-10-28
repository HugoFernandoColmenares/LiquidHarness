// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/app"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/config"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/llm"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

func newAppWithModel(t *testing.T, replies ...string) (*app.App, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	workspace := t.TempDir()
	base := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	a := app.New(app.Options{
		Stdout:    stdout,
		Stderr:    stderr,
		LookupEnv: func(string) (string, bool) { return "", false },
		Getwd:     func() (string, error) { return workspace, nil },
		PathOptions: config.PathOptions{
			GOOS:          "linux",
			Getenv:        func(string) string { return "" },
			UserConfigDir: func() (string, error) { return filepath.Join(base, "config"), nil },
			UserCacheDir:  func() (string, error) { return filepath.Join(base, "cache"), nil },
			HomeDir:       func() (string, error) { return filepath.Join(base, "home"), nil },
		},
		NewModel: func(config.Model) (ports.Model, error) {
			return llm.NewFake("fake", replies...), nil
		},
	})
	if err := a.Configure(config.Flags{}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	return a, stdout, stderr, workspace
}

func TestRunTurnUsesReadOnlyTools(t *testing.T) {
	a, stdout, stderr, workspace := newAppWithModel(t,
		`<|tool_call_start|>[fs_read(path="README.md")]<|tool_call_end|>`,
		"The objective is to test the harness.",
	)
	if err := os.WriteFile(filepath.Join(workspace, "README.md"),
		[]byte("# Demo\nThe objective is to test the harness.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := a.RunTurn(ctx, app.RunOptions{Prompt: "read the README"})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if result.StopReason != "answer" {
		t.Errorf("stop reason = %q", result.StopReason)
	}
	if result.Steps != 2 || result.ToolCalls != 1 {
		t.Errorf("steps = %d, tool calls = %d", result.Steps, result.ToolCalls)
	}
	if result.Answer != "The objective is to test the harness." {
		t.Errorf("answer = %q", result.Answer)
	}
	if !strings.HasPrefix(result.PromptHash, "sha256:") {
		t.Errorf("prompt hash = %q", result.PromptHash)
	}
	if !strings.Contains(stdout.String(), "The objective is to test the harness.") {
		t.Errorf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "tool fs_read ok") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunTurnJSONKeepsStdoutClean(t *testing.T) {
	a, stdout, _, workspace := newAppWithModel(t,
		`<|tool_call_start|>[fs_list(path=".")]<|tool_call_end|>`,
		"done",
	)
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.SetJSON(true)

	if _, err := a.RunTurn(context.Background(), app.RunOptions{Prompt: "list"}); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout is not empty in JSON mode: %q", stdout.String())
	}
}

func TestRunTurnRejectsEscape(t *testing.T) {
	a, _, stderr, _ := newAppWithModel(t,
		`<|tool_call_start|>[fs_read(path="../../etc/passwd")]<|tool_call_end|>`,
		"I cannot read that file.",
	)

	if _, err := a.RunTurn(context.Background(), app.RunOptions{Prompt: "read outside"}); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if !strings.Contains(stderr.String(), "failed") {
		t.Errorf("stderr = %q, want a failed tool line", stderr.String())
	}
}

func TestRunTurnRequiresPrompt(t *testing.T) {
	a, _, _, _ := newAppWithModel(t)
	if _, err := a.RunTurn(context.Background(), app.RunOptions{Prompt: "  "}); !core.Is(err, core.CodeUsage) {
		t.Fatalf("error = %v, want USAGE", err)
	}
}

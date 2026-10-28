// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/llm"
)

func TestRunPrintUsesAgentLoop(t *testing.T) {
	a := newTestAppWithModel(t, llm.NewFake("fake", "hola desde el loop"))

	output, err := execute(t, a, "run", "--print", "di hola")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output, "hola desde el loop") {
		t.Errorf("output = %q", output)
	}
}

func TestRunPrintRequiresPrompt(t *testing.T) {
	a := newTestAppWithModel(t, llm.NewFake("fake", "ok"))

	_, err := execute(t, a, "run", "--print", "  ")
	if !core.Is(err, core.CodeUsage) {
		t.Fatalf("error = %v, want USAGE", err)
	}
}

func TestRunRejectsConflictingApprovalFlags(t *testing.T) {
	a := newTestAppWithModel(t, llm.NewFake("fake", "ok"))

	_, err := execute(t, a, "run", "--print", "hi", "--yes", "--deny")
	if !core.Is(err, core.CodeUsage) {
		t.Fatalf("error = %v, want USAGE", err)
	}
}

func TestRunValidatesAllowRisk(t *testing.T) {
	a := newTestAppWithModel(t, llm.NewFake("fake", "ok"))

	for _, value := range []string{"R0", "R5", "R9", "high", "2"} {
		_, err := execute(t, a, "run", "--print", "hi", "--allow-risk", value)
		if !core.Is(err, core.CodeUsage) {
			t.Errorf("--allow-risk %q: error = %v, want USAGE", value, err)
		}
	}
}

func TestRunAcceptsAllowRisk(t *testing.T) {
	a := newTestAppWithModel(t, llm.NewFake("fake", "ok"))

	output, err := execute(t, a, "run", "--print", "hi", "--allow-risk", "R2")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output, "ok") {
		t.Errorf("output = %q", output)
	}
}

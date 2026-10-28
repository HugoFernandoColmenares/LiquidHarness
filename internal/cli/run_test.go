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

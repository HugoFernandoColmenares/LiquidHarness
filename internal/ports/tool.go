// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package ports

import (
	"context"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

// Tool is one executable capability exposed to the model. Implementations validate their own
// arguments and never panic: a bad call is a ToolResult with Error set, not a crashed process.
type Tool interface {
	// Spec is the static description sent to the model and used by policy.
	Spec() core.ToolSpec
	// Execute runs the call. A returned error means the harness itself failed; an expected
	// failure (missing file, bad argument, denied path) belongs in ToolResult.Error.
	Execute(ctx context.Context, call core.ToolCall) (core.ToolResult, error)
}

// ToolRegistry is the read-only catalog of tools available in a turn. Implementations are
// safe for concurrent reads after construction.
type ToolRegistry interface {
	// Get returns the tool registered under name.
	Get(name string) (Tool, bool)
	// Specs returns the specs sorted by name, so prompts and evals are deterministic.
	Specs() []core.ToolSpec
	// Names returns the registered names sorted alphabetically.
	Names() []string
}

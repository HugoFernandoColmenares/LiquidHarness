// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package ports

import (
	"context"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

// Emitter receives the ordered events of a turn. The agent loop emits from a single goroutine,
// so implementations never observe concurrent calls, but they must not block for long: the
// loop waits for Emit to return before it continues.
type Emitter interface {
	Emit(ctx context.Context, event core.Event) error
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrBudgetSteps means the turn used all its model calls.
	ErrBudgetSteps = errors.New("step budget exhausted")
	// ErrBudgetTools means the turn used all its tool calls.
	ErrBudgetTools = errors.New("tool-call budget exhausted")
	// ErrBudgetTime means the turn exceeded its wall-clock limit.
	ErrBudgetTime = errors.New("turn deadline exceeded")
)

// Budget bounds one turn. Zero values disable the corresponding limit.
type Budget struct {
	MaxSteps     int
	MaxToolCalls int
	Deadline     time.Time
}

// Check reports whether the turn may continue.
func (b Budget) Check(now time.Time, steps, toolCalls int) error {
	switch {
	case b.MaxSteps > 0 && steps >= b.MaxSteps:
		return fmt.Errorf("%w: %d/%d steps", ErrBudgetSteps, steps, b.MaxSteps)
	case b.MaxToolCalls > 0 && toolCalls >= b.MaxToolCalls:
		return fmt.Errorf("%w: %d/%d calls", ErrBudgetTools, toolCalls, b.MaxToolCalls)
	case !b.Deadline.IsZero() && now.After(b.Deadline):
		return fmt.Errorf("%w after %s", ErrBudgetTime, now.Sub(b.Deadline).Round(time.Second))
	default:
		return nil
	}
}

// Warning returns the message to inject when the step budget crosses 75% or 90%, and whether a
// new threshold was crossed. Each threshold is reported at most once per turn.
func (b Budget) Warning(turn *Turn, steps int) (string, bool) {
	if b.MaxSteps <= 0 {
		return "", false
	}
	percent := steps * 100 / b.MaxSteps
	threshold := 0
	switch {
	case percent >= 90:
		threshold = 90
	case percent >= 75:
		threshold = 75
	}
	if threshold == 0 || turn.warnedAt[threshold] {
		return "", false
	}
	turn.warnedAt[threshold] = true
	return fmt.Sprintf("[budget] %d/%d steps used. Prioritize a final answer.", steps, b.MaxSteps), true
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package agent implements the turn loop: build context, call the model, parse tool calls,
// authorize them, execute them, persist events and detect loops. It orchestrates ports and the
// policy engine but never imports a concrete adapter.
//
// Planned for F2 (loop and context) and refined in F3 (tools) and F4 (resume).
package agent

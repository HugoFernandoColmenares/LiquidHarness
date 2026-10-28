// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package agent implements the turn loop: build context, call the model, parse tool calls,
// authorize them, execute them, persist events and detect loops. It orchestrates ports and the
// policy engine but never imports a concrete adapter.
//
// F1 provides the versioned system prompt. F2 adds the loop and the context manager; F3 adds
// tools and F4 adds resume.
package agent

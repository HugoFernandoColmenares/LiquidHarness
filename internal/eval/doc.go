// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package eval runs versioned datasets, applies deterministic assertions and an optional judge,
// computes metrics and enforces regression gates against a baseline. Exit code 1 means the gate
// failed; 3 means the run was denied by policy.
//
// Planned for F8, with smoke datasets earlier to protect the agent loop.
package eval

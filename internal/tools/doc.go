// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package tools implements the built-in tool catalog: filesystem reads and writes, search,
// retrieval, memory, todos, ask_user and the opt-in HTTP and shell tools. Every handler
// validates its arguments, resolves paths through fsx.Jail and returns the canonical result
// shape defined in core.
//
// Planned for F3. The registry is the single place where a tool's JSON Schema and risk class
// are declared.
package tools

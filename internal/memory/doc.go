// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package memory decides what the agent remembers between sessions: extraction from finished
// turns, deduplication, decay and user-visible deletion. Memory is local, inspectable and
// disabled per project when the workspace is not trusted.
//
// Planned for F6, on top of the store package.
package memory

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package store persists sessions, messages, tool calls, approvals, audit events, memories,
// documents and eval reports in SQLite. It uses modernc.org/sqlite (CGO-free), FTS5 for lexical
// search and float32 BLOB vectors for semantic search; sqlite-vec is an optional accelerator.
//
// Planned for F4. Migrations are embedded and forward-only; every write is transactional.
package store

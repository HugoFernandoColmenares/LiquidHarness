// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package rag implements local retrieval: walking and filtering the workspace, chunking with
// overlap, embeddings through the local model, hybrid lexical and vector search, and citations
// that point back to exact file offsets.
//
// Planned for F6. Indexing obeys the same denylist as the filesystem tools (.env, keys,
// .ssh, .aws, keychains) and never sends content off the machine.
package rag

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package ports declares the interfaces the domain needs from the outside world: model,
// storage, tools, policy, retrieval, approvals, recording, identifiers and time. Adapters in
// llm, store, tools, rag and mcpclient implement them; core never sees an adapter.
//
// Interfaces land with their first real adapter (F1 onward). Empty interfaces written before a
// consumer exists are worse than no interface at all.
package ports

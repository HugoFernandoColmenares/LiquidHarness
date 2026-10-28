// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package llm contains model adapters: the OpenAI-compatible HTTP client, SSE streaming, wire
// DTOs, the LFM tool-call parser and the llama-server supervisor. The embedded llama.cpp
// backend lives in the llm/embedded subpackage behind the "embedded" build tag.
//
// Planned for F1. Wire types never leak into core or the agent loop.
package llm

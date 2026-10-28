// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package llm contains model adapters: the OpenAI-compatible HTTP client, SSE streaming, wire
// DTOs, the LFM tool-call parser and the llama-server supervisor. The embedded llama.cpp
// backend lives in the llm/embedded subpackage behind the "embedded" build tag.
//
// F1 implements the client, streaming, wire types, parser and fake model. The supervisor and
// the embedded backend land later. Wire types never leak into core or the agent loop.
package llm

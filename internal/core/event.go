// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"encoding/json"
	"time"
)

// EventType names a lifecycle event emitted by the agent loop.
type EventType string

const (
	// EventTurnStarted is emitted when a turn begins.
	EventTurnStarted EventType = "turn.started"
	// EventTurnFinished is emitted when a turn completes or fails.
	EventTurnFinished EventType = "turn.finished"
	// EventModelDelta is emitted for every visible text delta of a model answer.
	EventModelDelta EventType = "model.delta"
	// EventBudgetWarning is emitted when a turn crosses 75% or 90% of its step budget.
	EventBudgetWarning EventType = "budget.warning"
	// EventToolRequested is emitted before policy evaluation.
	EventToolRequested EventType = "tool.requested"
	// EventToolApproved is emitted when a human approves a call.
	EventToolApproved EventType = "tool.approved"
	// EventToolDenied is emitted when policy or a human denies a call.
	EventToolDenied EventType = "tool.denied"
	// EventToolStarted is emitted right before a tool starts executing.
	EventToolStarted EventType = "tool.started"
	// EventToolFinished is emitted after a tool call returns.
	EventToolFinished EventType = "tool.finished"
)

// Event is an auditable fact. Data is tool- or event-specific JSON.
//
// Seq is a per-turn counter that starts at 1 and increases by one for every emitted event.
// Consumers can rely on it to detect dropped or reordered events; it is not globally unique.
type Event struct {
	Seq       uint64          `json:"seq"`
	ID        string          `json:"id"`
	Type      EventType       `json:"type"`
	Time      time.Time       `json:"time"`
	SessionID string          `json:"session_id,omitempty"`
	TurnID    string          `json:"turn_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

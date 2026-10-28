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
	// EventToolRequested is emitted before policy evaluation.
	EventToolRequested EventType = "tool.requested"
	// EventToolApproved is emitted when a human approves a call.
	EventToolApproved EventType = "tool.approved"
	// EventToolDenied is emitted when policy or a human denies a call.
	EventToolDenied EventType = "tool.denied"
	// EventToolFinished is emitted after a tool call returns.
	EventToolFinished EventType = "tool.finished"
)

// Event is an auditable fact. Data is tool- or event-specific JSON.
type Event struct {
	ID        string          `json:"id"`
	Type      EventType       `json:"type"`
	Time      time.Time       `json:"time"`
	SessionID string          `json:"session_id,omitempty"`
	TurnID    string          `json:"turn_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

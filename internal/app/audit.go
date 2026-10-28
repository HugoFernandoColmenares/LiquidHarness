// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/store"
)

// auditEmitter forwards every event to the renderer and writes one audit record per tool call
// when the call is denied or finishes. Audit failures are logged and never abort a turn: losing
// the log must not lose the work.
type auditEmitter struct {
	render ports.Emitter
	log    *store.AuditLog
	// report receives audit write failures; nil disables reporting.
	report func(error)

	mu      sync.Mutex
	pending map[string]pendingCall
}

type pendingCall struct {
	sessionID     string
	turnID        string
	tool          string
	risk          string
	argsHash      string
	argsRedacted  map[string]any
	decision      string
	rule          string
	approvedBy    string
	approvalScope string
}

func newAuditEmitter(render ports.Emitter, log *store.AuditLog, report func(error)) *auditEmitter {
	return &auditEmitter{
		render:  render,
		log:     log,
		report:  report,
		pending: make(map[string]pendingCall),
	}
}

// Emit implements ports.Emitter.
func (e *auditEmitter) Emit(ctx context.Context, event core.Event) error {
	if err := e.render.Emit(ctx, event); err != nil {
		return err
	}
	if e.log == nil {
		return nil
	}
	e.record(event)
	return nil
}

func (e *auditEmitter) record(event core.Event) {
	switch event.Type {
	case core.EventToolRequested:
		var data struct {
			CallID        string          `json:"call_id"`
			Name          string          `json:"name"`
			Risk          string          `json:"risk"`
			Arguments     json.RawMessage `json:"arguments"`
			ArgumentsHash string          `json:"args_hash"`
			Decision      string          `json:"decision"`
			Rule          string          `json:"rule"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			e.fail(err)
			return
		}
		e.mu.Lock()
		e.pending[data.CallID] = pendingCall{
			sessionID:    event.SessionID,
			turnID:       event.TurnID,
			tool:         data.Name,
			risk:         data.Risk,
			argsHash:     data.ArgumentsHash,
			argsRedacted: redactArguments(data.Arguments),
			decision:     data.Decision,
			rule:         data.Rule,
		}
		e.mu.Unlock()

	case core.EventToolApproved:
		var data struct {
			CallID     string `json:"call_id"`
			ApprovedBy string `json:"approved_by"`
			Scope      string `json:"approval_scope"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			e.fail(err)
			return
		}
		e.update(data.CallID, func(item *pendingCall) {
			item.decision = string(ports.OutcomeAllow)
			item.approvedBy = data.ApprovedBy
			item.approvalScope = data.Scope
		})

	case core.EventToolDenied:
		var data struct {
			CallID string `json:"call_id"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			e.fail(err)
			return
		}
		e.finish(data.CallID, func(record *store.AuditRecord) {
			record.Decision = string(ports.OutcomeDeny)
			record.Result = "denied"
			record.ErrorCode = "POLICY_DENIED"
		})

	case core.EventToolFinished:
		var data struct {
			CallID string          `json:"call_id"`
			Result core.ToolResult `json:"result"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			e.fail(err)
			return
		}
		e.finish(data.CallID, func(record *store.AuditRecord) {
			record.DurationMS = data.Result.Meta.DurationMS
			record.BytesOut = data.Result.Meta.Bytes
			if data.Result.OK {
				record.Result = "ok"
				return
			}
			record.Result = "error"
			if data.Result.Error != nil {
				record.ErrorCode = data.Result.Error.Code
			}
		})
	default:
		// turn.*, model.delta, budget.warning and tool.started carry no audit record of their
		// own; the per-call record is written when the call is denied or finishes.
	}
}

// update mutates the pending call without writing a record.
func (e *auditEmitter) update(callID string, mutate func(*pendingCall)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	item, ok := e.pending[callID]
	if !ok {
		return
	}
	mutate(&item)
	e.pending[callID] = item
}

// finish writes the record and forgets the call.
func (e *auditEmitter) finish(callID string, mutate func(*store.AuditRecord)) {
	e.mu.Lock()
	item, ok := e.pending[callID]
	delete(e.pending, callID)
	e.mu.Unlock()
	if !ok {
		return
	}
	record := store.AuditRecord{
		SessionID:     item.sessionID,
		TurnID:        item.turnID,
		CallID:        callID,
		Tool:          item.tool,
		Risk:          item.risk,
		ArgsHash:      item.argsHash,
		ArgsRedacted:  item.argsRedacted,
		Decision:      item.decision,
		Rule:          item.rule,
		ApprovedBy:    item.approvedBy,
		ApprovalScope: item.approvalScope,
	}
	mutate(&record)
	if err := e.log.Append(record); err != nil {
		e.fail(err)
	}
}

func (e *auditEmitter) fail(err error) {
	if e.report != nil {
		e.report(err)
	}
}

// redactArguments keeps scalar arguments and replaces content-bearing fields with their size,
// so the audit log shows what a call targeted without storing file bodies.
func redactArguments(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		switch key {
		case "content", "old", "new", "text":
			if text, ok := value.(string); ok {
				out[key+"_bytes"] = len(text)
				continue
			}
			out[key] = value
		default:
			out[key] = summarize(value)
		}
	}
	return out
}

func summarize(value any) any {
	switch typed := value.(type) {
	case string:
		return truncate(typed, 200)
	case float64, bool, nil:
		return typed
	case []any:
		return len(typed)
	case map[string]any:
		return len(typed)
	default:
		return fmt.Sprintf("%T", value)
	}
}

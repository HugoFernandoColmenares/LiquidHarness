// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/agent"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

// askPolicy asks for every call.
type askPolicy struct{}

func (askPolicy) Decide(core.ToolSpec, core.RiskClass) ports.Decision {
	return ports.Decision{Outcome: ports.OutcomeAsk, Rule: "test-ask", Reason: "needs approval"}
}

// recordingPolicy remembers the risk it was asked about.
type recordingPolicy struct {
	risk core.RiskClass
}

func (p *recordingPolicy) Decide(_ core.ToolSpec, risk core.RiskClass) ports.Decision {
	p.risk = risk
	return ports.Decision{Outcome: ports.OutcomeAllow, Rule: "test-allow"}
}

// approverStub answers with a fixed decision and records the requests it saw.
type approverStub struct {
	decision ports.ApprovalDecision
	err      error

	mu       sync.Mutex
	requests []ports.ApprovalRequest
}

func (a *approverStub) Approve(_ context.Context, request ports.ApprovalRequest) (ports.ApprovalDecision, error) {
	a.mu.Lock()
	a.requests = append(a.requests, request)
	a.mu.Unlock()
	return a.decision, a.err
}

func (a *approverStub) Requests() []ports.ApprovalRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]ports.ApprovalRequest{}, a.requests...)
}

// escalatingTool reports a higher risk than its spec.
type escalatingTool struct {
	stubTool
	escalated core.RiskClass
}

func (t *escalatingTool) RiskFor(core.ToolCall) core.RiskClass { return t.escalated }

func findEvent(t *testing.T, events []core.Event, kind core.EventType) core.Event {
	t.Helper()
	for _, event := range events {
		if event.Type == kind {
			return event
		}
	}
	t.Fatalf("no %s event in %v", kind, eventTypes(events))
	return core.Event{}
}

func eventTypes(events []core.Event) []core.EventType {
	types := make([]core.EventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func TestPolicyDenyFeedsErrorBackWithoutRunning(t *testing.T) {
	var runs atomic.Int64
	read := &stubTool{name: "fs_read", risk: core.RiskR0, run: func(_ context.Context, call core.ToolCall) (core.ToolResult, error) {
		runs.Add(1)
		return core.ToolResult{CallID: call.ID, OK: true}, nil
	}}
	model := &scriptedModel{steps: []scriptStep{
		{calls: []core.ToolCall{toolCall("call-1", "fs_read", `{"path":"README.md"}`)}},
		{text: "recovered"},
	}}
	log := &eventLog{}
	loop := newLoopWith(t, model, newRegistry(t, read), log, denyAll{}, nil, nil)

	result, err := loop.Run(context.Background(), "session", "read it")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.StopReason != agent.StopAnswer || result.Answer != "recovered" {
		t.Fatalf("result = %+v", result)
	}
	if runs.Load() != 0 {
		t.Errorf("denied tool executed %d times", runs.Load())
	}

	events := log.Events()
	findEvent(t, events, core.EventToolRequested)
	denied := findEvent(t, events, core.EventToolDenied)
	var deniedData struct {
		Rule   string `json:"rule"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(denied.Data, &deniedData); err != nil {
		t.Fatal(err)
	}
	if deniedData.Rule != "test-deny" || !strings.Contains(deniedData.Reason, "denied by test") {
		t.Errorf("denied data = %+v", deniedData)
	}
	for _, event := range events {
		if event.Type == core.EventToolStarted || event.Type == core.EventToolFinished {
			t.Errorf("denied call emitted %s", event.Type)
		}
	}

	requests := model.Requests()
	last := requests[len(requests)-1]
	found := false
	for _, message := range last.Messages {
		if message.Role == core.RoleTool && strings.Contains(message.Content, "POLICY_DENIED") {
			found = true
		}
	}
	if !found {
		t.Errorf("model did not receive POLICY_DENIED: %+v", last.Messages)
	}
}

func TestAskWithoutApproverDenies(t *testing.T) {
	var runs atomic.Int64
	write := &stubTool{name: "fs_create", risk: core.RiskR1, run: func(_ context.Context, call core.ToolCall) (core.ToolResult, error) {
		runs.Add(1)
		return core.ToolResult{CallID: call.ID, OK: true}, nil
	}}
	model := &scriptedModel{steps: []scriptStep{
		{calls: []core.ToolCall{toolCall("call-1", "fs_create", `{"path":"a.txt","content":"x"}`)}},
		{text: "denied"},
	}}
	log := &eventLog{}
	loop := newLoopWith(t, model, newRegistry(t, write), log, askPolicy{}, nil, nil)

	if _, err := loop.Run(context.Background(), "", "write"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runs.Load() != 0 {
		t.Error("tool ran without an approver")
	}
	denied := findEvent(t, log.Events(), core.EventToolDenied)
	if !strings.Contains(string(denied.Data), "no approver") {
		t.Errorf("denied data = %s", denied.Data)
	}
}

func TestApprovalRunsTheCall(t *testing.T) {
	var runs atomic.Int64
	write := &stubTool{name: "fs_create", risk: core.RiskR1, run: func(_ context.Context, call core.ToolCall) (core.ToolResult, error) {
		runs.Add(1)
		return core.ToolResult{
			CallID: call.ID,
			OK:     true,
			Data:   json.RawMessage(`{"path":"a.txt"}`),
		}, nil
	}}
	approver := &approverStub{decision: ports.ApprovalDecision{
		Approved:   true,
		Scope:      "session",
		ApprovedBy: "user",
	}}
	model := &scriptedModel{steps: []scriptStep{
		{calls: []core.ToolCall{toolCall("call-1", "fs_create", `{"path":"a.txt","content":"x"}`)}},
		{text: "written"},
	}}
	log := &eventLog{}
	loop := newLoopWith(t, model, newRegistry(t, write), log, askPolicy{}, approver, nil)

	result, err := loop.Run(context.Background(), "session", "write")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.StopReason != agent.StopAnswer {
		t.Errorf("stop reason = %s", result.StopReason)
	}
	if runs.Load() != 1 {
		t.Errorf("tool ran %d times, want 1", runs.Load())
	}
	requests := approver.Requests()
	if len(requests) != 1 {
		t.Fatalf("approver saw %d requests, want 1", len(requests))
	}
	if requests[0].Tool != "fs_create" || requests[0].Risk != core.RiskR1 {
		t.Errorf("request = %+v", requests[0])
	}

	events := log.Events()
	approved := findEvent(t, events, core.EventToolApproved)
	var approvedData struct {
		ApprovedBy string `json:"approved_by"`
		Scope      string `json:"approval_scope"`
	}
	if err := json.Unmarshal(approved.Data, &approvedData); err != nil {
		t.Fatal(err)
	}
	if approvedData.ApprovedBy != "user" || approvedData.Scope != "session" {
		t.Errorf("approved data = %+v", approvedData)
	}
	findEvent(t, events, core.EventToolStarted)
	findEvent(t, events, core.EventToolFinished)
}

func TestApprovalDenialDoesNotRun(t *testing.T) {
	var runs atomic.Int64
	write := &stubTool{name: "fs_delete", risk: core.RiskR2, run: func(_ context.Context, call core.ToolCall) (core.ToolResult, error) {
		runs.Add(1)
		return core.ToolResult{CallID: call.ID, OK: true}, nil
	}}
	approver := &approverStub{}
	model := &scriptedModel{steps: []scriptStep{
		{calls: []core.ToolCall{toolCall("call-1", "fs_delete", `{"path":"a.txt"}`)}},
		{text: "canceled"},
	}}
	log := &eventLog{}
	loop := newLoopWith(t, model, newRegistry(t, write), log, askPolicy{}, approver, nil)

	if _, err := loop.Run(context.Background(), "", "delete"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runs.Load() != 0 {
		t.Error("denied tool ran")
	}
	denied := findEvent(t, log.Events(), core.EventToolDenied)
	if !strings.Contains(string(denied.Data), "user denied") {
		t.Errorf("denied data = %s", denied.Data)
	}
}

func TestApprovedCallsRunSequentially(t *testing.T) {
	var active, peak atomic.Int64
	read := &stubTool{name: "fs_read", risk: core.RiskR1, active: &active, peak: &peak}
	approver := &approverStub{decision: ports.ApprovalDecision{Approved: true}}
	model := &scriptedModel{steps: []scriptStep{
		{calls: []core.ToolCall{
			toolCall("call-1", "fs_read", `{"path":"a"}`),
			toolCall("call-2", "fs_read", `{"path":"b"}`),
		}},
		{text: "done"},
	}}
	loop := newLoopWith(t, model, newRegistry(t, read), nil, askPolicy{}, approver, nil)

	if _, err := loop.Run(context.Background(), "", "read both"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if peak.Load() > 1 {
		t.Errorf("approved calls ran in parallel (peak %d)", peak.Load())
	}
	if len(approver.Requests()) != 2 {
		t.Errorf("approver saw %d requests, want 2", len(approver.Requests()))
	}
}

func TestRiskAssessorEscalates(t *testing.T) {
	tool := &escalatingTool{
		stubTool:  stubTool{name: "fs_create", risk: core.RiskR1},
		escalated: core.RiskR2,
	}
	policy := &recordingPolicy{}
	model := &scriptedModel{steps: []scriptStep{
		{calls: []core.ToolCall{toolCall("call-1", "fs_create", `{"path":"a.txt","overwrite":true}`)}},
		{text: "done"},
	}}
	log := &eventLog{}
	loop := newLoopWith(t, model, newRegistry(t, tool), log, policy, nil, nil)

	if _, err := loop.Run(context.Background(), "", "overwrite"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if policy.risk != core.RiskR2 {
		t.Errorf("policy saw risk %s, want R2", policy.risk)
	}
	requested := findEvent(t, log.Events(), core.EventToolRequested)
	var data struct {
		Risk     string `json:"risk"`
		Rule     string `json:"rule"`
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(requested.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Risk != "R2" || data.Decision != "allow" {
		t.Errorf("requested data = %+v", data)
	}
}

func TestApproverErrorIsInternal(t *testing.T) {
	read := &stubTool{name: "fs_create", risk: core.RiskR1}
	approver := &approverStub{err: errors.New("terminal gone")}
	model := &scriptedModel{steps: []scriptStep{
		{calls: []core.ToolCall{toolCall("call-1", "fs_create", `{"path":"a.txt"}`)}},
	}}
	loop := newLoopWith(t, model, newRegistry(t, read), nil, askPolicy{}, approver, nil)

	_, err := loop.Run(context.Background(), "", "write")
	if err == nil {
		t.Fatal("Run succeeded despite an approver error")
	}
	if !core.Is(err, core.CodeInternal) {
		t.Errorf("error = %v, want INTERNAL", err)
	}
}

func TestApproverContextCancellationIsCanceled(t *testing.T) {
	read := &stubTool{name: "fs_create", risk: core.RiskR1}
	approver := &approverStub{err: context.Canceled}
	model := &scriptedModel{steps: []scriptStep{
		{calls: []core.ToolCall{toolCall("call-1", "fs_create", `{"path":"a.txt"}`)}},
	}}
	loop := newLoopWith(t, model, newRegistry(t, read), nil, askPolicy{}, approver, nil)

	_, err := loop.Run(context.Background(), "", "write")
	if err == nil {
		t.Fatal("Run succeeded despite cancellation")
	}
	if !core.Is(err, core.CodeCanceled) {
		t.Errorf("error = %v, want CANCELED", err)
	}
}

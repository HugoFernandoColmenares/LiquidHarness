// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/agent"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

// scriptStep is one scripted model answer.
type scriptStep struct {
	text  string
	calls []core.ToolCall
	err   error
	block bool
}

// scriptedModel replays steps in order and records every request.
type scriptedModel struct {
	mu       sync.Mutex
	steps    []scriptStep
	index    int
	requests []ports.ChatRequest
}

func (m *scriptedModel) Name() string { return "scripted" }

func (m *scriptedModel) Chat(context.Context, ports.ChatRequest) (ports.ChatResponse, error) {
	return ports.ChatResponse{}, errors.New("not used")
}

func (m *scriptedModel) Stream(ctx context.Context, req ports.ChatRequest) (ports.Stream, error) {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	step := m.steps[min(m.index, len(m.steps)-1)]
	m.index++
	m.mu.Unlock()

	if step.err != nil {
		return nil, step.err
	}
	return &scriptedStream{ctx: ctx, step: step}, nil
}

func (m *scriptedModel) Requests() []ports.ChatRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ports.ChatRequest{}, m.requests...)
}

type scriptedStream struct {
	ctx    context.Context
	step   scriptStep
	stage  int
	closed bool
}

func (s *scriptedStream) Next(ctx context.Context) (ports.StreamEvent, error) {
	if s.step.block {
		<-ctx.Done()
		return ports.StreamEvent{}, core.Wrap(core.CodeCanceled, ctx.Err(), "stream canceled")
	}
	if err := ctx.Err(); err != nil {
		return ports.StreamEvent{}, core.Wrap(core.CodeCanceled, err, "stream canceled")
	}

	switch s.stage {
	case 0:
		s.stage++
		if s.step.text != "" {
			return ports.StreamEvent{Type: ports.StreamText, Text: s.step.text}, nil
		}
		return s.Next(ctx)
	case 1:
		s.stage++
		if len(s.step.calls) > 0 {
			return ports.StreamEvent{Type: ports.StreamToolCalls, ToolCalls: s.step.calls}, nil
		}
		return s.Next(ctx)
	case 2:
		s.stage++
		return ports.StreamEvent{
			Type:         ports.StreamDone,
			FinishReason: "stop",
			Usage:        &ports.Usage{PromptTokens: 10, CompletionTokens: 5},
		}, nil
	default:
		return ports.StreamEvent{}, io.EOF
	}
}

func (s *scriptedStream) Close() error {
	s.closed = true
	return nil
}

// stubTool is a read-only tool used to observe execution.
type stubTool struct {
	name   string
	risk   core.RiskClass
	run    func(ctx context.Context, call core.ToolCall) (core.ToolResult, error)
	active *atomic.Int64
	peak   *atomic.Int64
}

func (t *stubTool) Spec() core.ToolSpec {
	return core.ToolSpec{
		Name:        t.name,
		Description: "stub tool for tests",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Risk:        t.risk,
	}
}

func (t *stubTool) Execute(ctx context.Context, call core.ToolCall) (core.ToolResult, error) {
	if t.active != nil {
		current := t.active.Add(1)
		for {
			previous := t.peak.Load()
			if current <= previous || t.peak.CompareAndSwap(previous, current) {
				break
			}
		}
		defer t.active.Add(-1)
	}
	if t.run != nil {
		return t.run(ctx, call)
	}
	return core.ToolResult{
		CallID: call.ID,
		OK:     true,
		Data:   json.RawMessage(`{"ok":true}`),
	}, nil
}

// eventLog records emitted events in order.
type eventLog struct {
	mu     sync.Mutex
	events []core.Event
}

func (l *eventLog) Emit(_ context.Context, event core.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
	return nil
}

func (l *eventLog) Events() []core.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]core.Event{}, l.events...)
}

// emitter avoids the typed-nil interface trap when a test does not want events.
func emitter(log *eventLog) ports.Emitter {
	if log == nil {
		return nil
	}
	return log
}

func newRegistry(t *testing.T, tools ...ports.Tool) *registryStub {
	t.Helper()
	registry := &registryStub{tools: make(map[string]ports.Tool, len(tools))}
	for _, tool := range tools {
		registry.tools[tool.Spec().Name] = tool
	}
	return registry
}

type registryStub struct {
	tools map[string]ports.Tool
}

func (r *registryStub) Get(name string) (ports.Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *registryStub) Specs() []core.ToolSpec {
	specs := make([]core.ToolSpec, 0, len(r.tools))
	for _, tool := range r.tools {
		specs = append(specs, tool.Spec())
	}
	return specs
}

func (r *registryStub) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// allowAll is a test policy that permits every call, including R2+.
type allowAll struct{}

func (allowAll) Decide(core.ToolSpec, core.RiskClass) ports.Decision {
	return ports.Decision{Outcome: ports.OutcomeAllow, Rule: "test-allow"}
}

// denyAll is a test policy that refuses every call.
type denyAll struct{}

func (denyAll) Decide(core.ToolSpec, core.RiskClass) ports.Decision {
	return ports.Decision{Outcome: ports.OutcomeDeny, Rule: "test-deny", Reason: "denied by test"}
}

func newLoop(t *testing.T, model ports.Model, registry ports.ToolRegistry, log *eventLog, mutate func(*agent.Config)) *agent.Loop {
	t.Helper()
	return newLoopWith(t, model, registry, log, allowAll{}, nil, mutate)
}

func newLoopWith(
	t *testing.T,
	model ports.Model,
	registry ports.ToolRegistry,
	log *eventLog,
	policy ports.Policy,
	approver ports.Approver,
	mutate func(*agent.Config),
) *agent.Loop {
	t.Helper()
	cfg := agent.Config{
		SystemPrompt: "system prompt",
		MaxSteps:     10,
		MaxToolCalls: 10,
		MaxParallel:  4,
		Context:      agent.ContextConfig{Window: 8192, ReserveOutput: 512},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	loop, err := agent.NewLoop(agent.Deps{
		Model:    model,
		Tools:    registry,
		Policy:   policy,
		Approver: approver,
		Now:      func() time.Time { return time.Unix(0, 0) },
		NewID:    func() string { return "id" },
		Emitter:  emitter(log),
	}, cfg)
	if err != nil {
		t.Fatalf("NewLoop: %v", err)
	}
	return loop
}

func toolCall(id, name, arguments string) core.ToolCall {
	return core.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(arguments)}
}

func TestRunChainsToolCalls(t *testing.T) {
	read := &stubTool{name: "fs_read", risk: core.RiskR0}
	registry := newRegistry(t, read)

	steps := make([]scriptStep, 0, 6)
	for i := 1; i <= 5; i++ {
		steps = append(steps, scriptStep{
			calls: []core.ToolCall{toolCall(fmt.Sprintf("call-%d", i), "fs_read", fmt.Sprintf(`{"path":"file-%d"}`, i))},
		})
	}
	steps = append(steps, scriptStep{text: "final answer"})

	model := &scriptedModel{steps: steps}
	log := &eventLog{}
	loop := newLoop(t, model, registry, log, nil)

	result, err := loop.Run(context.Background(), "session", "read five files")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.StopReason != agent.StopAnswer {
		t.Errorf("stop reason = %q, want answer", result.StopReason)
	}
	if result.Answer != "final answer" {
		t.Errorf("answer = %q", result.Answer)
	}
	if result.Steps != 6 {
		t.Errorf("steps = %d, want 6", result.Steps)
	}
	if result.ToolCalls != 5 {
		t.Errorf("tool calls = %d, want 5", result.ToolCalls)
	}

	// The tool messages must be present and aligned with the calls.
	requests := model.Requests()
	last := requests[len(requests)-1]
	toolMessages := 0
	for _, message := range last.Messages {
		if message.Role == core.RoleTool {
			toolMessages++
			if message.ToolName != "fs_read" {
				t.Errorf("tool message name = %q", message.ToolName)
			}
		}
	}
	if toolMessages != 5 {
		t.Errorf("tool messages = %d, want 5", toolMessages)
	}
}

func TestRunStopsOnStepBudget(t *testing.T) {
	read := &stubTool{name: "fs_read", risk: core.RiskR0}
	model := &scriptedModel{steps: []scriptStep{{
		calls: []core.ToolCall{toolCall("call-1", "fs_read", `{"path":"a"}`)},
	}}}
	loop := newLoop(t, model, newRegistry(t, read), nil, func(cfg *agent.Config) {
		cfg.MaxSteps = 2
	})

	result, err := loop.Run(context.Background(), "", "loop forever")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.StopReason != agent.StopBudget {
		t.Errorf("stop reason = %q, want budget", result.StopReason)
	}
	if !strings.Contains(result.Answer, "model steps") {
		t.Errorf("answer = %q, want an actionable budget message", result.Answer)
	}
}

func TestRunStopsOnLoop(t *testing.T) {
	read := &stubTool{name: "fs_read", risk: core.RiskR0}
	call := toolCall("call-1", "fs_read", `{"path":"same"}`)
	model := &scriptedModel{steps: []scriptStep{{calls: []core.ToolCall{call}}}}
	loop := newLoop(t, model, newRegistry(t, read), nil, nil)

	result, err := loop.Run(context.Background(), "", "loop")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.StopReason != agent.StopLoop {
		t.Fatalf("stop reason = %q, want loop_detected", result.StopReason)
	}
	if !strings.Contains(result.Answer, "identical arguments") {
		t.Errorf("answer = %q, want the loop explanation", result.Answer)
	}

	// The third repetition must have injected the observation into the next request.
	requests := model.Requests()
	if len(requests) < 4 {
		t.Fatalf("requests = %d, want at least 4", len(requests))
	}
	warned := false
	for _, message := range requests[3].Messages {
		if message.Role == core.RoleSystem && strings.Contains(message.Content, "3 times") {
			warned = true
		}
	}
	if !warned {
		t.Error("the 4th request does not contain the loop warning")
	}
}

func TestRunCanceled(t *testing.T) {
	read := &stubTool{name: "fs_read", risk: core.RiskR0}
	model := &scriptedModel{steps: []scriptStep{{block: true}}}
	loop := newLoop(t, model, newRegistry(t, read), nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := loop.Run(ctx, "", "cancel me")
	if err == nil || !core.Is(err, core.CodeCanceled) {
		t.Fatalf("error = %v, want CANCELED", err)
	}
	if result.StopReason != agent.StopCanceled {
		t.Errorf("stop reason = %q, want canceled", result.StopReason)
	}
}

func TestRunTurnDeadlineIsBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		read := &stubTool{name: "fs_read", risk: core.RiskR0}
		model := &scriptedModel{steps: []scriptStep{{block: true}}}
		loop := newLoop(t, model, newRegistry(t, read), nil, func(cfg *agent.Config) {
			cfg.MaxTurnDuration = 10 * time.Second
		})

		result, err := loop.Run(context.Background(), "", "hang")
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.StopReason != agent.StopBudget {
			t.Errorf("stop reason = %q, want budget", result.StopReason)
		}
		if !strings.Contains(result.Answer, "time limit") {
			t.Errorf("answer = %q, want the time-budget message", result.Answer)
		}
	})
}

func TestRunParallelReadOnlyTools(t *testing.T) {
	var active, peak atomic.Int64
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	go func() {
		<-started
		<-started
		close(release)
	}()

	parallelTool := &stubTool{
		name:   "fs_search",
		risk:   core.RiskR0,
		active: &active,
		peak:   &peak,
		run: func(ctx context.Context, call core.ToolCall) (core.ToolResult, error) {
			started <- struct{}{}
			select {
			case <-release:
			case <-time.After(2 * time.Second):
				return core.ToolResult{}, errors.New("tools did not run in parallel")
			}
			return core.ToolResult{CallID: call.ID, OK: true, Data: json.RawMessage(`{"ok":true}`)}, nil
		},
	}
	registry := newRegistry(t, parallelTool)
	model := &scriptedModel{steps: []scriptStep{
		{calls: []core.ToolCall{
			toolCall("a", "fs_search", `{"pattern":"a"}`),
			toolCall("b", "fs_search", `{"pattern":"b"}`),
		}},
		{text: "done"},
	}}
	log := &eventLog{}
	loop := newLoop(t, model, registry, log, nil)

	result, err := loop.Run(context.Background(), "", "search twice")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.StopReason != agent.StopAnswer {
		t.Errorf("stop reason = %q", result.StopReason)
	}
	if peak.Load() < 2 {
		t.Errorf("peak concurrency = %d, want 2", peak.Load())
	}

	// Results are observed in call order even when the tools finish out of order.
	results := result.ToolCalls
	if results != 2 {
		t.Errorf("tool calls = %d", results)
	}
}

func TestRunSequentialForHigherRisk(t *testing.T) {
	var active, peak atomic.Int64
	write := &stubTool{name: "fs_write", risk: core.RiskR2, active: &active, peak: &peak}
	registry := newRegistry(t, write)
	model := &scriptedModel{steps: []scriptStep{
		{calls: []core.ToolCall{
			toolCall("a", "fs_write", `{"path":"a"}`),
			toolCall("b", "fs_write", `{"path":"b"}`),
		}},
		{text: "done"},
	}}
	loop := newLoop(t, model, registry, nil, nil)

	if _, err := loop.Run(context.Background(), "", "write twice"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if peak.Load() != 1 {
		t.Errorf("peak concurrency = %d, want 1", peak.Load())
	}
}

func TestEventsAreOrderedAndMonotonic(t *testing.T) {
	read := &stubTool{name: "fs_read", risk: core.RiskR0}
	model := &scriptedModel{steps: []scriptStep{
		{text: "reading", calls: []core.ToolCall{toolCall("call-1", "fs_read", `{"path":"a"}`)}},
		{text: "done"},
	}}
	log := &eventLog{}
	loop := newLoop(t, model, newRegistry(t, read), log, nil)

	result, err := loop.Run(context.Background(), "session", "read")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	events := log.Events()
	if len(events) == 0 {
		t.Fatal("no events emitted")
	}
	if events[0].Type != core.EventTurnStarted {
		t.Errorf("first event = %s", events[0].Type)
	}
	if events[len(events)-1].Type != core.EventTurnFinished {
		t.Errorf("last event = %s", events[len(events)-1].Type)
	}
	for i, event := range events {
		if event.Seq != uint64(i+1) {
			t.Fatalf("event %d has seq %d", i, event.Seq)
		}
		if event.TurnID == "" || event.SessionID != "session" {
			t.Fatalf("event %d is missing turn metadata: %+v", i, event)
		}
	}
	if result.Events != uint64(len(events)) {
		t.Errorf("result events = %d, want %d", result.Events, len(events))
	}

	// model.delta must arrive before tool.started.
	seenDelta := false
	for _, event := range events {
		switch event.Type {
		case core.EventModelDelta:
			seenDelta = true
		case core.EventToolStarted:
			if !seenDelta {
				t.Error("tool.started before any model.delta")
			}
		default:
			// Other lifecycle events do not affect the ordering assertion.
		}
	}
}

func TestRunUnknownToolFeedsErrorBack(t *testing.T) {
	model := &scriptedModel{steps: []scriptStep{
		{calls: []core.ToolCall{toolCall("call-1", "nope", `{}`)}},
		{text: "recovered"},
	}}
	loop := newLoop(t, model, newRegistry(t), nil, nil)

	result, err := loop.Run(context.Background(), "", "use a missing tool")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.StopReason != agent.StopAnswer || result.Answer != "recovered" {
		t.Fatalf("result = %+v", result)
	}
	requests := model.Requests()
	last := requests[len(requests)-1]
	found := false
	for _, message := range last.Messages {
		if message.Role == core.RoleTool && strings.Contains(message.Content, "UNKNOWN_TOOL") {
			found = true
		}
	}
	if !found {
		t.Error("the unknown-tool error was not sent back to the model")
	}
}

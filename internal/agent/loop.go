// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

const (
	defaultMaxParallel = 4
	maxToolErrorBytes  = 2 << 10
)

// Config bounds one loop instance. Values are validated once in NewLoop so a turn cannot start
// with a nonsensical budget.
type Config struct {
	// SystemPrompt is the versioned prompt built by SystemPrompt.
	SystemPrompt string
	// MaxSteps is the maximum number of model calls in a turn.
	MaxSteps int
	// MaxToolCalls is the maximum number of tool calls in a turn.
	MaxToolCalls int
	// MaxParallel is the maximum number of concurrent read-only tools. 1 means sequential.
	MaxParallel int
	// MaxTurnDuration is the wall-clock limit of a turn. Zero disables it.
	MaxTurnDuration time.Duration
	// Context is the budget split of every model call.
	Context ContextConfig
	// Compactor replaces old history when the context overflows. Nil means the default
	// ExtractiveCompactor; use a non-nil no-op to disable compaction.
	Compactor Compactor

	Temperature     float64
	TopK            int
	MaxOutputTokens int
}

// Deps are the collaborators of the loop.
type Deps struct {
	Model   ports.Model
	Tools   ports.ToolRegistry
	Emitter ports.Emitter
	Logger  *slog.Logger
	// Now and NewID are injectable for deterministic tests.
	Now   func() time.Time
	NewID func() string
}

// Loop runs agent turns.
type Loop struct {
	deps   Deps
	cfg    Config
	budget Budget
	ctxmgr *ContextManager
}

// NewLoop validates deps and cfg.
func NewLoop(deps Deps, cfg Config) (*Loop, error) {
	if deps.Model == nil {
		return nil, core.E(core.CodeInternal, "agent: model is required")
	}
	if deps.Tools == nil {
		return nil, core.E(core.CodeInternal, "agent: tool registry is required")
	}
	if strings.TrimSpace(cfg.SystemPrompt) == "" {
		return nil, core.E(core.CodeConfigInvalid, "agent: system prompt is required")
	}
	if cfg.MaxSteps <= 0 {
		return nil, core.E(core.CodeConfigInvalid, "agent: max steps must be positive")
	}
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = defaultMaxParallel
	}
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.DiscardHandler)
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = core.NewID
	}
	if cfg.Compactor == nil {
		cfg.Compactor = ExtractiveCompactor{}
	}

	manager, err := NewContextManager(cfg.Context, cfg.Compactor)
	if err != nil {
		return nil, err
	}

	return &Loop{
		deps: deps,
		cfg:  cfg,
		budget: Budget{
			MaxSteps:     cfg.MaxSteps,
			MaxToolCalls: cfg.MaxToolCalls,
		},
		ctxmgr: manager,
	}, nil
}

// Run executes one turn until the model answers, a budget runs out, the loop detector fires or
// the context is canceled. A canceled turn returns a CANCELED error; a budget or loop stop
// returns a normal Result with an actionable answer and a nil error.
func (l *Loop) Run(ctx context.Context, sessionID, input string) (Result, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Result{}, core.E(core.CodeUsage, "prompt must not be empty")
	}

	turn := newTurn(sessionID, input, l.deps.NewID(), l.deps.Now())
	turn.Append(core.Message{Role: core.RoleSystem, Content: l.cfg.SystemPrompt})
	turn.Append(core.Message{Role: core.RoleUser, Content: input})

	if l.cfg.MaxTurnDuration > 0 {
		l.budget.Deadline = l.deps.Now().Add(l.cfg.MaxTurnDuration)
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.cfg.MaxTurnDuration)
		defer cancel()
	}

	result := Result{TurnID: turn.ID, SessionID: sessionID}
	finish := func(reason StopReason, answer string, err error) (Result, error) {
		result.StopReason = reason
		result.Answer = answer
		result.Steps = turn.steps
		result.ToolCalls = turn.toolCalls
		result.Usage = turn.usage
		result.Err = err
		if emitErr := l.emit(ctx, turn, core.EventTurnFinished, turnFinishedData{
			StopReason: reason,
			Steps:      turn.steps,
			ToolCalls:  turn.toolCalls,
			Usage:      turn.usage,
		}); emitErr != nil && err == nil {
			err = emitErr
			result.Err = err
		}
		result.Events = turn.seq
		return result, err
	}

	if err := l.emit(ctx, turn, core.EventTurnStarted, turnStartedData{
		Model:    l.deps.Model.Name(),
		Input:    input,
		MaxSteps: l.cfg.MaxSteps,
	}); err != nil {
		return finish(StopFailed, "", core.Wrap(core.CodeInternal, err, "emit turn.started"))
	}

	for {
		if err := ctx.Err(); err != nil {
			return finish(StopCanceled, turn.Answer(),
				core.Wrap(core.CodeCanceled, err, "turn canceled"))
		}
		if err := l.budget.Check(l.deps.Now(), turn.steps, turn.toolCalls); err != nil {
			return finish(StopBudget, withStopMessage(turn.Answer(), budgetMessage(err)), nil)
		}
		if warning, ok := l.budget.Warning(turn, turn.steps); ok {
			turn.Append(core.Message{Role: core.RoleSystem, Content: warning})
			if err := l.emit(ctx, turn, core.EventBudgetWarning, map[string]any{
				"message": warning,
				"steps":   turn.steps,
			}); err != nil {
				return finish(StopFailed, turn.Answer(), core.Wrap(core.CodeInternal, err, "emit budget.warning"))
			}
		}

		messages, err := l.ctxmgr.Build(ctx, turn)
		if err != nil {
			if core.Is(err, core.CodeCanceled) {
				return finish(StopCanceled, turn.Answer(), err)
			}
			return finish(StopFailed, turn.Answer(), err)
		}

		answer, reasoning, calls, usage, err := l.stream(ctx, turn, messages)
		if err != nil {
			return l.failTurn(ctx, turn, finish, err)
		}

		turn.steps++
		turn.addUsage(usage)
		turn.answer.WriteString(answer)
		turn.reasoning.WriteString(reasoning)
		turn.Append(core.Message{
			Role:      core.RoleAssistant,
			Content:   answer,
			Reasoning: reasoning,
			ToolCalls: calls,
		})

		if len(calls) == 0 {
			return finish(StopAnswer, turn.Answer(), nil)
		}

		if stop, message := l.detectLoops(turn, calls); stop {
			return finish(StopLoop, withStopMessage(turn.Answer(), message), nil)
		}

		if l.budget.MaxToolCalls > 0 && turn.toolCalls+len(calls) > l.budget.MaxToolCalls {
			return finish(StopBudget, withStopMessage(turn.Answer(), budgetMessage(fmt.Errorf(
				"%w: %d calls requested, %d/%d used",
				ErrBudgetTools, len(calls), turn.toolCalls, l.budget.MaxToolCalls))), nil)
		}

		results, err := l.executeTools(ctx, turn, calls)
		if err != nil {
			return l.failTurn(ctx, turn, finish, err)
		}
		for i, toolResult := range results {
			turn.Observe(toolResult)
			message := toolMessage(toolResult)
			message.ToolName = calls[i].Name
			turn.Append(message)
		}
		turn.toolCalls += len(calls)
	}
}

// failTurn classifies a model or tool error as cancellation, deadline or failure.
func (l *Loop) failTurn(
	ctx context.Context,
	turn *Turn,
	finish func(StopReason, string, error) (Result, error),
	err error,
) (Result, error) {
	// The turn timeout is a budget, not a user cancellation, even when the backend reports the
	// dead connection as a canceled request.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return finish(StopBudget, withStopMessage(turn.Answer(), budgetMessage(ErrBudgetTime)), nil)
	}
	if core.Is(err, core.CodeCanceled) || errors.Is(err, context.Canceled) {
		return finish(StopCanceled, turn.Answer(),
			core.Wrap(core.CodeCanceled, err, "turn canceled"))
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return finish(StopBudget, withStopMessage(turn.Answer(), budgetMessage(ErrBudgetTime)), nil)
	}
	return finish(StopFailed, turn.Answer(), err)
}

// stream runs one model call and returns the accumulated answer, reasoning, tool calls and
// usage. Visible deltas are emitted as model.delta events.
func (l *Loop) stream(
	ctx context.Context,
	turn *Turn,
	messages []core.Message,
) (string, string, []core.ToolCall, ports.Usage, error) {
	request := ports.ChatRequest{
		Messages:    messages,
		Tools:       l.deps.Tools.Specs(),
		Temperature: l.cfg.Temperature,
		TopK:        l.cfg.TopK,
		MaxTokens:   l.cfg.MaxOutputTokens,
	}

	stream, err := l.deps.Model.Stream(ctx, request)
	if err != nil {
		return "", "", nil, ports.Usage{}, err
	}
	defer func() { _ = stream.Close() }()

	var answer, reasoning strings.Builder
	var calls []core.ToolCall
	var usage ports.Usage

	for {
		event, nextErr := stream.Next(ctx)
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return "", "", nil, ports.Usage{}, nextErr
		}

		switch event.Type {
		case ports.StreamText:
			answer.WriteString(event.Text)
			if err := l.emit(ctx, turn, core.EventModelDelta, modelDeltaData{Text: event.Text}); err != nil {
				return "", "", nil, ports.Usage{}, core.Wrap(core.CodeInternal, err, "emit model.delta")
			}
		case ports.StreamReasoning:
			reasoning.WriteString(event.Reasoning)
		case ports.StreamToolCalls:
			calls = event.ToolCalls
		case ports.StreamDone:
			if event.Usage != nil {
				usage = *event.Usage
			}
		}
	}
	return answer.String(), reasoning.String(), calls, usage, nil
}

// executeTools runs the calls of one step. Read-only calls run in parallel when every call is
// R0/R1 and no two calls are identical; everything else runs in call order. Results are always
// returned in call order so the tool messages stay aligned with the assistant message.
func (l *Loop) executeTools(
	ctx context.Context,
	turn *Turn,
	calls []core.ToolCall,
) ([]core.ToolResult, error) {
	results := make([]core.ToolResult, len(calls))
	parallel := l.canRunParallel(calls)

	if !parallel {
		for i, call := range calls {
			if err := l.emit(ctx, turn, core.EventToolStarted, toolStartedData{
				CallID:    call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			}); err != nil {
				return nil, core.Wrap(core.CodeInternal, err, "emit tool.started")
			}
			results[i] = l.runTool(ctx, call)
			if err := l.emit(ctx, turn, core.EventToolFinished, toolFinishedData{
				CallID: call.ID,
				Name:   call.Name,
				Result: results[i],
			}); err != nil {
				return nil, core.Wrap(core.CodeInternal, err, "emit tool.finished")
			}
		}
	} else {
		for _, call := range calls {
			if err := l.emit(ctx, turn, core.EventToolStarted, toolStartedData{
				CallID:    call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			}); err != nil {
				return nil, core.Wrap(core.CodeInternal, err, "emit tool.started")
			}
		}

		var wait sync.WaitGroup
		limit := make(chan struct{}, l.cfg.MaxParallel)
		for i, call := range calls {
			wait.Add(1)
			go func(i int, call core.ToolCall) {
				defer wait.Done()
				limit <- struct{}{}
				defer func() { <-limit }()
				results[i] = l.runTool(ctx, call)
			}(i, call)
		}
		wait.Wait()

		for i, call := range calls {
			if err := l.emit(ctx, turn, core.EventToolFinished, toolFinishedData{
				CallID: call.ID,
				Name:   call.Name,
				Result: results[i],
			}); err != nil {
				return nil, core.Wrap(core.CodeInternal, err, "emit tool.finished")
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, core.Wrap(core.CodeCanceled, err, "tool execution canceled")
	}
	return results, nil
}

// runTool executes one call and never returns an error: expected failures become ToolError so
// the model can correct itself, and panics are contained.
func (l *Loop) runTool(ctx context.Context, call core.ToolCall) core.ToolResult {
	started := l.deps.Now()

	tool, ok := l.deps.Tools.Get(call.Name)
	if !ok {
		return core.ToolResult{
			CallID: call.ID,
			Error: &core.ToolError{
				Code:    "UNKNOWN_TOOL",
				Message: fmt.Sprintf("unknown tool %q", call.Name),
				Hint:    "available tools: " + strings.Join(l.deps.Tools.Names(), ", "),
			},
			Meta: core.ToolMeta{DurationMS: l.deps.Now().Sub(started).Milliseconds()},
		}
	}

	var result core.ToolResult
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				l.deps.Logger.Error("tool panicked", "tool", call.Name, "panic", recovered)
				result = core.ToolResult{
					CallID: call.ID,
					Error: &core.ToolError{
						Code:    "TOOL_PANIC",
						Message: fmt.Sprintf("%s panicked: %v", call.Name, recovered),
					},
				}
			}
		}()
		executed, err := tool.Execute(ctx, call)
		if err != nil {
			result = core.ToolResult{
				CallID: call.ID,
				Error: &core.ToolError{
					Code:    "TOOL_FAILED",
					Message: truncateText(err.Error(), maxToolErrorBytes),
				},
			}
			return
		}
		result = executed
	}()

	if result.CallID == "" {
		result.CallID = call.ID
	}
	if result.Meta.DurationMS == 0 {
		result.Meta.DurationMS = l.deps.Now().Sub(started).Milliseconds()
	}
	return result
}

// canRunParallel reports whether all calls are independent read-only calls.
func (l *Loop) canRunParallel(calls []core.ToolCall) bool {
	if l.cfg.MaxParallel <= 1 || len(calls) < 2 {
		return false
	}
	seen := make(map[string]bool, len(calls))
	for _, call := range calls {
		tool, ok := l.deps.Tools.Get(call.Name)
		if !ok || tool.Spec().Risk > core.RiskR1 {
			return false
		}
		key := callKey(call)
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// detectLoops stops the turn when the model repeats the same call with identical arguments.
// The third repetition injects an observation; the fourth stops with an actionable message.
func (l *Loop) detectLoops(turn *Turn, calls []core.ToolCall) (bool, string) {
	for _, call := range calls {
		key := callKey(call)
		turn.loopCounts[key]++
		count := turn.loopCounts[key]

		if count >= 4 {
			return true, fmt.Sprintf(
				"I stopped because %s was called with identical arguments %d times and the result "+
					"will not change. Try a different approach, narrower arguments, or answer with "+
					"the information already gathered.", call.Name, count)
		}
		if count == 3 && !turn.loopWarned[key] {
			turn.loopWarned[key] = true
			turn.Append(core.Message{
				Role: core.RoleSystem,
				Content: fmt.Sprintf(
					"You have called %s with identical arguments 3 times. The result will not "+
						"change. Try a different approach or answer with what you have.", call.Name),
			})
		}
	}
	return false, ""
}

// emit publishes one event with the next sequence number.
func (l *Loop) emit(ctx context.Context, turn *Turn, kind core.EventType, data any) error {
	if l.deps.Emitter == nil {
		return nil
	}
	var raw json.RawMessage
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("encode %s event: %w", kind, err)
		}
		raw = encoded
	}
	event := core.Event{
		Seq:       turn.nextSeq(),
		ID:        l.deps.NewID(),
		Type:      kind,
		Time:      l.deps.Now(),
		SessionID: turn.SessionID,
		TurnID:    turn.ID,
		Data:      raw,
	}
	return l.deps.Emitter.Emit(ctx, event)
}

// callKey hashes the tool name and the canonical JSON of its arguments.
func callKey(call core.ToolCall) string {
	arguments := bytes.TrimSpace(call.Arguments)
	var value any
	if err := json.Unmarshal(arguments, &value); err == nil {
		if normalized, err := json.Marshal(value); err == nil {
			arguments = normalized
		}
	}
	sum := sha256.New()
	sum.Write([]byte(call.Name))
	sum.Write([]byte{0})
	sum.Write(arguments)
	return hex.EncodeToString(sum.Sum(nil)[:8])
}

func withStopMessage(answer, message string) string {
	if answer == "" {
		return message
	}
	return answer + "\n\n" + message
}

func budgetMessage(err error) string {
	switch {
	case errors.Is(err, ErrBudgetSteps):
		return "I stopped because this turn used all its model steps. Send a narrower request to continue."
	case errors.Is(err, ErrBudgetTools):
		return "I stopped because this turn used all its tool calls. Send a narrower request to continue."
	case errors.Is(err, ErrBudgetTime):
		return "I stopped because the turn reached its time limit. Send a narrower request to continue."
	default:
		return "I stopped because the turn budget ran out. Send a narrower request to continue."
	}
}

func truncateText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

// Event payloads. They are JSON contracts consumed by the CLI and, later, by the TUI and the
// audit log, so field names are stable.
type turnStartedData struct {
	Model    string `json:"model"`
	Input    string `json:"input"`
	MaxSteps int    `json:"max_steps"`
}

type modelDeltaData struct {
	Text string `json:"text"`
}

type toolStartedData struct {
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolFinishedData struct {
	CallID string          `json:"call_id"`
	Name   string          `json:"name"`
	Result core.ToolResult `json:"result"`
}

type turnFinishedData struct {
	StopReason StopReason  `json:"stop_reason"`
	Steps      int         `json:"steps"`
	ToolCalls  int         `json:"tool_calls"`
	Usage      ports.Usage `json:"usage"`
}

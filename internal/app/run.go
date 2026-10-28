// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/agent"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/policy"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/store"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/tools"
)

// RunOptions controls one agent turn.
type RunOptions struct {
	Prompt string
	// SessionID is empty for a one-shot turn; F4 wires persisted sessions.
	SessionID string
	// Approval configures the CLI approval shortcuts.
	Approval ApprovalOptions
}

// RunResult is the stable result of `run --print`.
type RunResult struct {
	TurnID     string      `json:"turn_id"`
	SessionID  string      `json:"session_id,omitempty"`
	Answer     string      `json:"answer"`
	StopReason string      `json:"stop_reason"`
	Steps      int         `json:"steps"`
	ToolCalls  int         `json:"tool_calls"`
	Usage      ports.Usage `json:"usage"`
	Model      string      `json:"model"`
	PromptHash string      `json:"prompt_hash"`
}

// RunTurn executes one turn with the tool catalog selected by the policy mode. In text mode the
// answer streams to stdout and tool activity goes to stderr; in JSON mode everything is buffered
// so stdout stays a single envelope.
func (a *App) RunTurn(ctx context.Context, opts RunOptions) (RunResult, error) {
	if !a.configured {
		return RunResult{}, core.E(core.CodeInternal, "configuration was not loaded")
	}

	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		return RunResult{}, core.E(core.CodeUsage, "prompt must not be empty")
	}

	model, err := a.newModel(a.Config.Model)
	if err != nil {
		return RunResult{}, err
	}

	cleanup, err := a.ensureModel(ctx)
	if err != nil {
		return RunResult{}, err
	}
	defer cleanup()

	workspace := a.Config.General.Workspace
	registry, err := a.toolRegistry(workspace)
	if err != nil {
		return RunResult{}, core.Wrap(core.CodeConfigInvalid, err,
			"prepare the tools for %q", workspace)
	}

	engine, err := policy.NewEngine(a.Config.Policy.Mode, a.Config.Tools.Allowlist)
	if err != nil {
		return RunResult{}, core.Wrap(core.CodeConfigInvalid, err, "build the policy engine")
	}
	if engine.Mode == policy.ModeAllow {
		_, _ = fmt.Fprintf(a.Stderr,
			"%s policy mode is allow: writes run without approval inside the workspace\n", prefix)
	}

	auditLog, err := store.OpenAudit(workspace)
	if err != nil {
		return RunResult{}, core.Wrap(core.CodeConfigInvalid, err, "open the audit log")
	}
	defer func() {
		if closeErr := auditLog.Close(); closeErr != nil {
			a.Logger.Warn("close audit log", "error", closeErr)
		}
	}()

	specs := registry.Specs()
	system := agent.SystemPrompt(agent.PromptOptions{
		Tools:     specs,
		Workspace: workspace,
		Policy:    a.Config.Policy.Mode.String(),
		Language:  a.Config.General.Language,
	})
	hash := agent.PromptHash(system)

	emitter := newAuditEmitter(&turnEmitter{app: a}, auditLog, func(err error) {
		a.Logger.Warn("write audit record", "error", err)
	})
	loop, err := agent.NewLoop(agent.Deps{
		Model:    model,
		Tools:    registry,
		Policy:   engine,
		Approver: newApprover(a.Stdin, a.Stderr, opts.Approval),
		Emitter:  emitter,
		Logger:   a.Logger,
	}, agent.Config{
		SystemPrompt:    system,
		MaxSteps:        a.Config.Agent.MaxSteps,
		MaxToolCalls:    a.Config.Agent.MaxToolCalls,
		MaxParallel:     a.Config.Agent.ParallelReadOnlyTools,
		MaxTurnDuration: a.Config.Agent.MaxTurnDuration.Std(),
		Context: agent.ContextConfig{
			Window:        a.Config.Model.ContextWindow,
			ReserveOutput: a.Config.Model.MaxOutputTokens,
			Tools:         specs,
		},
		Temperature:     a.Config.Model.Temperature,
		TopK:            a.Config.Model.TopK,
		MaxOutputTokens: a.Config.Model.MaxOutputTokens,
	})
	if err != nil {
		return RunResult{}, err
	}

	result, err := loop.Run(ctx, opts.SessionID, prompt)
	if err != nil {
		return RunResult{
			TurnID:     result.TurnID,
			SessionID:  result.SessionID,
			Answer:     result.Answer,
			StopReason: string(result.StopReason),
			Steps:      result.Steps,
			ToolCalls:  result.ToolCalls,
			Usage:      result.Usage,
			Model:      model.Name(),
			PromptHash: hash,
		}, err
	}

	return RunResult{
		TurnID:     result.TurnID,
		SessionID:  result.SessionID,
		Answer:     result.Answer,
		StopReason: string(result.StopReason),
		Steps:      result.Steps,
		ToolCalls:  result.ToolCalls,
		Usage:      result.Usage,
		Model:      model.Name(),
		PromptHash: hash,
	}, nil
}

// toolRegistry returns the catalog that matches the policy posture. Read-only mode never
// exposes write tools to the model, not even as denied calls.
func (a *App) toolRegistry(workspace string) (*tools.Registry, error) {
	if !a.Config.Policy.Mode.AllowsWrites() {
		return tools.NewReadOnly(workspace)
	}
	return tools.NewWorkspace(workspace, tools.Options{MaxWriteBytes: a.Config.Policy.MaxWriteBytes})
}

// turnEmitter renders turn events. Text mode streams the answer and reports tool activity on
// stderr; JSON mode stays silent because the CLI prints one envelope at the end.
type turnEmitter struct {
	app *App
}

func (e *turnEmitter) Emit(_ context.Context, event core.Event) error {
	if e.app.JSON {
		return nil
	}

	switch event.Type {
	case core.EventModelDelta:
		var data struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return core.Wrap(core.CodeInternal, err, "decode model.delta")
		}
		if _, err := io.WriteString(e.app.Stdout, data.Text); err != nil {
			return core.Wrap(core.CodeInternal, err, "write answer")
		}
	case core.EventToolApproved:
		var data struct {
			Name       string `json:"name"`
			ApprovedBy string `json:"approved_by"`
			Scope      string `json:"approval_scope"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return core.Wrap(core.CodeInternal, err, "decode tool.approved")
		}
		_, _ = fmt.Fprintf(e.app.Stderr, "tool %s approved by %s (%s)\n",
			data.Name, data.ApprovedBy, data.Scope)
	case core.EventToolDenied:
		var data struct {
			Name   string `json:"name"`
			Rule   string `json:"rule"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return core.Wrap(core.CodeInternal, err, "decode tool.denied")
		}
		_, _ = fmt.Fprintf(e.app.Stderr, "tool %s denied (%s): %s\n",
			data.Name, data.Rule, data.Reason)
	case core.EventToolStarted:
		var data struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return core.Wrap(core.CodeInternal, err, "decode tool.started")
		}
		_, _ = fmt.Fprintf(e.app.Stderr, "tool %s %s\n", data.Name, compactJSON(data.Arguments))
	case core.EventToolFinished:
		var data struct {
			Name   string          `json:"name"`
			Result core.ToolResult `json:"result"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return core.Wrap(core.CodeInternal, err, "decode tool.finished")
		}
		if data.Result.OK {
			_, _ = fmt.Fprintf(e.app.Stderr, "tool %s ok (%d bytes, %d ms)\n",
				data.Name, data.Result.Meta.Bytes, data.Result.Meta.DurationMS)
			return nil
		}
		message := "unknown error"
		if data.Result.Error != nil {
			message = data.Result.Error.Message
		}
		_, _ = fmt.Fprintf(e.app.Stderr, "tool %s failed: %s\n", data.Name, message)
	default:
		// turn.started, turn.finished and budget.warning are consumed by the TUI and the
		// audit log, not by the one-shot CLI.
	}
	return nil
}

// compactJSON shortens tool arguments for the progress line.
func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		return ""
	}
	return truncate(buffer.String(), 160)
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

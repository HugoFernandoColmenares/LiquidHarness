// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package ports

import (
	"context"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

// Outcome is the policy verdict for one tool call.
type Outcome string

const (
	// OutcomeAllow runs the call without asking.
	OutcomeAllow Outcome = "allow"
	// OutcomeAsk requires a human decision before the call runs.
	OutcomeAsk Outcome = "ask"
	// OutcomeDeny refuses the call.
	OutcomeDeny Outcome = "deny"
)

// Decision is the verdict plus the audit metadata that explains it.
type Decision struct {
	Outcome Outcome
	// Rule identifies the policy rule that produced the decision.
	Rule string
	// Reason is a human-readable explanation shown in prompts and tool errors.
	Reason string
}

// Policy decides whether a tool call may run. Implementations are pure: they never touch the
// filesystem, the terminal or the clock.
type Policy interface {
	Decide(spec core.ToolSpec, risk core.RiskClass) Decision
}

// ApprovalRequest is what a human sees before an ask decision runs.
type ApprovalRequest struct {
	Tool      string
	Risk      core.RiskClass
	Arguments []byte
	Reason    string
	Rule      string
}

// ApprovalDecision is the human answer.
type ApprovalDecision struct {
	Approved bool
	// Scope records whether the answer covers this call only or the rest of the session.
	Scope string
	// ApprovedBy identifies who answered, for the audit log: "user" for an interactive
	// prompt or "flag" when a CLI shortcut pre-approved the call.
	ApprovedBy string
}

// Approver asks a human to approve a tool call.
type Approver interface {
	Approve(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error)
}

// RiskAssessor lets a tool escalate the static risk of its spec based on the arguments, for
// example fs_create with overwrite=true (R2) versus a fresh file (R1).
type RiskAssessor interface {
	RiskFor(call core.ToolCall) core.RiskClass
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"fmt"
	"strings"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

// Rule identifiers. They are stable strings because they appear in audit records.
const (
	RuleReadOnly      = "mode-read-only"
	RuleAskWrite      = "mode-ask-write"
	RuleAllow         = "mode-allow"
	RuleRiskTooHigh   = "risk-too-high"
	RuleToolNotListed = "tool-not-in-allowlist"
	RuleInvalidRisk   = "invalid-risk"
)

// Engine is the default policy: the mode sets the posture and the tool allowlist narrows it.
// Shell and network tools are not registered in F3, so their flags do not need a rule yet.
type Engine struct {
	Mode Mode
	// Allowlist restricts which tools are visible. An empty list means "every registered tool".
	Allowlist []string
}

// NewEngine validates and returns an engine.
func NewEngine(mode Mode, allowlist []string) (*Engine, error) {
	if !mode.Valid() {
		return nil, fmt.Errorf("invalid policy mode %q", mode)
	}
	clean := make([]string, 0, len(allowlist))
	for _, name := range allowlist {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		clean = append(clean, name)
	}
	return &Engine{Mode: mode, Allowlist: clean}, nil
}

// Decide implements ports.Policy.
func (e *Engine) Decide(spec core.ToolSpec, risk core.RiskClass) ports.Decision {
	if !risk.Valid() {
		return ports.Decision{
			Outcome: ports.OutcomeDeny,
			Rule:    RuleInvalidRisk,
			Reason:  fmt.Sprintf("%s reports an invalid risk class", spec.Name),
		}
	}
	if len(e.Allowlist) > 0 && !contains(e.Allowlist, spec.Name) {
		return ports.Decision{
			Outcome: ports.OutcomeDeny,
			Rule:    RuleToolNotListed,
			Reason:  fmt.Sprintf("%s is not in the tool allowlist", spec.Name),
		}
	}

	switch e.Mode {
	case ModeReadOnly:
		if risk > core.RiskR0 {
			return ports.Decision{
				Outcome: ports.OutcomeDeny,
				Rule:    RuleReadOnly,
				Reason:  fmt.Sprintf("%s is %s and the policy is read-only", spec.Name, risk),
			}
		}
		return ports.Decision{Outcome: ports.OutcomeAllow, Rule: RuleReadOnly}
	case ModeAsk:
		switch {
		case risk == core.RiskR0:
			return ports.Decision{Outcome: ports.OutcomeAllow, Rule: RuleAllow}
		case risk <= core.RiskR2:
			return ports.Decision{
				Outcome: ports.OutcomeAsk,
				Rule:    RuleAskWrite,
				Reason:  fmt.Sprintf("%s is %s and needs approval", spec.Name, risk),
			}
		default:
			return ports.Decision{
				Outcome: ports.OutcomeDeny,
				Rule:    RuleRiskTooHigh,
				Reason:  fmt.Sprintf("%s is %s and ask mode stops at R2", spec.Name, risk),
			}
		}
	case ModeAllow:
		if risk <= core.RiskR2 {
			return ports.Decision{Outcome: ports.OutcomeAllow, Rule: RuleAllow}
		}
		return ports.Decision{
			Outcome: ports.OutcomeDeny,
			Rule:    RuleRiskTooHigh,
			Reason:  fmt.Sprintf("%s is %s and allow mode stops at R2", spec.Name, risk),
		}
	default:
		return ports.Decision{
			Outcome: ports.OutcomeDeny,
			Rule:    RuleInvalidRisk,
			Reason:  fmt.Sprintf("unknown policy mode %q", e.Mode),
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

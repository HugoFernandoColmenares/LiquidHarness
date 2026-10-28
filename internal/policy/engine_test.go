// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package policy_test

import (
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/policy"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

func spec(name string) core.ToolSpec {
	return core.ToolSpec{Name: name, Description: "test tool", Risk: core.RiskR0}
}

func TestEngineModeMatrix(t *testing.T) {
	cases := []struct {
		mode policy.Mode
		risk core.RiskClass
		want ports.Outcome
		rule string
	}{
		{mode: policy.ModeReadOnly, risk: core.RiskR0, want: ports.OutcomeAllow, rule: policy.RuleReadOnly},
		{mode: policy.ModeReadOnly, risk: core.RiskR1, want: ports.OutcomeDeny, rule: policy.RuleReadOnly},
		{mode: policy.ModeReadOnly, risk: core.RiskR2, want: ports.OutcomeDeny, rule: policy.RuleReadOnly},
		{mode: policy.ModeAsk, risk: core.RiskR0, want: ports.OutcomeAllow, rule: policy.RuleAllow},
		{mode: policy.ModeAsk, risk: core.RiskR1, want: ports.OutcomeAsk, rule: policy.RuleAskWrite},
		{mode: policy.ModeAsk, risk: core.RiskR2, want: ports.OutcomeAsk, rule: policy.RuleAskWrite},
		{mode: policy.ModeAsk, risk: core.RiskR3, want: ports.OutcomeDeny, rule: policy.RuleRiskTooHigh},
		{mode: policy.ModeAllow, risk: core.RiskR0, want: ports.OutcomeAllow, rule: policy.RuleAllow},
		{mode: policy.ModeAllow, risk: core.RiskR2, want: ports.OutcomeAllow, rule: policy.RuleAllow},
		{mode: policy.ModeAllow, risk: core.RiskR3, want: ports.OutcomeDeny, rule: policy.RuleRiskTooHigh},
	}

	for _, test := range cases {
		engine, err := policy.NewEngine(test.mode, nil)
		if err != nil {
			t.Fatalf("NewEngine(%q): %v", test.mode, err)
		}
		decision := engine.Decide(spec("fs_read"), test.risk)
		if decision.Outcome != test.want {
			t.Errorf("%s %s: outcome = %q, want %q", test.mode, test.risk, decision.Outcome, test.want)
		}
		if decision.Rule != test.rule {
			t.Errorf("%s %s: rule = %q, want %q", test.mode, test.risk, decision.Rule, test.rule)
		}
	}
}

func TestEngineAllowlistNarrowsTheCatalog(t *testing.T) {
	engine, err := policy.NewEngine(policy.ModeAsk, []string{"fs_read", " fs_search "})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.Decide(spec("fs_read"), core.RiskR0); got.Outcome != ports.OutcomeAllow {
		t.Errorf("fs_read outcome = %q, want allow", got.Outcome)
	}
	if got := engine.Decide(spec("fs_search"), core.RiskR0); got.Outcome != ports.OutcomeAllow {
		t.Errorf("fs_search outcome = %q, want allow", got.Outcome)
	}
	denied := engine.Decide(spec("fs_create"), core.RiskR1)
	if denied.Outcome != ports.OutcomeDeny || denied.Rule != policy.RuleToolNotListed {
		t.Errorf("fs_create = %+v, want deny by allowlist", denied)
	}
}

func TestEngineRejectsInvalidRisk(t *testing.T) {
	engine, err := policy.NewEngine(policy.ModeAsk, nil)
	if err != nil {
		t.Fatal(err)
	}
	decision := engine.Decide(spec("fs_read"), core.RiskClass(42))
	if decision.Outcome != ports.OutcomeDeny || decision.Rule != policy.RuleInvalidRisk {
		t.Errorf("decision = %+v, want deny by invalid risk", decision)
	}
}

func TestNewEngineValidatesMode(t *testing.T) {
	if _, err := policy.NewEngine(policy.Mode("yolo"), nil); err == nil {
		t.Fatal("NewEngine accepted an unknown mode")
	}
}

func TestModeHelpers(t *testing.T) {
	for _, mode := range []policy.Mode{policy.ModeReadOnly, policy.ModeAsk, policy.ModeAllow} {
		parsed, err := policy.ParseMode(" " + mode.String() + " ")
		if err != nil || parsed != mode {
			t.Errorf("ParseMode(%q) = %q, %v", mode, parsed, err)
		}
	}
	if policy.ModeReadOnly.AllowsWrites() {
		t.Error("read-only must not allow writes")
	}
	if !policy.ModeAsk.AllowsWrites() || !policy.ModeAllow.AllowsWrites() {
		t.Error("ask and allow must allow writes")
	}
	if _, err := policy.ParseMode("root"); err == nil {
		t.Error("ParseMode accepted an unknown mode")
	}
}

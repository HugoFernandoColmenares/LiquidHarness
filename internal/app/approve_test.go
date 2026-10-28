// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

func request(risk core.RiskClass) ports.ApprovalRequest {
	return ports.ApprovalRequest{
		Tool:      "fs_create",
		Risk:      risk,
		Arguments: json.RawMessage(`{"path":"notes.md","content":"hello"}`),
		Reason:    "fs_create is R1 and needs approval",
		Rule:      "mode-ask-write",
	}
}

func TestApproverYesOnlyCoversR1(t *testing.T) {
	var out bytes.Buffer
	approver := newApprover(nil, &out, ApprovalOptions{Yes: true})

	decision, err := approver.Approve(context.Background(), request(core.RiskR1))
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Approved || decision.ApprovedBy != "flag" || decision.Scope != "flag" {
		t.Errorf("decision = %+v", decision)
	}

	decision, err = approver.Approve(context.Background(), request(core.RiskR2))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Approved {
		t.Error("--yes approved an R2 call")
	}
	if !strings.Contains(out.String(), "no interactive input") {
		t.Errorf("output = %q", out.String())
	}
}

func TestApproverDenyWins(t *testing.T) {
	risk := core.RiskR2
	approver := newApprover(strings.NewReader("y\n"), &bytes.Buffer{}, ApprovalOptions{
		Yes:       true,
		Deny:      true,
		AllowUpTo: &risk,
	})
	decision, err := approver.Approve(context.Background(), request(core.RiskR1))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Approved {
		t.Error("--deny did not deny")
	}
}

func TestApproverAllowRisk(t *testing.T) {
	upTo := core.RiskR2
	approver := newApprover(nil, &bytes.Buffer{}, ApprovalOptions{AllowUpTo: &upTo})

	for _, risk := range []core.RiskClass{core.RiskR1, core.RiskR2} {
		decision, err := approver.Approve(context.Background(), request(risk))
		if err != nil {
			t.Fatal(err)
		}
		if !decision.Approved {
			t.Errorf("%s was not approved", risk)
		}
	}
	decision, err := approver.Approve(context.Background(), request(core.RiskR3))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Approved {
		t.Error("--allow-risk R2 approved R3")
	}
}

func TestApproverInteractive(t *testing.T) {
	cases := []struct {
		input    string
		approved bool
		scope    string
		contains string
	}{
		{input: "y\n", approved: true, scope: "once", contains: "approved once"},
		{input: "yes\n", approved: true, scope: "once", contains: "approved once"},
		{input: "a\n", approved: true, scope: "session", contains: "approved for this session"},
		{input: "n\n", approved: false, contains: "denied"},
		{input: "", approved: false, contains: "input closed"},
	}

	for _, test := range cases {
		t.Run(test.input, func(t *testing.T) {
			var out bytes.Buffer
			approver := newApprover(strings.NewReader(test.input), &out, ApprovalOptions{})
			decision, err := approver.Approve(context.Background(), request(core.RiskR1))
			if err != nil {
				t.Fatal(err)
			}
			if decision.Approved != test.approved {
				t.Errorf("approved = %t, want %t", decision.Approved, test.approved)
			}
			if decision.Scope != test.scope {
				t.Errorf("scope = %q, want %q", decision.Scope, test.scope)
			}
			if test.approved && decision.ApprovedBy != "user" {
				t.Errorf("approved_by = %q", decision.ApprovedBy)
			}
			if !strings.Contains(out.String(), test.contains) {
				t.Errorf("output = %q, want %q", out.String(), test.contains)
			}
			if !strings.Contains(out.String(), "fs_create") || !strings.Contains(out.String(), "R1") {
				t.Errorf("prompt does not name the tool and risk: %q", out.String())
			}
		})
	}
}

func TestApproverRemembersSessionApproval(t *testing.T) {
	var out bytes.Buffer
	// Only one line is available: the second call must use the cached session answer.
	approver := newApprover(strings.NewReader("a\n"), &out, ApprovalOptions{})

	first, err := approver.Approve(context.Background(), request(core.RiskR1))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Approved || first.Scope != "session" {
		t.Fatalf("first decision = %+v", first)
	}

	second, err := approver.Approve(context.Background(), request(core.RiskR2))
	if err != nil {
		t.Fatal(err)
	}
	if !second.Approved || second.Scope != "session" {
		t.Errorf("second decision = %+v, want the cached session approval", second)
	}
	if !strings.Contains(out.String(), "approved earlier in this session") {
		t.Errorf("output = %q", out.String())
	}
}

func TestApproverHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	approver := newApprover(strings.NewReader("y\n"), &bytes.Buffer{}, ApprovalOptions{})
	if _, err := approver.Approve(ctx, request(core.RiskR1)); err == nil {
		t.Fatal("Approve ignored a canceled context")
	}
}

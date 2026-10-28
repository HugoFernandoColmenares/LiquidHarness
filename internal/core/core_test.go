// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"errors"
	"testing"

	"github.com/liquidharness/liquidharness/internal/core"
)

func TestExitCodes(t *testing.T) {
	cases := []struct {
		kind core.Code
		want int
	}{
		{core.CodeInternal, 1},
		{core.CodeUsage, 2},
		{core.CodePolicyDenied, 3},
		{core.CodeModelUnavailable, 4},
		{core.CodeToolFailed, 5},
		{core.CodeConfigInvalid, 6},
		{core.CodeCanceled, 7},
		{core.CodeNotFound, 8},
		{core.CodeConflict, 9},
		{core.CodeContextOverflow, 10},
	}
	for _, test := range cases {
		err := core.E(test.kind, "boom")
		if got := core.ExitCode(err); got != test.want {
			t.Errorf("%s exit code = %d, want %d", test.kind, got, test.want)
		}
	}
	if got := core.ExitCode(nil); got != 0 {
		t.Errorf("nil exit code = %d, want 0", got)
	}
	if got := core.ExitCode(errors.New("plain")); got != 1 {
		t.Errorf("plain error exit code = %d, want 1", got)
	}
}

func TestErrorUnwrapAndIdentity(t *testing.T) {
	cause := errors.New("disk full")
	err := core.Wrap(core.CodeToolFailed, cause, "write failed")
	if !errors.Is(err, cause) {
		t.Fatal("wrapped cause is not reachable with errors.Is")
	}
	var coded *core.Error
	if !errors.As(err, &coded) {
		t.Fatal("coded error is not reachable with errors.As")
	}
	if coded.Code() != "TOOL_FAILED" || coded.ExitCode() != 5 || coded.Retryable() {
		t.Errorf("unexpected coded error: %+v", coded)
	}
	if coded.UserMessage() != "write failed" {
		t.Errorf("user message = %q", coded.UserMessage())
	}
	if !core.Is(err, core.CodeToolFailed) {
		t.Error("core.Is did not match")
	}
}

func TestSilentError(t *testing.T) {
	err := core.Silent(core.CodeInternal, "already reported")
	if !err.Silent {
		t.Error("silent flag is not set")
	}
	if core.ExitCode(err) != 1 {
		t.Errorf("exit code = %d", core.ExitCode(err))
	}
}

func TestRiskClasses(t *testing.T) {
	for value := 0; value <= 5; value++ {
		class := core.RiskClass(value)
		if !class.Valid() {
			t.Errorf("%d should be valid", value)
		}
		parsed, err := core.ParseRisk(class.String())
		if err != nil || parsed != class {
			t.Errorf("round trip %s: got %v, %v", class, parsed, err)
		}
	}
	if _, err := core.ParseRisk("R9"); err == nil {
		t.Error("R9 should be rejected")
	}
}

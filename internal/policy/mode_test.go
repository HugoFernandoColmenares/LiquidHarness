// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package policy_test

import (
	"encoding"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/policy"
)

func TestParseMode(t *testing.T) {
	cases := []struct {
		input string
		want  policy.Mode
	}{
		{"read-only", policy.ModeReadOnly},
		{"READ-ONLY", policy.ModeReadOnly},
		{" ask ", policy.ModeAsk},
		{"allow", policy.ModeAllow},
	}
	for _, test := range cases {
		got, err := policy.ParseMode(test.input)
		if err != nil {
			t.Errorf("ParseMode(%q): %v", test.input, err)
			continue
		}
		if got != test.want {
			t.Errorf("ParseMode(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	for _, input := range []string{"", "workspace", "yolo", "askk"} {
		if _, err := policy.ParseMode(input); err == nil {
			t.Errorf("ParseMode(%q) should fail", input)
		}
	}
}

func TestModeAllowsWrites(t *testing.T) {
	if policy.ModeReadOnly.AllowsWrites() {
		t.Error("read-only must not allow writes")
	}
	if !policy.ModeAsk.AllowsWrites() || !policy.ModeAllow.AllowsWrites() {
		t.Error("ask and allow must permit writes subject to policy")
	}
}

func TestModeTextMarshalling(t *testing.T) {
	var _ encoding.TextMarshaler = policy.ModeAsk
	var _ encoding.TextUnmarshaler = (*policy.Mode)(nil)

	var mode policy.Mode
	if err := mode.UnmarshalText([]byte("allow")); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if mode != policy.ModeAllow {
		t.Errorf("mode = %q", mode)
	}
	if err := mode.UnmarshalText([]byte("nope")); err == nil {
		t.Error("invalid mode should fail")
	}
}

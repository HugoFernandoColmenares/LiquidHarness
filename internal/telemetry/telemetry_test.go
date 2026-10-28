// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package telemetry_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/liquidharness/liquidharness/internal/telemetry"
)

func TestRedact(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Authorization: Bearer abc.def.ghi", "Authorization: Bearer ***"},
		{"key sk-1234567890abcdef", "key sk-***"},
		{"api_key=super-secret", "api_key=***"},
		{"token: abc123", "token: ***"},
		{"https://user:pass@example.com/v1", "https://***@example.com/v1"},
		{"nothing to redact", "nothing to redact"},
	}
	for _, test := range cases {
		if got := telemetry.Redact(test.input); got != test.want {
			t.Errorf("Redact(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestNewLogger(t *testing.T) {
	var buffer bytes.Buffer
	logger, err := telemetry.New(telemetry.Options{Level: "debug", Format: "json", Output: &buffer})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	logger.Info("tool finished", "api_key", "sk-1234567890abcdef", "path", "docs/a.md")
	output := buffer.String()
	if strings.Contains(output, "sk-1234567890abcdef") {
		t.Errorf("secret leaked into logs: %s", output)
	}
	if !strings.Contains(output, `"api_key":"***"`) {
		t.Errorf("sensitive attribute was not masked: %s", output)
	}
}

func TestNewLoggerRejectsInvalidOptions(t *testing.T) {
	if _, err := telemetry.New(telemetry.Options{Level: "verbose"}); err == nil {
		t.Error("invalid level should fail")
	}
	if _, err := telemetry.New(telemetry.Options{Format: "xml"}); err == nil {
		t.Error("invalid format should fail")
	}
}

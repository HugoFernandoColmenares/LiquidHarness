// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/app"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/config"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

func newTestApp(t *testing.T) (*app.App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	workspace := t.TempDir()
	base := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	a := app.New(app.Options{
		Stdout:    stdout,
		Stderr:    stderr,
		LookupEnv: func(string) (string, bool) { return "", false },
		Getwd:     func() (string, error) { return workspace, nil },
		PathOptions: config.PathOptions{
			GOOS:          "linux",
			Getenv:        func(string) string { return "" },
			UserConfigDir: func() (string, error) { return filepath.Join(base, "config"), nil },
			UserCacheDir:  func() (string, error) { return filepath.Join(base, "cache"), nil },
			HomeDir:       func() (string, error) { return filepath.Join(base, "home"), nil },
		},
	})
	return a, stdout, stderr
}

func TestConfigureAndDoctor(t *testing.T) {
	a, _, _ := newTestApp(t)
	if err := a.Configure(config.Flags{}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if !a.Configured() {
		t.Fatal("app is not configured")
	}

	report := a.Doctor(context.Background(), true)
	if !report.OK {
		for _, check := range report.Checks {
			t.Logf("%s: %s (%s)", check.Name, check.Status, check.Detail)
		}
		t.Fatalf("doctor reported %d failure(s)", report.Failures)
	}
	if len(report.Checks) == 0 {
		t.Fatal("doctor returned no checks")
	}
	if report.Warnings < 0 || report.Failures != 0 {
		t.Errorf("unexpected counts: %+v", report)
	}
}

func TestReportErrorJSON(t *testing.T) {
	a, stdout, _ := newTestApp(t)
	a.SetJSON(true)
	a.ReportError(core.E(core.CodePolicyDenied, "fs_delete requires approval"))

	var envelope app.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, stdout.String())
	}
	if envelope.OK {
		t.Error("envelope should be a failure")
	}
	if envelope.Error == nil {
		t.Fatal("missing error payload")
	}
	if envelope.Error.Code != "POLICY_DENIED" || envelope.Error.ExitCode != 3 || envelope.Error.Retryable {
		t.Errorf("unexpected payload: %+v", envelope.Error)
	}
	if envelope.SchemaVersion != 1 {
		t.Errorf("schema version = %d", envelope.SchemaVersion)
	}
}

func TestReportErrorSilent(t *testing.T) {
	a, stdout, stderr := newTestApp(t)
	a.SetJSON(true)
	a.ReportError(core.Silent(core.CodeInternal, "already reported"))
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("silent error produced output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestReportErrorHuman(t *testing.T) {
	a, stdout, stderr := newTestApp(t)
	a.ReportError(core.E(core.CodeUsage, "unknown flag"))
	if stdout.Len() != 0 {
		t.Errorf("human error wrote to stdout: %q", stdout.String())
	}
	if got := stderr.String(); got == "" || !bytes.Contains([]byte(got), []byte("USAGE")) {
		t.Errorf("stderr = %q", got)
	}
}

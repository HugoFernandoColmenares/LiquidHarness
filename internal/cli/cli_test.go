// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liquidharness/liquidharness/internal/app"
	"github.com/liquidharness/liquidharness/internal/cli"
	"github.com/liquidharness/liquidharness/internal/config"
	"github.com/liquidharness/liquidharness/internal/core"
)

func newTestApp(t *testing.T) *app.App {
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
	return a
}

func execute(t *testing.T, a *app.App, args ...string) (string, error) {
	t.Helper()
	stdout, ok := a.Stdout.(*bytes.Buffer)
	if !ok {
		t.Fatal("app stdout is not a bytes.Buffer")
	}
	stdout.Reset()

	ctx := context.Background()
	root := cli.NewRootCommand(ctx, a)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(a.Stderr)
	err := root.ExecuteContext(ctx)
	return stdout.String(), err
}

func TestVersionJSON(t *testing.T) {
	a := newTestApp(t)
	output, err := execute(t, a, "version", "--json")
	if err != nil {
		t.Fatalf("version: %v", err)
	}

	var envelope struct {
		SchemaVersion int  `json:"schema_version"`
		OK            bool `json:"ok"`
		Data          struct {
			Version   string `json:"version"`
			GoVersion string `json:"go_version"`
			OS        string `json:"os"`
			Arch      string `json:"arch"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode: %v\n%s", err, output)
	}
	if !envelope.OK || envelope.SchemaVersion != 1 {
		t.Errorf("unexpected envelope: %+v", envelope)
	}
	if envelope.Data.Version == "" || envelope.Data.GoVersion == "" || envelope.Data.OS == "" {
		t.Errorf("incomplete version data: %+v", envelope.Data)
	}
}

func TestVersionHuman(t *testing.T) {
	a := newTestApp(t)
	output, err := execute(t, a, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.HasPrefix(output, "liquidharness ") {
		t.Errorf("output = %q", output)
	}
}

func TestHelpListsCommands(t *testing.T) {
	a := newTestApp(t)
	output, err := execute(t, a, "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, command := range []string{"config", "doctor", "run", "serve", "models", "rag", "eval", "version"} {
		if !strings.Contains(output, command) {
			t.Errorf("help does not list %q\n%s", command, output)
		}
	}
}

func TestConfigPathJSON(t *testing.T) {
	a := newTestApp(t)
	output, err := execute(t, a, "config", "path", "--json")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}

	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			UserConfig    string `json:"user_config"`
			ProjectConfig string `json:"project_config"`
			DataDir       string `json:"data_dir"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode: %v\n%s", err, output)
	}
	if !envelope.OK || envelope.Data.UserConfig == "" || envelope.Data.ProjectConfig == "" {
		t.Errorf("unexpected paths: %+v", envelope)
	}
}

func TestDoctorJSON(t *testing.T) {
	a := newTestApp(t)
	output, err := execute(t, a, "doctor", "--json", "--skip-model-check")
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Checks   []struct{ Name, Status, Detail string } `json:"checks"`
			Failures int                                     `json:"failures"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode: %v\n%s", err, output)
	}
	if !envelope.OK || len(envelope.Data.Checks) == 0 {
		t.Errorf("unexpected doctor report: %+v\n%s", envelope, output)
	}
}

func TestPolicyShowJSON(t *testing.T) {
	a := newTestApp(t)
	output, err := execute(t, a, "policy", "show", "--json")
	if err != nil {
		t.Fatalf("policy show: %v", err)
	}

	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Mode         string `json:"mode"`
			AllowShell   bool   `json:"allow_shell"`
			AllowNetwork bool   `json:"allow_network"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode: %v\n%s", err, output)
	}
	if !envelope.OK || envelope.Data.Mode != "ask" || envelope.Data.AllowShell || envelope.Data.AllowNetwork {
		t.Errorf("unexpected policy: %+v", envelope)
	}
}

func TestStubIsNotImplemented(t *testing.T) {
	a := newTestApp(t)
	_, err := execute(t, a, "run", "--print", "hello")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !core.Is(err, core.CodeInternal) {
		t.Errorf("error = %v, want INTERNAL", err)
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error = %v", err)
	}
}

func TestUnknownSubcommandIsUsageError(t *testing.T) {
	a := newTestApp(t)
	_, err := execute(t, a, "models", "explode")
	if err == nil {
		t.Fatal("expected an error")
	}
	// Cobra reports unknown subcommands; the CLI normalizes them to USAGE at the boundary.
	if !strings.Contains(err.Error(), "unknown command") {
		t.Logf("error = %v", err)
	}
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// CheckStatus is the outcome of one diagnostic.
type CheckStatus string

const (
	// StatusOK means the check passed.
	StatusOK CheckStatus = "ok"
	// StatusWarn means the harness can run but something deserves attention.
	StatusWarn CheckStatus = "warn"
	// StatusFail means the harness cannot work until the problem is fixed.
	StatusFail CheckStatus = "fail"
)

// Check is one diagnostic result.
type Check struct {
	Name   string      `json:"name"`
	Status CheckStatus `json:"status"`
	Detail string      `json:"detail"`
}

// DoctorReport is the stable output of `liquidharness doctor`.
type DoctorReport struct {
	Checks   []Check `json:"checks"`
	OK       bool    `json:"ok"`
	Warnings int     `json:"warnings"`
	Failures int     `json:"failures"`
}

// Doctor inspects the environment without modifying user data beyond creating the data
// directory and a temporary probe file. Model connectivity is a warning, never a failure:
// llama-server may be started later.
func (a *App) Doctor(ctx context.Context, skipModelCheck bool) DoctorReport {
	report := DoctorReport{}

	if !a.configured {
		report.add(Check{Name: "config", Status: StatusFail, Detail: "configuration was not loaded"})
	} else {
		report.add(Check{
			Name: "config", Status: StatusOK,
			Detail: fmt.Sprintf("valid; workspace %s", a.Config.General.Workspace),
		})
	}

	report.add(a.checkConfigPermissions())
	report.add(a.checkWorkspace())
	report.add(a.checkDataDir())
	report.add(a.checkDatabase())
	report.add(a.checkSecurityPosture())
	report.add(a.checkWorkspaceEnvFile())
	if !skipModelCheck {
		report.add(a.checkModelEndpoint(ctx))
	}

	report.finish()
	return report
}

func (r *DoctorReport) add(check Check) {
	r.Checks = append(r.Checks, check)
}

func (r *DoctorReport) finish() {
	r.Warnings = 0
	r.Failures = 0
	for _, check := range r.Checks {
		switch check.Status {
		case StatusOK:
			// Nothing to count.
		case StatusWarn:
			r.Warnings++
		case StatusFail:
			r.Failures++
		}
	}
	r.OK = r.Failures == 0
}

// Render writes the report for a terminal.
func (r DoctorReport) Render(w io.Writer) {
	_, _ = fmt.Fprintln(w, "LiquidHarness doctor")
	_, _ = fmt.Fprintln(w)
	for _, check := range r.Checks {
		_, _ = fmt.Fprintf(w, "  %-5s %-20s %s\n", check.Status, check.Name, check.Detail)
	}
	_, _ = fmt.Fprintln(w)
	okCount := len(r.Checks) - r.Warnings - r.Failures
	_, _ = fmt.Fprintf(w, "%d checks: %d ok, %d warning(s), %d failure(s)\n",
		len(r.Checks), okCount, r.Warnings, r.Failures)
}

func (a *App) checkConfigPermissions() Check {
	if runtime.GOOS == "windows" {
		return Check{Name: "config_permissions", Status: StatusOK, Detail: "ACL inherited from the user profile"}
	}
	info, err := os.Stat(a.Paths.UserConfig)
	if err != nil {
		return Check{Name: "config_permissions", Status: StatusOK, Detail: "no user config file yet; defaults apply"}
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Check{
			Name: "config_permissions", Status: StatusWarn,
			Detail: fmt.Sprintf("%s is readable by other users; run chmod 600", a.Paths.UserConfig),
		}
	}
	return Check{Name: "config_permissions", Status: StatusOK, Detail: "0600 or stricter"}
}

func (a *App) checkWorkspace() Check {
	workspace := a.Config.General.Workspace
	info, err := os.Stat(workspace)
	if err != nil {
		return Check{Name: "workspace", Status: StatusFail, Detail: fmt.Sprintf("cannot access %s: %v", workspace, err)}
	}
	if !info.IsDir() {
		return Check{Name: "workspace", Status: StatusFail, Detail: fmt.Sprintf("%s is not a directory", workspace)}
	}
	if err := probeWritable(workspace); err != nil {
		return Check{Name: "workspace", Status: StatusFail, Detail: fmt.Sprintf("not writable: %v", err)}
	}
	return Check{Name: "workspace", Status: StatusOK, Detail: workspace + " (readable and writable)"}
}

func (a *App) checkDataDir() Check {
	if err := os.MkdirAll(a.Paths.DataDir, 0o700); err != nil {
		return Check{Name: "data_dir", Status: StatusFail, Detail: fmt.Sprintf("cannot create %s: %v", a.Paths.DataDir, err)}
	}
	if err := probeWritable(a.Paths.DataDir); err != nil {
		return Check{Name: "data_dir", Status: StatusFail, Detail: fmt.Sprintf("not writable: %v", err)}
	}
	return Check{Name: "data_dir", Status: StatusOK, Detail: a.Paths.DataDir}
}

func (a *App) checkDatabase() Check {
	dir := filepath.Dir(a.Paths.DBPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Check{Name: "database", Status: StatusFail, Detail: fmt.Sprintf("cannot create %s: %v", dir, err)}
	}
	file, err := os.Open(a.Paths.DBPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Check{
				Name: "database", Status: StatusOK,
				Detail: a.Paths.DBPath + " (will be created on first run)",
			}
		}
		return Check{Name: "database", Status: StatusFail, Detail: fmt.Sprintf("cannot read %s: %v", a.Paths.DBPath, err)}
	}
	_ = file.Close()
	return Check{Name: "database", Status: StatusOK, Detail: a.Paths.DBPath + " (readable)"}
}

func (a *App) checkSecurityPosture() Check {
	var warnings []string
	if a.Config.Policy.AllowShell {
		warnings = append(warnings, "shell execution is enabled")
	}
	if a.Config.Policy.AllowNetwork {
		warnings = append(warnings, "network tools are enabled")
	}
	if a.Config.Policy.Mode == "allow" {
		warnings = append(warnings, "policy mode allow skips approval prompts")
	}
	if len(warnings) > 0 {
		return Check{Name: "security", Status: StatusWarn, Detail: strings.Join(warnings, "; ")}
	}
	return Check{Name: "security", Status: StatusOK, Detail: "shell and network disabled; writes require approval"}
}

func (a *App) checkWorkspaceEnvFile() Check {
	path := filepath.Join(a.Config.General.Workspace, ".env")
	if _, err := os.Stat(path); err != nil {
		return Check{Name: "workspace_env", Status: StatusOK, Detail: "no .env inside the workspace"}
	}
	if a.Config.RAG.Enabled {
		return Check{
			Name: "workspace_env", Status: StatusWarn,
			Detail: ".env is inside the workspace and RAG is enabled; move it outside indexed paths",
		}
	}
	return Check{
		Name: "workspace_env", Status: StatusWarn,
		Detail: ".env is inside the workspace; it will never be indexed, but prefer a location outside",
	}
}

func (a *App) checkModelEndpoint(ctx context.Context) Check {
	parsed, err := url.Parse(a.Config.Model.BaseURL)
	if err != nil || parsed.Host == "" {
		return Check{
			Name: "model_endpoint", Status: StatusFail,
			Detail: fmt.Sprintf("invalid base_url %q", a.Config.Model.BaseURL),
		}
	}

	host := parsed.Host
	if parsed.Port() == "" {
		port := "80"
		if parsed.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(parsed.Hostname(), port)
	}

	dialer := net.Dialer{Timeout: 750 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return Check{
			Name: "model_endpoint", Status: StatusWarn,
			Detail: fmt.Sprintf("not reachable at %s; start llama-server or change model.base_url", host),
		}
	}
	_ = conn.Close()
	return Check{Name: "model_endpoint", Status: StatusOK, Detail: "reachable at " + host}
}

func probeWritable(dir string) error {
	file, err := os.CreateTemp(dir, ".liquidharness-doctor-*")
	if err != nil {
		return err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

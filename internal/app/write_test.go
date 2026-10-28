// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/app"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/config"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/store"
)

func auditRecords(t *testing.T, workspace string) []store.AuditRecord {
	t.Helper()
	dir := filepath.Join(workspace, ".liquidharness", "audit")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read audit dir: %v", err)
	}
	var records []store.AuditRecord
	for _, entry := range entries {
		file, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var record store.AuditRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				_ = file.Close()
				t.Fatalf("decode audit record: %v", err)
			}
			records = append(records, record)
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return records
}

func TestRunTurnDeniesWriteWithoutApproval(t *testing.T) {
	a, _, stderr, workspace := newAppWithInput(t, nil, config.Flags{},
		`<|tool_call_start|>[fs_create(path="notes.md", content="hello")]<|tool_call_end|>`,
		"I could not write the file.",
	)

	result, err := a.RunTurn(context.Background(), app.RunOptions{Prompt: "write notes"})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if result.Answer != "I could not write the file." {
		t.Errorf("answer = %q", result.Answer)
	}
	if _, err := os.Stat(filepath.Join(workspace, "notes.md")); !os.IsNotExist(err) {
		t.Errorf("file was created without approval: %v", err)
	}
	if !strings.Contains(stderr.String(), "denied") {
		t.Errorf("stderr = %q", stderr.String())
	}

	records := auditRecords(t, workspace)
	if len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
	record := records[0]
	if record.Tool != "fs_create" || record.Decision != "deny" || record.Result != "denied" {
		t.Errorf("record = %+v", record)
	}
	if record.ArgsHash == "" || record.ArgsRedacted["path"] != "notes.md" {
		t.Errorf("record = %+v", record)
	}
	if _, ok := record.ArgsRedacted["content"]; ok {
		t.Error("content was not redacted in the audit log")
	}
}

func TestRunTurnApprovesWriteInteractively(t *testing.T) {
	a, _, stderr, workspace := newAppWithInput(t, strings.NewReader("y\n"), config.Flags{},
		`<|tool_call_start|>[fs_create(path="notes.md", content="hello")]<|tool_call_end|>`,
		"Written.",
	)

	result, err := a.RunTurn(context.Background(), app.RunOptions{Prompt: "write notes"})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if result.Answer != "Written." {
		t.Errorf("answer = %q", result.Answer)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "notes.md"))
	if err != nil {
		t.Fatalf("file was not created: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q", data)
	}
	if !strings.Contains(stderr.String(), "needs approval") {
		t.Errorf("stderr = %q", stderr.String())
	}

	records := auditRecords(t, workspace)
	if len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
	record := records[0]
	if record.Decision != "allow" || record.ApprovedBy != "user" ||
		record.ApprovalScope != "once" || record.Result != "ok" {
		t.Errorf("record = %+v", record)
	}
	if record.BytesOut == 0 {
		t.Error("bytes_out was not recorded")
	}
}

func TestRunTurnYesApprovesR1(t *testing.T) {
	a, _, _, workspace := newAppWithInput(t, nil, config.Flags{},
		`<|tool_call_start|>[fs_create(path="notes.md", content="hello")]<|tool_call_end|>`,
		"Written.",
	)

	_, err := a.RunTurn(context.Background(), app.RunOptions{
		Prompt:   "write notes",
		Approval: app.ApprovalOptions{Yes: true},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "notes.md")); err != nil {
		t.Errorf("file was not created: %v", err)
	}
	records := auditRecords(t, workspace)
	if len(records) != 1 || records[0].ApprovedBy != "flag" {
		t.Errorf("records = %+v", records)
	}
}

func TestRunTurnYesDoesNotApproveR2(t *testing.T) {
	a, _, _, workspace := newAppWithInput(t, nil, config.Flags{},
		`<|tool_call_start|>[fs_delete(path="notes.md")]<|tool_call_end|>`,
		"I could not delete it.",
	)
	path := filepath.Join(workspace, "notes.md")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := a.RunTurn(context.Background(), app.RunOptions{
		Prompt:   "delete notes",
		Approval: app.ApprovalOptions{Yes: true},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("R2 delete ran under --yes: %v", err)
	}
	records := auditRecords(t, workspace)
	if len(records) != 1 || records[0].Decision != "deny" {
		t.Errorf("records = %+v", records)
	}
}

func TestRunTurnAllowModeWritesWithoutApproval(t *testing.T) {
	mode := "allow"
	a, _, stderr, workspace := newAppWithInput(t, nil, config.Flags{PolicyMode: &mode},
		`<|tool_call_start|>[fs_create(path="notes.md", content="hello")]<|tool_call_end|>`,
		"Written.",
	)

	if _, err := a.RunTurn(context.Background(), app.RunOptions{Prompt: "write"}); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "notes.md")); err != nil {
		t.Errorf("file was not created: %v", err)
	}
	if !strings.Contains(stderr.String(), "policy mode is allow") {
		t.Errorf("stderr = %q, want an allow-mode warning", stderr.String())
	}
	records := auditRecords(t, workspace)
	if len(records) != 1 || records[0].Rule != "mode-allow" {
		t.Errorf("records = %+v", records)
	}
}

func TestRunTurnReadOnlyHidesWriteTools(t *testing.T) {
	mode := "read-only"
	a, _, stderr, workspace := newAppWithInput(t, nil, config.Flags{PolicyMode: &mode},
		`<|tool_call_start|>[fs_create(path="notes.md", content="hello")]<|tool_call_end|>`,
		"I cannot write in read-only mode.",
	)

	if _, err := a.RunTurn(context.Background(), app.RunOptions{Prompt: "write"}); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "notes.md")); !os.IsNotExist(err) {
		t.Errorf("file was created in read-only mode: %v", err)
	}
	if !strings.Contains(stderr.String(), "UNKNOWN_TOOL") &&
		!strings.Contains(stderr.String(), "unknown tool") {
		t.Errorf("stderr = %q, want an unknown-tool report", stderr.String())
	}
}

func TestRunTurnAllowRiskCoversR2(t *testing.T) {
	a, _, _, workspace := newAppWithInput(t, nil, config.Flags{},
		`<|tool_call_start|>[fs_delete(path="old.txt")]<|tool_call_end|>`,
		"Deleted.",
	)
	if err := os.WriteFile(filepath.Join(workspace, "old.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	upTo := core.RiskR2
	_, err := a.RunTurn(context.Background(), app.RunOptions{
		Prompt:   "delete",
		Approval: app.ApprovalOptions{AllowUpTo: &upTo},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "old.txt")); !os.IsNotExist(err) {
		t.Errorf("file was not deleted: %v", err)
	}
	records := auditRecords(t, workspace)
	if len(records) != 1 || records[0].Result != "ok" || records[0].ApprovedBy != "flag" {
		t.Errorf("records = %+v", records)
	}
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/store"
)

type recordingEmitter struct {
	events []core.Event
}

func (e *recordingEmitter) Emit(_ context.Context, event core.Event) error {
	e.events = append(e.events, event)
	return nil
}

func auditEvent(t *testing.T, kind core.EventType, data any) core.Event {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return core.Event{
		Type:      kind,
		SessionID: "session-1",
		TurnID:    "turn-1",
		Data:      raw,
	}
}

func readAudit(t *testing.T, dir string) []store.AuditRecord {
	t.Helper()
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
				t.Fatalf("decode %s: %v", entry.Name(), err)
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

func TestAuditEmitterRecordsSuccessfulCall(t *testing.T) {
	workspace := t.TempDir()
	log, err := store.OpenAudit(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	render := &recordingEmitter{}
	emitter := newAuditEmitter(render, log, nil)
	ctx := context.Background()

	requested := auditEvent(t, core.EventToolRequested, map[string]any{
		"call_id":   "call-1",
		"name":      "fs_create",
		"risk":      "R1",
		"arguments": json.RawMessage(`{"path":"notes.md","content":"secret body","overwrite":true}`),
		"args_hash": "sha256:abc",
		"decision":  "allow",
		"rule":      "mode-allow",
		"reason":    "",
	})
	finished := auditEvent(t, core.EventToolFinished, map[string]any{
		"call_id": "call-1",
		"name":    "fs_create",
		"result": core.ToolResult{
			CallID: "call-1",
			OK:     true,
			Meta:   core.ToolMeta{DurationMS: 7, Bytes: 42},
		},
	})
	for _, event := range []core.Event{requested, finished} {
		if err := emitter.Emit(ctx, event); err != nil {
			t.Fatal(err)
		}
	}

	records := readAudit(t, log.Dir())
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	record := records[0]
	if record.CallID != "call-1" || record.Tool != "fs_create" || record.Risk != "R1" {
		t.Errorf("record = %+v", record)
	}
	if record.ArgsHash != "sha256:abc" || record.Decision != "allow" || record.Rule != "mode-allow" {
		t.Errorf("record = %+v", record)
	}
	if record.Result != "ok" || record.DurationMS != 7 || record.BytesOut != 42 {
		t.Errorf("record = %+v", record)
	}
	if record.SessionID != "session-1" || record.TurnID != "turn-1" {
		t.Errorf("record = %+v", record)
	}
	if _, ok := record.ArgsRedacted["content"]; ok {
		t.Error("content was not redacted")
	}
	if got := record.ArgsRedacted["content_bytes"]; got != float64(len("secret body")) {
		t.Errorf("content_bytes = %v", got)
	}
	if record.ArgsRedacted["path"] != "notes.md" || record.ArgsRedacted["overwrite"] != true {
		t.Errorf("args_redacted = %+v", record.ArgsRedacted)
	}
	if len(render.events) != 2 {
		t.Errorf("renderer saw %d events, want 2", len(render.events))
	}
}

func TestAuditEmitterRecordsApproval(t *testing.T) {
	workspace := t.TempDir()
	log, err := store.OpenAudit(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	emitter := newAuditEmitter(&recordingEmitter{}, log, nil)
	ctx := context.Background()

	events := []core.Event{
		auditEvent(t, core.EventToolRequested, map[string]any{
			"call_id": "call-1", "name": "fs_delete", "risk": "R2",
			"arguments": json.RawMessage(`{"path":"old.txt"}`), "args_hash": "sha256:def",
			"decision": "ask", "rule": "mode-ask-write",
		}),
		auditEvent(t, core.EventToolApproved, map[string]any{
			"call_id": "call-1", "approved_by": "user", "approval_scope": "session",
		}),
		auditEvent(t, core.EventToolFinished, map[string]any{
			"call_id": "call-1",
			"result":  core.ToolResult{CallID: "call-1", OK: true},
		}),
	}
	for _, event := range events {
		if err := emitter.Emit(ctx, event); err != nil {
			t.Fatal(err)
		}
	}

	records := readAudit(t, log.Dir())
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0].Decision != "allow" || records[0].ApprovedBy != "user" ||
		records[0].ApprovalScope != "session" {
		t.Errorf("record = %+v", records[0])
	}
}

func TestAuditEmitterRecordsDenial(t *testing.T) {
	workspace := t.TempDir()
	log, err := store.OpenAudit(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	emitter := newAuditEmitter(&recordingEmitter{}, log, nil)
	ctx := context.Background()

	events := []core.Event{
		auditEvent(t, core.EventToolRequested, map[string]any{
			"call_id": "call-1", "name": "fs_delete", "risk": "R2",
			"arguments": json.RawMessage(`{"path":"old.txt"}`), "args_hash": "sha256:def",
			"decision": "ask", "rule": "mode-ask-write",
		}),
		auditEvent(t, core.EventToolDenied, map[string]any{
			"call_id": "call-1", "rule": "mode-ask-write", "reason": "the user denied this call",
		}),
	}
	for _, event := range events {
		if err := emitter.Emit(ctx, event); err != nil {
			t.Fatal(err)
		}
	}

	records := readAudit(t, log.Dir())
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	record := records[0]
	if record.Decision != "deny" || record.Result != "denied" || record.ErrorCode != "POLICY_DENIED" {
		t.Errorf("record = %+v", record)
	}
}

func TestAuditEmitterReportsWriteFailure(t *testing.T) {
	log, err := store.OpenAudit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	// Replace the directory with a file so the next open fails.
	if err := os.RemoveAll(log.Dir()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log.Dir(), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	var reported []error
	emitter := newAuditEmitter(&recordingEmitter{}, log, func(err error) {
		reported = append(reported, err)
	})
	ctx := context.Background()
	event := auditEvent(t, core.EventToolRequested, map[string]any{
		"call_id": "call-1", "name": "fs_read", "risk": "R0", "args_hash": "sha256:abc",
		"decision": "allow", "rule": "mode-allow",
	})
	denied := auditEvent(t, core.EventToolDenied, map[string]any{"call_id": "call-1", "reason": "x"})
	if err := emitter.Emit(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(ctx, denied); err != nil {
		t.Fatalf("audit failure aborted the turn: %v", err)
	}
	if len(reported) != 1 {
		t.Errorf("reported %d failures, want 1", len(reported))
	}
}

var _ ports.Emitter = (*auditEmitter)(nil)

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package store_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/store"
)

func readLines(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return lines
}

func TestAuditAppendsJSONL(t *testing.T) {
	workspace := t.TempDir()
	log, err := store.OpenAudit(workspace)
	if err != nil {
		t.Fatalf("OpenAudit: %v", err)
	}
	defer func() {
		if err := log.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	if _, err := os.Stat(log.Dir()); err != nil {
		t.Fatalf("audit dir: %v", err)
	}

	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	records := []store.AuditRecord{
		{
			TS: now, CallID: "call-1", Tool: "fs_read", Risk: "R0", ArgsHash: "sha256:aaa",
			Decision: "allow", Rule: "mode-allow", Result: "ok", DurationMS: 3, BytesOut: 10,
		},
		{
			TS: now.Add(time.Second), CallID: "call-2", Tool: "fs_create", Risk: "R1", ArgsHash: "sha256:bbb",
			Decision: "deny", Rule: "mode-ask-write", Result: "denied", ErrorCode: "POLICY_DENIED",
		},
	}
	for _, record := range records {
		if err := log.Append(record); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	path := filepath.Join(log.Dir(), "audit-20261028.jsonl")
	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	for i, line := range lines {
		var decoded store.AuditRecord
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("line %d is not JSON: %v", i, err)
		}
		if decoded.CallID != records[i].CallID || decoded.Tool != records[i].Tool {
			t.Errorf("line %d = %+v", i, decoded)
		}
	}
	if strings.Contains(lines[1], "\n") {
		t.Error("record contains an embedded newline")
	}
}

func TestAuditRotatesByDay(t *testing.T) {
	workspace := t.TempDir()
	log, err := store.OpenAudit(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	day1 := time.Date(2026, 10, 27, 23, 59, 0, 0, time.UTC)
	day2 := time.Date(2026, 10, 28, 0, 1, 0, 0, time.UTC)
	if err := log.Append(store.AuditRecord{TS: day1, CallID: "a", Tool: "fs_read", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(store.AuditRecord{TS: day2, CallID: "b", Tool: "fs_read", Result: "ok"}); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"audit-20261027.jsonl", "audit-20261028.jsonl"} {
		if _, err := os.Stat(filepath.Join(log.Dir(), name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAuditDefaultsTimestampAndReopens(t *testing.T) {
	workspace := t.TempDir()
	log, err := store.OpenAudit(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Append(store.AuditRecord{CallID: "a", Tool: "fs_read", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	// Appending after Close must reopen the same day file and keep both records.
	if err := log.Append(store.AuditRecord{CallID: "b", Tool: "fs_read", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(log.Dir(), "audit-"+time.Now().UTC().Format("20060102")+".jsonl")
	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	var first store.AuditRecord
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.TS.IsZero() {
		t.Error("TS was not defaulted")
	}
}

func TestOpenAuditCreatesDir(t *testing.T) {
	// filepath.Abs("") resolves to the working directory, so an empty workspace is valid here;
	// the guard that matters is that the directory is created below it.
	log, err := store.OpenAudit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	if !strings.HasSuffix(log.Dir(), filepath.Join(".liquidharness", "audit")) {
		t.Errorf("dir = %q", log.Dir())
	}
}

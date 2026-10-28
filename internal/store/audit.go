// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package store holds the append-only artifacts of a workspace: the audit log today and the
// session database in F4.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// auditDir is the workspace-relative directory that holds the audit log.
const auditDir = ".liquidharness/audit"

// AuditRecord is one line of the JSONL audit log. The field names are a stable contract: an
// external SIEM can consume the file without knowing the harness internals.
type AuditRecord struct {
	TS            time.Time      `json:"ts"`
	SessionID     string         `json:"session_id,omitempty"`
	TurnID        string         `json:"turn_id,omitempty"`
	CallID        string         `json:"call_id"`
	Tool          string         `json:"tool"`
	Risk          string         `json:"risk"`
	ArgsHash      string         `json:"args_hash"`
	ArgsRedacted  map[string]any `json:"args_redacted,omitempty"`
	Decision      string         `json:"decision"`
	Rule          string         `json:"rule"`
	ApprovedBy    string         `json:"approved_by,omitempty"`
	ApprovalScope string         `json:"approval_scope,omitempty"`
	Result        string         `json:"result"`
	DurationMS    int64          `json:"duration_ms"`
	BytesOut      int            `json:"bytes_out"`
	ErrorCode     string         `json:"error_code,omitempty"`
}

// AuditLog appends records to <workspace>/.liquidharness/audit/audit-YYYYMMDD.jsonl. It opens
// the file lazily, so a turn that never calls a tool leaves no empty file behind.
type AuditLog struct {
	dir string

	mu   sync.Mutex
	file *os.File
	day  string
}

// OpenAudit prepares the audit directory of workspace. It does not create the log file.
func OpenAudit(workspace string) (*AuditLog, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	dir := filepath.Join(abs, filepath.FromSlash(auditDir))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create audit directory: %w", err)
	}
	return &AuditLog{dir: dir}, nil
}

// Dir returns the absolute audit directory.
func (l *AuditLog) Dir() string { return l.dir }

// Append writes one record as a single JSON line. A record without TS gets the current UTC
// time, and the file rotates at UTC midnight.
func (l *AuditLog) Append(record AuditRecord) error {
	if record.TS.IsZero() {
		record.TS = time.Now().UTC()
	}
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode audit record: %w", err)
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	day := record.TS.UTC().Format("20060102")
	if l.file == nil || l.day != day {
		if l.file != nil {
			_ = l.file.Close()
			l.file = nil
		}
		path := filepath.Join(l.dir, "audit-"+day+".jsonl")
		file, openErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if openErr != nil {
			return fmt.Errorf("open audit log: %w", openErr)
		}
		l.file = file
		l.day = day
	}
	if _, err := l.file.Write(line); err != nil {
		return fmt.Errorf("append audit record: %w", err)
	}
	return nil
}

// Close flushes and closes the current file. It is safe to call more than once.
func (l *AuditLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	if err != nil {
		return fmt.Errorf("close audit log: %w", err)
	}
	return nil
}

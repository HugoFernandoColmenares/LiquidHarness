// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/tools"
)

func newWriteWorkspace(t *testing.T) (*tools.Registry, string) {
	t.Helper()
	root := t.TempDir()
	registry, err := tools.NewWorkspace(root, tools.Options{MaxWriteBytes: 1 << 20})
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return registry, root
}

func writeFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func wantError(t *testing.T, result core.ToolResult, code string) {
	t.Helper()
	if result.OK || result.Error == nil {
		t.Fatalf("result = %+v, want error %s", result, code)
	}
	if result.Error.Code != code {
		t.Fatalf("error code = %q, want %q (%s)", result.Error.Code, code, result.Error.Message)
	}
}

func TestNewWorkspaceRegistersTools(t *testing.T) {
	registry, _ := newWriteWorkspace(t)
	want := map[string]core.RiskClass{
		"fs_create":  core.RiskR1,
		"fs_delete":  core.RiskR2,
		"fs_edit":    core.RiskR1,
		"fs_list":    core.RiskR0,
		"fs_move":    core.RiskR1,
		"fs_read":    core.RiskR0,
		"fs_search":  core.RiskR0,
		"todo_write": core.RiskR0,
	}
	if registry.Len() != len(want) {
		t.Fatalf("registered %d tools, want %d: %v", registry.Len(), len(want), registry.Names())
	}
	for name, risk := range want {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if tool.Spec().Risk != risk {
			t.Errorf("%s risk = %s, want %s", name, tool.Spec().Risk, risk)
		}
		if !json.Valid(tool.Spec().InputSchema) {
			t.Errorf("%s has an invalid schema", name)
		}
	}
}

func TestFSCreate(t *testing.T) {
	registry, root := newWriteWorkspace(t)

	result := execute(t, registry, "fs_create", `{"path":"docs/new.txt","content":"hello"}`)
	if !result.OK {
		t.Fatalf("create: %+v", result.Error)
	}
	data := decode[struct {
		Path    string `json:"path"`
		Bytes   int    `json:"bytes"`
		SHA256  string `json:"sha256"`
		Created bool   `json:"created"`
	}](t, result)
	if data.Path != "docs/new.txt" || data.Bytes != 5 || !data.Created {
		t.Errorf("data = %+v", data)
	}
	if data.SHA256 != hashOf("hello") {
		t.Errorf("sha256 = %q, want %q", data.SHA256, hashOf("hello"))
	}
	path := filepath.Join(root, "docs", "new.txt")
	if got := readFile(t, path); got != "hello" {
		t.Errorf("content = %q", got)
	}

	wantError(t, execute(t, registry, "fs_create", `{"path":"docs/new.txt","content":"again"}`), "CONFLICT")

	result = execute(t, registry, "fs_create",
		`{"path":"docs/new.txt","content":"replaced","overwrite":true}`)
	if !result.OK {
		t.Fatalf("overwrite: %+v", result.Error)
	}
	data = decode[struct {
		Path    string `json:"path"`
		Bytes   int    `json:"bytes"`
		SHA256  string `json:"sha256"`
		Created bool   `json:"created"`
	}](t, result)
	if data.Created {
		t.Error("overwrite reported created=true")
	}
	if got := readFile(t, path); got != "replaced" {
		t.Errorf("content after overwrite = %q", got)
	}

	wantError(t, execute(t, registry, "fs_create",
		`{"path":"docs/missing/new.txt","content":"x","create_dirs":false}`), "NOT_FOUND")
	wantError(t, execute(t, registry, "fs_create", `{"path":"../escape.txt","content":"x"}`),
		"OUTSIDE_WORKSPACE")
	wantError(t, execute(t, registry, "fs_create", `{"path":".","content":"x"}`),
		"INVALID_ARGUMENT")
}

func TestFSCreateLimitsSize(t *testing.T) {
	root := t.TempDir()
	registry, err := tools.NewWorkspace(root, tools.Options{MaxWriteBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	wantError(t, execute(t, registry, "fs_create", `{"path":"big.txt","content":"12345"}`), "TOO_LARGE")
}

func TestFSCreateRiskEscalatesOnOverwrite(t *testing.T) {
	registry, _ := newWriteWorkspace(t)
	tool, ok := registry.Get("fs_create")
	if !ok {
		t.Fatal("fs_create is not registered")
	}
	assessor, ok := tool.(ports.RiskAssessor)
	if !ok {
		t.Fatal("fs_create does not implement RiskAssessor")
	}
	call := func(arguments string) core.RiskClass {
		return assessor.RiskFor(core.ToolCall{Name: "fs_create", Arguments: json.RawMessage(arguments)})
	}
	if got := call(`{"path":"a.txt","content":"x"}`); got != core.RiskR1 {
		t.Errorf("fresh create risk = %s, want R1", got)
	}
	if got := call(`{"path":"a.txt","content":"x","overwrite":true}`); got != core.RiskR2 {
		t.Errorf("overwrite risk = %s, want R2", got)
	}
}

func TestFSEdit(t *testing.T) {
	registry, root := newWriteWorkspace(t)
	path := writeFile(t, root, "notes.md", "alpha\nbeta\n")

	result := execute(t, registry, "fs_edit", `{
		"path":"notes.md",
		"expected_sha256":"`+hashOf("alpha\nbeta\n")+`",
		"edits":[{"old":"beta","new":"gamma"}]
	}`)
	if !result.OK {
		t.Fatalf("edit: %+v", result.Error)
	}
	data := decode[struct {
		Path     string `json:"path"`
		Bytes    int    `json:"bytes"`
		SHA256   string `json:"sha256"`
		Edits    int    `json:"edits"`
		Diff     string `json:"diff"`
		Verified bool   `json:"verified"`
	}](t, result)
	if !data.Verified || data.Edits != 1 {
		t.Errorf("data = %+v", data)
	}
	if data.SHA256 != hashOf("alpha\ngamma\n") {
		t.Errorf("sha256 = %q", data.SHA256)
	}
	if !strings.Contains(data.Diff, "-beta") || !strings.Contains(data.Diff, "+gamma") {
		t.Errorf("diff = %q", data.Diff)
	}
	if got := readFile(t, path); got != "alpha\ngamma\n" {
		t.Errorf("content = %q", got)
	}
}

func TestFSEditGuards(t *testing.T) {
	registry, root := newWriteWorkspace(t)
	writeFile(t, root, "notes.md", "alpha\nbeta\n")
	writeFile(t, root, "repeat.txt", "x\nx\n")
	writeFile(t, root, "binary.dat", "\x00\x01")

	wantError(t, execute(t, registry, "fs_edit",
		`{"path":"notes.md","edits":[{"old":"beta","new":"gamma"}]}`), "INVALID_ARGUMENT")
	wantError(t, execute(t, registry, "fs_edit",
		`{"path":"notes.md","expected_sha256":"`+hashOf("stale")+`","edits":[{"old":"beta","new":"gamma"}]}`),
		"CONFLICT")
	wantError(t, execute(t, registry, "fs_edit",
		`{"path":"repeat.txt","expected_sha256":"`+hashOf("x\nx\n")+`","edits":[{"old":"x","new":"y"}]}`),
		"CONFLICT")
	wantError(t, execute(t, registry, "fs_edit",
		`{"path":"notes.md","expected_sha256":"`+hashOf("alpha\nbeta\n")+`","edits":[{"old":"missing","new":"y"}]}`),
		"NOT_FOUND")
	wantError(t, execute(t, registry, "fs_edit",
		`{"path":"binary.dat","expected_sha256":"`+hashOf("\x00\x01")+`","edits":[{"old":"a","new":"b"}]}`),
		"BINARY_FILE")
	wantError(t, execute(t, registry, "fs_edit",
		`{"path":"../escape.md","expected_sha256":"`+hashOf("x")+`","edits":[{"old":"a","new":"b"}]}`),
		"OUTSIDE_WORKSPACE")

	result := execute(t, registry, "fs_edit",
		`{"path":"repeat.txt","expected_sha256":"`+hashOf("x\nx\n")+`","edits":[{"old":"x","new":"y","replace_all":true}]}`)
	if !result.OK {
		t.Fatalf("replace_all: %+v", result.Error)
	}
	if got := readFile(t, filepath.Join(root, "repeat.txt")); got != "y\ny\n" {
		t.Errorf("content = %q", got)
	}
}

func TestFSDeleteMovesToTrash(t *testing.T) {
	registry, root := newWriteWorkspace(t)
	path := writeFile(t, root, "docs/old.txt", "keep me")

	result := execute(t, registry, "fs_delete", `{"path":"docs/old.txt"}`)
	if !result.OK {
		t.Fatalf("delete: %+v", result.Error)
	}
	data := decode[struct {
		Path      string `json:"path"`
		TrashedTo string `json:"trashed_to"`
		Deleted   bool   `json:"deleted"`
		Permanent bool   `json:"permanent"`
	}](t, result)
	if data.Path != "docs/old.txt" || data.TrashedTo == "" || data.Deleted || data.Permanent {
		t.Errorf("data = %+v", data)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("source still exists: %v", err)
	}
	trash := filepath.Join(root, filepath.FromSlash(data.TrashedTo))
	if got := readFile(t, trash); got != "keep me" {
		t.Errorf("trash content = %q", got)
	}
}

func TestFSDeleteGuards(t *testing.T) {
	registry, root := newWriteWorkspace(t)
	writeFile(t, root, "docs/nested/file.txt", "x")

	wantError(t, execute(t, registry, "fs_delete", `{"path":"docs"}`), "INVALID_ARGUMENT")
	wantError(t, execute(t, registry, "fs_delete", `{"path":"."}`), "INVALID_ARGUMENT")
	wantError(t, execute(t, registry, "fs_delete", `{"path":"../escape.txt"}`), "OUTSIDE_WORKSPACE")
	wantError(t, execute(t, registry, "fs_delete", `{"path":"missing.txt"}`), "NOT_FOUND")

	result := execute(t, registry, "fs_delete", `{"path":"docs","recursive":true,"permanent":true}`)
	if !result.OK {
		t.Fatalf("recursive delete: %+v", result.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Errorf("directory still exists: %v", err)
	}
}

func TestFSMove(t *testing.T) {
	registry, root := newWriteWorkspace(t)
	writeFile(t, root, "docs/one.txt", "one")
	writeFile(t, root, "docs/two.txt", "two")

	result := execute(t, registry, "fs_move", `{"from":"docs/one.txt","to":"docs/renamed.txt"}`)
	if !result.OK {
		t.Fatalf("move: %+v", result.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "one.txt")); !os.IsNotExist(err) {
		t.Errorf("source still exists: %v", err)
	}
	if got := readFile(t, filepath.Join(root, "docs", "renamed.txt")); got != "one" {
		t.Errorf("content = %q", got)
	}

	wantError(t, execute(t, registry, "fs_move",
		`{"from":"docs/renamed.txt","to":"docs/two.txt"}`), "CONFLICT")
	wantError(t, execute(t, registry, "fs_move",
		`{"from":"docs/renamed.txt","to":"docs/renamed.txt"}`), "INVALID_ARGUMENT")
	wantError(t, execute(t, registry, "fs_move",
		`{"from":"docs/renamed.txt","to":"../escape.txt"}`), "OUTSIDE_WORKSPACE")
	wantError(t, execute(t, registry, "fs_move",
		`{"from":"missing.txt","to":"docs/x.txt"}`), "NOT_FOUND")

	result = execute(t, registry, "fs_move",
		`{"from":"docs/renamed.txt","to":"docs/two.txt","overwrite":true}`)
	if !result.OK {
		t.Fatalf("overwrite move: %+v", result.Error)
	}
	if got := readFile(t, filepath.Join(root, "docs", "two.txt")); got != "one" {
		t.Errorf("content after overwrite = %q", got)
	}

	result = execute(t, registry, "fs_move", `{"from":"docs","to":"moved-docs"}`)
	if !result.OK {
		t.Fatalf("move directory: %+v", result.Error)
	}
	if got := readFile(t, filepath.Join(root, "moved-docs", "two.txt")); got != "one" {
		t.Errorf("moved directory content = %q", got)
	}
}

func TestFSMoveRiskEscalatesOnOverwrite(t *testing.T) {
	registry, _ := newWriteWorkspace(t)
	tool, _ := registry.Get("fs_move")
	assessor, ok := tool.(ports.RiskAssessor)
	if !ok {
		t.Fatal("fs_move does not implement RiskAssessor")
	}
	if got := assessor.RiskFor(core.ToolCall{Arguments: json.RawMessage(`{"from":"a","to":"b"}`)}); got != core.RiskR1 {
		t.Errorf("plain move risk = %s, want R1", got)
	}
	if got := assessor.RiskFor(core.ToolCall{Arguments: json.RawMessage(`{"from":"a","to":"b","overwrite":true}`)}); got != core.RiskR2 {
		t.Errorf("overwrite move risk = %s, want R2", got)
	}
}

func TestTodoWrite(t *testing.T) {
	registry, root := newWriteWorkspace(t)

	result := execute(t, registry, "todo_write", `{"items":[
		{"text":"read the docs","status":"done"},
		{"text":"write the patch","status":"in_progress"},
		{"text":"run the tests","status":"pending"}
	]}`)
	if !result.OK {
		t.Fatalf("todo_write: %+v", result.Error)
	}
	data := decode[struct {
		Items []struct {
			Text   string `json:"text"`
			Status string `json:"status"`
		} `json:"items"`
		Pending   int `json:"pending"`
		Completed int `json:"completed"`
	}](t, result)
	if len(data.Items) != 3 || data.Completed != 1 || data.Pending != 2 {
		t.Errorf("data = %+v", data)
	}

	wantError(t, execute(t, registry, "todo_write", `{"items":[{"text":"x","status":"blocked"}]}`),
		"INVALID_ARGUMENT")
	wantError(t, execute(t, registry, "todo_write", `{"items":[{"text":" ","status":"pending"}]}`),
		"INVALID_ARGUMENT")

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("todo_write touched the workspace: %v", entries)
	}
}

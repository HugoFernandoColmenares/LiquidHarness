// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/tools"
)

func newWorkspace(t *testing.T) *tools.Registry {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"README.md":     "# Demo\nsecond line\nthird line\n",
		"docs/guide.md": "hello world\nHELLO again\n",
		"docs/code.go":  "package demo\n\nfunc main() {}\n",
		".git/config":   "secret",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "binary.dat"), []byte{0x00, 0x01, 0x02}, 0o600); err != nil {
		t.Fatal(err)
	}

	registry, err := tools.NewReadOnly(root)
	if err != nil {
		t.Fatalf("NewReadOnly: %v", err)
	}
	return registry
}

func execute(t *testing.T, registry *tools.Registry, name, arguments string) core.ToolResult {
	t.Helper()
	tool, ok := registry.Get(name)
	if !ok {
		t.Fatalf("%s is not registered", name)
	}
	result, err := tool.Execute(context.Background(), core.ToolCall{
		ID:        "call-1",
		Name:      name,
		Arguments: json.RawMessage(arguments),
	})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if result.CallID != "call-1" {
		t.Errorf("%s call id = %q", name, result.CallID)
	}
	return result
}

func decode[T any](t *testing.T, result core.ToolResult) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(result.Data, &value); err != nil {
		t.Fatalf("decode %s: %v", result.Data, err)
	}
	return value
}

func TestNewReadOnlyRegistersTools(t *testing.T) {
	registry := newWorkspace(t)
	names := registry.Names()
	want := []string{"fs_list", "fs_read", "fs_search"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for _, spec := range registry.Specs() {
		if spec.Risk != core.RiskR0 {
			t.Errorf("%s risk = %s, want R0", spec.Name, spec.Risk)
		}
		if !json.Valid(spec.InputSchema) {
			t.Errorf("%s has an invalid schema", spec.Name)
		}
	}
}

func TestFSRead(t *testing.T) {
	registry := newWorkspace(t)

	result := execute(t, registry, "fs_read", `{"path":"README.md"}`)
	if !result.OK || result.Error != nil {
		t.Fatalf("result = %+v", result)
	}
	data := decode[struct {
		Path      string `json:"path"`
		Lines     int    `json:"lines"`
		Offset    int    `json:"offset"`
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
		SHA256    string `json:"sha256"`
	}](t, result)
	if data.Path != "README.md" || data.Lines != 3 {
		t.Errorf("data = %+v", data)
	}
	if data.Content != "# Demo\nsecond line\nthird line" {
		t.Errorf("content = %q", data.Content)
	}
	whole := hashOf("# Demo\nsecond line\nthird line\n")
	if data.SHA256 != whole {
		t.Errorf("sha256 = %q, want %q", data.SHA256, whole)
	}

	slice := execute(t, registry, "fs_read", `{"path":"README.md","offset":1,"limit":1}`)
	sliceData := decode[struct {
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
		SHA256    string `json:"sha256"`
	}](t, slice)
	if sliceData.Content != "second line" || !sliceData.Truncated {
		t.Errorf("slice = %+v", sliceData)
	}
	if sliceData.SHA256 != whole {
		t.Errorf("slice sha256 = %q, want the whole-file hash %q", sliceData.SHA256, whole)
	}
	if !slice.Meta.Truncated {
		t.Error("meta.truncated = false")
	}
}

func TestFSReadErrors(t *testing.T) {
	registry := newWorkspace(t)

	cases := []struct {
		name      string
		arguments string
		code      string
	}{
		{"missing", `{"path":"nope.txt"}`, "NOT_FOUND"},
		{"directory", `{"path":"docs"}`, "IS_DIRECTORY"},
		{"escape", `{"path":"../outside.txt"}`, "OUTSIDE_WORKSPACE"},
		{"binary", `{"path":"binary.dat"}`, "BINARY_FILE"},
		{"no path", `{}`, "INVALID_ARGUMENT"},
		{"bad json", `{`, "INVALID_ARGUMENT"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := execute(t, registry, "fs_read", test.arguments)
			if result.OK || result.Error == nil {
				t.Fatalf("result = %+v, want an error", result)
			}
			if result.Error.Code != test.code {
				t.Errorf("code = %q, want %q", result.Error.Code, test.code)
			}
		})
	}
}

func TestFSList(t *testing.T) {
	registry := newWorkspace(t)

	shallow := decode[struct {
		Entries []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"entries"`
		Truncated bool `json:"truncated"`
	}](t, execute(t, registry, "fs_list", `{"path":"."}`))

	paths := make([]string, 0, len(shallow.Entries))
	for _, entry := range shallow.Entries {
		paths = append(paths, entry.Path)
		if strings.HasPrefix(entry.Path, ".git") {
			t.Errorf("the .git directory must be skipped: %v", paths)
		}
	}
	if !contains(paths, "README.md") || !contains(paths, "docs") {
		t.Errorf("entries = %v", paths)
	}
	if contains(paths, "docs/guide.md") {
		t.Errorf("depth 1 returned nested entries: %v", paths)
	}

	deep := decode[struct {
		Entries []struct {
			Path string `json:"path"`
		} `json:"entries"`
	}](t, execute(t, registry, "fs_list", `{"path":"docs","depth":2}`))
	if !contains(pathsOf(deep.Entries), "docs/code.go") {
		t.Errorf("depth 2 missed nested files: %+v", deep.Entries)
	}

	limited := execute(t, registry, "fs_list", `{"path":".","limit":1}`)
	if !limited.Meta.Truncated {
		t.Error("limit 1 did not report truncation")
	}
}

func TestFSListErrors(t *testing.T) {
	registry := newWorkspace(t)
	result := execute(t, registry, "fs_list", `{"path":"README.md"}`)
	if result.Error == nil || result.Error.Code != "NOT_DIRECTORY" {
		t.Fatalf("result = %+v", result)
	}
}

func TestFSSearch(t *testing.T) {
	registry := newWorkspace(t)

	result := execute(t, registry, "fs_search", `{"pattern":"hello"}`)
	data := decode[struct {
		Matches []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"matches"`
		FilesScanned int  `json:"files_scanned"`
		Truncated    bool `json:"truncated"`
	}](t, result)
	if len(data.Matches) != 2 {
		t.Fatalf("matches = %+v", data.Matches)
	}
	if data.Matches[0].Path != "docs/guide.md" || data.Matches[0].Line != 1 {
		t.Errorf("first match = %+v", data.Matches[0])
	}

	filtered := decode[struct {
		Matches []struct {
			Path string `json:"path"`
		} `json:"matches"`
	}](t, execute(t, registry, "fs_search", `{"pattern":"package","glob":"*.go"}`))
	if len(filtered.Matches) != 1 || filtered.Matches[0].Path != "docs/code.go" {
		t.Errorf("glob filter = %+v", filtered.Matches)
	}

	limited := execute(t, registry, "fs_search", `{"pattern":"line","max_results":1}`)
	if !limited.Meta.Truncated {
		t.Error("max_results 1 did not report truncation")
	}
}

func TestFSSearchErrors(t *testing.T) {
	registry := newWorkspace(t)

	invalid := execute(t, registry, "fs_search", `{"pattern":"[unclosed"}`)
	if invalid.Error == nil || invalid.Error.Code != "INVALID_PATTERN" {
		t.Errorf("invalid pattern = %+v", invalid)
	}

	escape := execute(t, registry, "fs_search", `{"pattern":"root","path":".."}`)
	if escape.Error == nil || escape.Error.Code != "OUTSIDE_WORKSPACE" {
		t.Errorf("escape = %+v", escape)
	}

	missing := execute(t, registry, "fs_search", `{"pattern":"x","path":"nope"}`)
	if missing.Error == nil || missing.Error.Code != "NOT_FOUND" {
		t.Errorf("missing = %+v", missing)
	}
}

func TestRegistryValidation(t *testing.T) {
	registry := tools.NewRegistry()

	if err := registry.Register(nil); err == nil {
		t.Error("nil tool must fail")
	}
	if err := registry.Register(fakeTool{name: "Bad Name"}); err == nil {
		t.Error("invalid name must fail")
	}
	if err := registry.Register(fakeTool{name: "empty_description"}); err == nil {
		t.Error("empty description must fail")
	}
	if err := registry.Register(fakeTool{name: "bad_schema", schema: `{`}); err == nil {
		t.Error("invalid schema must fail")
	}
	if err := registry.Register(fakeTool{name: "bad_risk", risk: core.RiskClass(9)}); err == nil {
		t.Error("invalid risk must fail")
	}
	good := fakeTool{name: "fs_read"}
	if err := registry.Register(good); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.Register(good); err == nil {
		t.Error("duplicate registration must fail")
	}
}

func TestFSListHonoursCancellation(t *testing.T) {
	registry := newWorkspace(t)
	tool, _ := registry.Get("fs_list")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := tool.Execute(ctx, core.ToolCall{
		ID:        "call-1",
		Name:      "fs_list",
		Arguments: json.RawMessage(`{"path":"."}`),
	}); !core.Is(err, core.CodeCanceled) {
		t.Fatalf("error = %v, want CANCELED", err)
	}
}

type fakeTool struct {
	name   string
	schema string
	risk   core.RiskClass
}

func (f fakeTool) Spec() core.ToolSpec {
	schema := f.schema
	if schema == "" {
		schema = `{"type":"object"}`
	}
	description := "fake tool"
	if f.name == "empty_description" {
		description = ""
	}
	return core.ToolSpec{
		Name:        f.name,
		Description: description,
		InputSchema: json.RawMessage(schema),
		Risk:        f.risk,
	}
}

func (f fakeTool) Execute(context.Context, core.ToolCall) (core.ToolResult, error) {
	return core.ToolResult{}, nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func pathsOf(entries []struct {
	Path string `json:"path"`
},
) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	return paths
}

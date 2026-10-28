// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePythonicCall(t *testing.T) {
	raw := ToolCallStart +
		`[fs_read(path="docs/notes.md", limit=10, recursive=True, tags=["a", "b"])]` +
		ToolCallEnd + "Reading the file."

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Content != "Reading the file." {
		t.Errorf("content = %q", parsed.Content)
	}
	if len(parsed.ToolCalls) != 1 {
		t.Fatalf("calls = %d", len(parsed.ToolCalls))
	}
	if parsed.ToolCalls[0].Name != "fs_read" {
		t.Errorf("name = %q", parsed.ToolCalls[0].Name)
	}
	if parsed.ToolCalls[0].Raw == "" {
		t.Error("raw block was not preserved")
	}

	args := decodeArguments(t, parsed.ToolCalls[0].Arguments)
	if args["path"] != "docs/notes.md" {
		t.Errorf("path = %v", args["path"])
	}
	if args["limit"] != float64(10) {
		t.Errorf("limit = %v", args["limit"])
	}
	if args["recursive"] != true {
		t.Errorf("recursive = %v", args["recursive"])
	}
	tags, ok := args["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("tags = %v", args["tags"])
	}
}

func TestParseJSONCalls(t *testing.T) {
	raw := "Before " + ToolCallStart +
		`[{"name":"fs_read","arguments":{"path":"x"}},` +
		`{"name":"fs_list","arguments":"{\"path\":\"y\"}"}]` +
		ToolCallEnd + " after"

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Content != "Before   after" {
		t.Errorf("content = %q", parsed.Content)
	}
	if len(parsed.ToolCalls) != 2 {
		t.Fatalf("calls = %d", len(parsed.ToolCalls))
	}
	if parsed.ToolCalls[0].Name != "fs_read" || parsed.ToolCalls[1].Name != "fs_list" {
		t.Errorf("names = %q, %q", parsed.ToolCalls[0].Name, parsed.ToolCalls[1].Name)
	}
	if args := decodeArguments(t, parsed.ToolCalls[1].Arguments); args["path"] != "y" {
		t.Errorf("double-encoded arguments = %v", args)
	}
}

func TestParseJSONFunctionEnvelope(t *testing.T) {
	raw := ToolCallStart +
		`[{"id":"call_1","type":"function","function":{"name":"fs_read","arguments":{"path":"x"}}}]` +
		ToolCallEnd

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.ToolCalls) != 1 {
		t.Fatalf("calls = %d", len(parsed.ToolCalls))
	}
	if parsed.ToolCalls[0].ID != "call_1" || parsed.ToolCalls[0].Name != "fs_read" {
		t.Errorf("call = %+v", parsed.ToolCalls[0])
	}
}

func TestParseThinking(t *testing.T) {
	raw := ThinkStart + "plan the answer" + ThinkEnd +
		ToolCallStart + `[fs_read(path="x")]` + ToolCallEnd + "answer"

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Reasoning != "plan the answer" {
		t.Errorf("reasoning = %q", parsed.Reasoning)
	}
	if parsed.Content != "answer" {
		t.Errorf("content = %q", parsed.Content)
	}
	if len(parsed.ToolCalls) != 1 {
		t.Errorf("calls = %d", len(parsed.ToolCalls))
	}
}

func TestParseFencedJSON(t *testing.T) {
	raw := "```json\n[{\"name\":\"fs_read\",\"arguments\":{\"path\":\"x\"}}]\n```\nDone"

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Content != "Done" {
		t.Errorf("content = %q", parsed.Content)
	}
	if len(parsed.ToolCalls) != 1 || parsed.ToolCalls[0].Name != "fs_read" {
		t.Errorf("calls = %+v", parsed.ToolCalls)
	}
}

func TestParseNoToolCalls(t *testing.T) {
	raw := "Just a normal answer with a list [1, 2, 3]."

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Content != raw {
		t.Errorf("content = %q", parsed.Content)
	}
	if len(parsed.ToolCalls) != 0 {
		t.Errorf("calls = %+v", parsed.ToolCalls)
	}
}

func TestParseRejectsMalformedCalls(t *testing.T) {
	tests := map[string]string{
		"missing argument value": ToolCallStart + `[fs_read(path=)]` + ToolCallEnd,
		"non-object arguments":   ToolCallStart + `[{"name":"fs_read","arguments":["x"]}]` + ToolCallEnd,
		"scalar arguments":       ToolCallStart + `[{"name":"fs_read","arguments":42}]` + ToolCallEnd,
		"missing name":           ToolCallStart + `[{"arguments":{"path":"x"}}]` + ToolCallEnd,
		"unterminated block":     ToolCallStart + `[fs_read(path="x")]`,
		"unterminated thinking":  ThinkStart + "plan",
		"positional arguments":   ToolCallStart + `[fs_read("x")]` + ToolCallEnd,
		"invalid name":           ToolCallStart + `[fs read(path="x")]` + ToolCallEnd,
	}

	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(raw); err == nil {
				t.Fatalf("expected an error for %q", raw)
			}
		})
	}
}

func TestParseRejectsTooManyCalls(t *testing.T) {
	var builder strings.Builder
	builder.WriteString(ToolCallStart)
	builder.WriteString("[")
	for index := range MaxToolCalls + 1 {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(`fs_read(path="x")`)
	}
	builder.WriteString("]")
	builder.WriteString(ToolCallEnd)

	if _, err := Parse(builder.String()); err == nil {
		t.Fatal("expected the call limit to be enforced")
	}
}

func TestMarkerFilterSplitsMarkers(t *testing.T) {
	var filter markerFilter
	var output strings.Builder

	chunks := []string{
		"hello <|tool_call_st",
		"art|>[fs_read(path=\"x\")]<|tool_call_end|> world",
	}
	for _, chunk := range chunks {
		output.WriteString(filter.Write(chunk))
	}
	rest, err := filter.Flush()
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	output.WriteString(rest)

	if strings.Contains(output.String(), "<|tool_call") {
		t.Errorf("marker leaked: %q", output.String())
	}
	if !strings.Contains(output.String(), "hello") || !strings.Contains(output.String(), "world") {
		t.Errorf("visible text lost: %q", output.String())
	}
}

func TestMarkerFilterRemovesThinking(t *testing.T) {
	var filter markerFilter
	first := filter.Write("before  thinkingprivate")
	second := filter.Write(" response after")
	rest, err := filter.Flush()
	if err != nil {
		t.Fatalf("flush: %v", err)
	}

	got := first + second + rest
	if strings.Contains(got, "private") {
		t.Errorf("reasoning leaked: %q", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("visible text lost: %q", got)
	}
}

func TestMarkerFilterRejectsTruncatedBlock(t *testing.T) {
	var filter markerFilter
	filter.Write(ToolCallStart + `[fs_read(path="x")`)
	if _, err := filter.Flush(); err == nil {
		t.Fatal("expected a truncated block error")
	}
}

func decodeArguments(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatalf("decode arguments %q: %v", raw, err)
	}
	return args
}

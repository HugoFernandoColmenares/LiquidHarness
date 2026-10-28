// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

const (
	defaultReadLines = 400
	maxReadLines     = 2000
	maxReadBytes     = 8 << 20
	binarySniffBytes = 8 << 10
)

// fsRead returns a bounded slice of a UTF-8 text file inside the workspace.
type fsRead struct {
	root string
	spec core.ToolSpec
}

func newFSRead(root string) *fsRead {
	return &fsRead{
		root: root,
		spec: core.ToolSpec{
			Name: "fs_read",
			Description: "Read a text file from the workspace. Returns numbered content slices " +
				"with an optional line offset and limit, plus the SHA-256 of the whole file that " +
				"fs_edit requires. Use fs_list or fs_search to find paths.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path relative to the workspace root"},
    "offset": {"type": "integer", "minimum": 0, "description": "First line to return, 0-based"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 2000, "description": "Maximum lines to return"}
  },
  "required": ["path"],
  "additionalProperties": false
}`),
			Risk: core.RiskR0,
		},
	}
}

func (t *fsRead) Spec() core.ToolSpec { return t.spec }

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

type readData struct {
	Path      string `json:"path"`
	Lines     int    `json:"lines"`
	Offset    int    `json:"offset"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	// SHA256 covers the whole file, not the returned slice, so the model can hand it to
	// fs_edit as expected_sha256.
	SHA256 string `json:"sha256"`
}

func (t *fsRead) Execute(_ context.Context, call core.ToolCall) (core.ToolResult, error) {
	started := time.Now()

	var args readArgs
	if toolErr := decodeArgs(call, &args); toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "fs_read requires a path",
			Hint:    "pass a file path relative to the workspace root",
		}, started), nil
	}
	if args.Offset < 0 {
		args.Offset = 0
	}
	if args.Limit <= 0 {
		args.Limit = defaultReadLines
	}
	if args.Limit > maxReadLines {
		args.Limit = maxReadLines
	}

	abs, toolErr := resolvePath(t.root, args.Path)
	if toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return newFailure(call, classify(err, args.Path), started), nil
	}
	if info.IsDir() {
		return newFailure(call, &core.ToolError{
			Code:    errIsDirectory,
			Message: fmt.Sprintf("%q is a directory", args.Path),
			Hint:    "use fs_list to inspect a directory",
		}, started), nil
	}
	if info.Size() > maxReadBytes {
		return newFailure(call, &core.ToolError{
			Code:    errFileTooLarge,
			Message: fmt.Sprintf("%q is larger than %d bytes", args.Path, maxReadBytes),
			Hint:    "use fs_search to find the relevant lines instead",
		}, started), nil
	}

	raw, err := os.ReadFile(abs)
	if err != nil {
		return newFailure(call, classify(err, args.Path), started), nil
	}
	if isBinary(raw) {
		return newFailure(call, &core.ToolError{
			Code:    errBinaryFile,
			Message: fmt.Sprintf("%q is a binary file", args.Path),
			Hint:    "only UTF-8 text files can be read",
		}, started), nil
	}

	lines := splitLines(string(raw))
	total := len(lines)
	sum := sha256.Sum256(raw)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if args.Offset >= total {
		return newResult(call, readData{
			Path:    relativeTo(t.root, abs),
			Lines:   total,
			Offset:  args.Offset,
			Content: "",
			SHA256:  digest,
		}, false, started), nil
	}
	end := min(args.Offset+args.Limit, total)
	selected := lines[args.Offset:end]
	truncated := end < total

	return newResult(call, readData{
		Path:      relativeTo(t.root, abs),
		Lines:     total,
		Offset:    args.Offset,
		Content:   strings.Join(selected, "\n"),
		Truncated: truncated,
		SHA256:    digest,
	}, truncated, started), nil
}

// splitLines splits text into lines without keeping the trailing newline, and drops the empty
// element that a final newline would otherwise produce.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// isBinary sniffs the first block for a NUL byte, the same heuristic git uses.
func isBinary(raw []byte) bool {
	head := raw
	if len(head) > binarySniffBytes {
		head = head[:binarySniffBytes]
	}
	return bytes.IndexByte(head, 0) >= 0
}

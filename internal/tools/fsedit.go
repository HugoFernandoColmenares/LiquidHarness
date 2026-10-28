// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/fsx"
)

const (
	maxEdits       = 20
	maxEditOldSize = 64 << 10
)

// fsEdit applies hash-guarded string replacements to an existing text file.
type fsEdit struct {
	root     string
	maxBytes int64
	spec     core.ToolSpec
}

func newFSEdit(root string, maxBytes int64) *fsEdit {
	if maxBytes <= 0 {
		maxBytes = defaultMaxWriteBytes
	}
	return &fsEdit{
		root:     root,
		maxBytes: maxBytes,
		spec: core.ToolSpec{
			Name: "fs_edit",
			Description: "Edit a text file with exact string replacements. Requires the current " +
				"SHA-256 of the file, rejects ambiguous replacements unless replace_all is set, " +
				"writes atomically and returns a unified-style diff.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path relative to the workspace root"},
    "expected_sha256": {"type": "string", "description": "SHA-256 of the current content, as returned by fs_read or fs_edit"},
    "edits": {
      "type": "array",
      "minItems": 1,
      "maxItems": 20,
      "items": {
        "type": "object",
        "properties": {
          "old": {"type": "string", "description": "Exact text to replace"},
          "new": {"type": "string", "description": "Replacement text"},
          "replace_all": {"type": "boolean", "description": "Replace every occurrence; defaults to false"}
        },
        "required": ["old", "new"],
        "additionalProperties": false
      }
    }
  },
  "required": ["path", "expected_sha256", "edits"],
  "additionalProperties": false
}`),
			Risk: core.RiskR1,
		},
	}
}

func (t *fsEdit) Spec() core.ToolSpec { return t.spec }

type editOperation struct {
	Old        string `json:"old"`
	New        string `json:"new"`
	ReplaceAll bool   `json:"replace_all"`
}

type editArgs struct {
	Path           string          `json:"path"`
	ExpectedSHA256 string          `json:"expected_sha256"`
	Edits          []editOperation `json:"edits"`
}

type editData struct {
	Path     string `json:"path"`
	Bytes    int    `json:"bytes"`
	SHA256   string `json:"sha256"`
	Edits    int    `json:"edits"`
	Diff     string `json:"diff"`
	Verified bool   `json:"verified"`
}

func (t *fsEdit) Execute(_ context.Context, call core.ToolCall) (core.ToolResult, error) {
	started := time.Now()

	var args editArgs
	if toolErr := decodeArgs(call, &args); toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "fs_edit requires a path",
			Hint:    "pass a file path relative to the workspace root",
		}, started), nil
	}
	expected := normalizeHash(args.ExpectedSHA256)
	if expected == "" {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "fs_edit requires expected_sha256",
			Hint:    "read the file with fs_read and pass the sha256 it returns",
		}, started), nil
	}
	if len(args.Edits) == 0 {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "fs_edit requires at least one edit",
		}, started), nil
	}
	if len(args.Edits) > maxEdits {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: fmt.Sprintf("fs_edit accepts at most %d edits", maxEdits),
		}, started), nil
	}
	for i, edit := range args.Edits {
		if edit.Old == "" {
			return newFailure(call, &core.ToolError{
				Code:    errInvalidArguments,
				Message: fmt.Sprintf("edit %d has an empty old string", i+1),
			}, started), nil
		}
		if len(edit.Old) > maxEditOldSize {
			return newFailure(call, &core.ToolError{
				Code:    errTooLarge,
				Message: fmt.Sprintf("edit %d old string exceeds %d bytes", i+1, maxEditOldSize),
			}, started), nil
		}
	}

	abs, toolErr := resolvePathForWrite(t.root, args.Path)
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
		}, started), nil
	}
	if info.Size() > t.maxBytes {
		return newFailure(call, &core.ToolError{
			Code:    errTooLarge,
			Message: fmt.Sprintf("%q is larger than %d bytes", args.Path, t.maxBytes),
		}, started), nil
	}

	before, err := os.ReadFile(abs)
	if err != nil {
		return newFailure(call, classify(err, args.Path), started), nil
	}
	if isBinary(before) {
		return newFailure(call, &core.ToolError{
			Code:    errBinaryFile,
			Message: fmt.Sprintf("%q is a binary file", args.Path),
		}, started), nil
	}
	current := sha256.Sum256(before)
	if hex.EncodeToString(current[:]) != expected {
		return newFailure(call, &core.ToolError{
			Code:    errConflict,
			Message: fmt.Sprintf("%q changed since it was read", args.Path),
			Hint:    "read the file again and retry with the new sha256",
		}, started), nil
	}

	text := string(before)
	for i, edit := range args.Edits {
		count := strings.Count(text, edit.Old)
		switch {
		case count == 0:
			return newFailure(call, &core.ToolError{
				Code:    errNotFound,
				Message: fmt.Sprintf("edit %d: old string not found in %q", i+1, args.Path),
				Hint:    "copy the exact text from fs_read, including indentation",
			}, started), nil
		case count > 1 && !edit.ReplaceAll:
			return newFailure(call, &core.ToolError{
				Code:    errConflict,
				Message: fmt.Sprintf("edit %d: old string appears %d times in %q", i+1, count, args.Path),
				Hint:    "add more context or set replace_all=true",
			}, started), nil
		case edit.ReplaceAll:
			text = strings.ReplaceAll(text, edit.Old, edit.New)
		default:
			text = strings.Replace(text, edit.Old, edit.New, 1)
		}
	}

	if int64(len(text)) > t.maxBytes {
		return newFailure(call, &core.ToolError{
			Code:    errTooLarge,
			Message: fmt.Sprintf("result is %d bytes, limit is %d", len(text), t.maxBytes),
		}, started), nil
	}

	if err := fsx.AtomicWrite(abs, []byte(text), info.Mode().Perm()); err != nil {
		return newFailure(call, classify(err, args.Path), started), nil
	}
	// Re-read so the reported hash is the hash on disk, not the hash in memory.
	written, err := os.ReadFile(abs)
	if err != nil {
		return newFailure(call, classify(err, args.Path), started), nil
	}
	digest := sha256.Sum256(written)

	return newResult(call, editData{
		Path:     relativeTo(t.root, abs),
		Bytes:    len(written),
		SHA256:   "sha256:" + hex.EncodeToString(digest[:]),
		Edits:    len(args.Edits),
		Diff:     unifiedDiff(string(before), string(written)),
		Verified: string(written) == text,
	}, false, started), nil
}

// normalizeHash accepts "sha256:abc" and "abc" and returns the lowercase hex digest.
func normalizeHash(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) != 64 {
		return ""
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return ""
		}
	}
	return value
}

// unifiedDiff renders the changed region with a common prefix and suffix trim. It is a compact
// review aid, not a patch that can be applied.
func unifiedDiff(before, after string) string {
	oldLines := strings.Split(before, "\n")
	newLines := strings.Split(after, "\n")

	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	var out strings.Builder
	for _, line := range oldLines[prefix : len(oldLines)-suffix] {
		out.WriteString("-")
		out.WriteString(line)
		out.WriteString("\n")
	}
	for _, line := range newLines[prefix : len(newLines)-suffix] {
		out.WriteString("+")
		out.WriteString(line)
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

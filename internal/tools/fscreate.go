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
	"path/filepath"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/fsx"
)

const defaultMaxWriteBytes = 1 << 20

// fsCreate writes a new text file inside the workspace.
type fsCreate struct {
	root     string
	maxBytes int64
	spec     core.ToolSpec
}

func newFSCreate(root string, maxBytes int64) *fsCreate {
	if maxBytes <= 0 {
		maxBytes = defaultMaxWriteBytes
	}
	return &fsCreate{
		root:     root,
		maxBytes: maxBytes,
		spec: core.ToolSpec{
			Name: "fs_create",
			Description: "Create a text file inside the workspace. Fails when the file exists " +
				"unless overwrite is true, which turns the call into a destructive R2 action.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path relative to the workspace root"},
    "content": {"type": "string", "description": "UTF-8 file content"},
    "overwrite": {"type": "boolean", "description": "Replace an existing file (R2); defaults to false"},
    "create_dirs": {"type": "boolean", "description": "Create missing parent directories; defaults to true"}
  },
  "required": ["path", "content"],
  "additionalProperties": false
}`),
			Risk: core.RiskR1,
		},
	}
}

func (t *fsCreate) Spec() core.ToolSpec { return t.spec }

type createArgs struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Overwrite  bool   `json:"overwrite"`
	CreateDirs *bool  `json:"create_dirs"`
}

type writeData struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
	// Created is true for a new file and false for a replacement.
	Created bool `json:"created"`
}

// RiskFor escalates overwrite to R2.
func (t *fsCreate) RiskFor(call core.ToolCall) core.RiskClass {
	var args createArgs
	if err := json.Unmarshal(call.Arguments, &args); err == nil && args.Overwrite {
		return core.RiskR2
	}
	return core.RiskR1
}

func (t *fsCreate) Execute(_ context.Context, call core.ToolCall) (core.ToolResult, error) {
	started := time.Now()

	var args createArgs
	if toolErr := decodeArgs(call, &args); toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "fs_create requires a path",
			Hint:    "pass a file path relative to the workspace root",
		}, started), nil
	}
	if int64(len(args.Content)) > t.maxBytes {
		return newFailure(call, &core.ToolError{
			Code:    errTooLarge,
			Message: fmt.Sprintf("content is %d bytes, limit is %d", len(args.Content), t.maxBytes),
			Hint:    "split the write into smaller files",
		}, started), nil
	}

	abs, toolErr := resolvePathForWrite(t.root, args.Path)
	if toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}

	exists := false
	if info, statErr := os.Stat(abs); statErr == nil {
		if info.IsDir() {
			return newFailure(call, &core.ToolError{
				Code:    errIsDirectory,
				Message: fmt.Sprintf("%q is a directory", args.Path),
			}, started), nil
		}
		exists = true
	} else if !os.IsNotExist(statErr) {
		return newFailure(call, classify(statErr, args.Path), started), nil
	}
	if exists && !args.Overwrite {
		return newFailure(call, &core.ToolError{
			Code:    errConflict,
			Message: fmt.Sprintf("%q already exists", args.Path),
			Hint:    "read the file first, or pass overwrite=true if replacing it is intended",
		}, started), nil
	}

	createDirs := true
	if args.CreateDirs != nil {
		createDirs = *args.CreateDirs
	}
	if createDirs {
		if err := fsx.EnsureParent(abs); err != nil {
			return newFailure(call, classify(err, args.Path), started), nil
		}
	} else {
		parentOK := false
		if info, statErr := os.Stat(filepath.Dir(abs)); statErr == nil && info.IsDir() {
			parentOK = true
		}
		if !parentOK {
			return newFailure(call, &core.ToolError{
				Code:    errNotFound,
				Message: fmt.Sprintf("parent directory of %q does not exist", args.Path),
				Hint:    "create it first or pass create_dirs=true",
			}, started), nil
		}
	}

	if err := fsx.AtomicWrite(abs, []byte(args.Content), 0o640); err != nil {
		return newFailure(call, classify(err, args.Path), started), nil
	}

	digest := sha256.Sum256([]byte(args.Content))
	return newResult(call, writeData{
		Path:    relativeTo(t.root, abs),
		Bytes:   len(args.Content),
		SHA256:  "sha256:" + hex.EncodeToString(digest[:]),
		Created: !exists,
	}, false, started), nil
}

// resolvePathForWrite maps a model path to an absolute path and rejects escapes.
func resolvePathForWrite(root, rel string) (string, *core.ToolError) {
	abs, err := fsx.ResolveForWrite(root, rel)
	if err == nil {
		return abs, nil
	}
	if isOutsideError(err) {
		return "", &core.ToolError{
			Code:    errOutsideWorkspace,
			Message: fmt.Sprintf("%q is outside the workspace", rel),
			Hint:    "use a path relative to the workspace root",
		}
	}
	return "", &core.ToolError{
		Code:    errInvalidArguments,
		Message: err.Error(),
		Hint:    "use a plain relative file path",
	}
}

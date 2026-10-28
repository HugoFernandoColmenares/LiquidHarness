// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/fsx"
)

// fsDelete removes a file or directory. By default it moves the entry to the workspace trash so
// the operation is reversible; permanent deletion is an explicit, audited choice.
type fsDelete struct {
	root string
	spec core.ToolSpec
}

func newFSDelete(root string) *fsDelete {
	return &fsDelete{
		root: root,
		spec: core.ToolSpec{
			Name: "fs_delete",
			Description: "Delete a file or directory inside the workspace. By default the entry " +
				"is moved to .liquidharness/trash so it can be recovered; permanent deletion " +
				"requires permanent=true.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File or directory relative to the workspace root"},
    "recursive": {"type": "boolean", "description": "Allow deleting a non-empty directory"},
    "permanent": {"type": "boolean", "description": "Delete instead of moving to the trash"}
  },
  "required": ["path"],
  "additionalProperties": false
}`),
			Risk: core.RiskR2,
		},
	}
}

func (t *fsDelete) Spec() core.ToolSpec { return t.spec }

type deleteArgs struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	Permanent bool   `json:"permanent"`
}

type deleteData struct {
	Path      string `json:"path"`
	TrashedTo string `json:"trashed_to,omitempty"`
	Deleted   bool   `json:"deleted"`
	Permanent bool   `json:"permanent"`
}

func (t *fsDelete) Execute(_ context.Context, call core.ToolCall) (core.ToolResult, error) {
	started := time.Now()

	var args deleteArgs
	if toolErr := decodeArgs(call, &args); toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "fs_delete requires a path",
		}, started), nil
	}

	abs, err := fsx.ResolveWithin(t.root, args.Path)
	if err != nil {
		if isOutsideError(err) {
			return newFailure(call, &core.ToolError{
				Code:    errOutsideWorkspace,
				Message: fmt.Sprintf("%q is outside the workspace", args.Path),
			}, started), nil
		}
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: err.Error(),
		}, started), nil
	}
	if filepath.Clean(abs) == filepath.Clean(t.root) {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "refusing to delete the workspace root",
		}, started), nil
	}

	info, err := os.Lstat(abs)
	if err != nil {
		return newFailure(call, classify(err, args.Path), started), nil
	}
	if info.IsDir() && !args.Recursive {
		entries, readErr := os.ReadDir(abs)
		if readErr != nil {
			return newFailure(call, classify(readErr, args.Path), started), nil
		}
		if len(entries) > 0 {
			return newFailure(call, &core.ToolError{
				Code:    errInvalidArguments,
				Message: fmt.Sprintf("%q is a non-empty directory", args.Path),
				Hint:    "pass recursive=true to delete it with its contents",
			}, started), nil
		}
	}

	if args.Permanent {
		if err := os.RemoveAll(abs); err != nil {
			return newFailure(call, classify(err, args.Path), started), nil
		}
		return newResult(call, deleteData{
			Path:      relativeTo(t.root, abs),
			Deleted:   true,
			Permanent: true,
		}, false, started), nil
	}

	destination, err := t.trashPath(abs)
	if err != nil {
		return newFailure(call, classify(err, args.Path), started), nil
	}
	if err := fsx.EnsureParent(destination); err != nil {
		return newFailure(call, classify(err, args.Path), started), nil
	}
	if err := os.Rename(abs, destination); err != nil {
		return newFailure(call, &core.ToolError{
			Code:    errIO,
			Message: fmt.Sprintf("move %q to the trash: %v", args.Path, err),
			Hint:    "pass permanent=true to delete in place",
		}, started), nil
	}

	return newResult(call, deleteData{
		Path:      relativeTo(t.root, abs),
		TrashedTo: relativeTo(t.root, destination),
	}, false, started), nil
}

// trashPath returns a unique path under .liquidharness/trash/<timestamp>/.
func (t *fsDelete) trashPath(abs string) (string, error) {
	rel, err := filepath.Rel(t.root, abs)
	if err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405.000Z")
	base := filepath.Join(t.root, ".liquidharness", "trash", stamp, rel)
	candidate := base
	for i := 1; ; i++ {
		if _, statErr := os.Lstat(candidate); os.IsNotExist(statErr) {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

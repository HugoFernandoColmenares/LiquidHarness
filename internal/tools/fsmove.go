// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/fsx"
)

// fsMove renames or moves an entry inside the workspace. Overwriting an existing target is R2.
type fsMove struct {
	root string
	spec core.ToolSpec
}

func newFSMove(root string) *fsMove {
	return &fsMove{
		root: root,
		spec: core.ToolSpec{
			Name: "fs_move",
			Description: "Move or rename a file or directory inside the workspace. Fails when " +
				"the target exists unless overwrite is true, which is a destructive R2 action.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "from": {"type": "string", "description": "Current path relative to the workspace root"},
    "to": {"type": "string", "description": "New path relative to the workspace root"},
    "overwrite": {"type": "boolean", "description": "Replace an existing target (R2); defaults to false"}
  },
  "required": ["from", "to"],
  "additionalProperties": false
}`),
			Risk: core.RiskR1,
		},
	}
}

func (t *fsMove) Spec() core.ToolSpec { return t.spec }

type moveArgs struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Overwrite bool   `json:"overwrite"`
}

type moveData struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Bytes int64  `json:"bytes"`
}

// RiskFor escalates overwrite to R2.
func (t *fsMove) RiskFor(call core.ToolCall) core.RiskClass {
	var args moveArgs
	if err := json.Unmarshal(call.Arguments, &args); err == nil && args.Overwrite {
		return core.RiskR2
	}
	return core.RiskR1
}

func (t *fsMove) Execute(_ context.Context, call core.ToolCall) (core.ToolResult, error) {
	started := time.Now()

	var args moveArgs
	if toolErr := decodeArgs(call, &args); toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	if strings.TrimSpace(args.From) == "" || strings.TrimSpace(args.To) == "" {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "fs_move requires from and to",
		}, started), nil
	}

	source, err := fsx.ResolveWithin(t.root, args.From)
	if err != nil {
		if isOutsideError(err) {
			return newFailure(call, &core.ToolError{
				Code:    errOutsideWorkspace,
				Message: fmt.Sprintf("%q is outside the workspace", args.From),
			}, started), nil
		}
		return newFailure(call, &core.ToolError{Code: errInvalidArguments, Message: err.Error()}, started), nil
	}
	info, err := os.Lstat(source)
	if err != nil {
		return newFailure(call, classify(err, args.From), started), nil
	}
	if filepath.Clean(source) == filepath.Clean(t.root) {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "refusing to move the workspace root",
		}, started), nil
	}

	target, toolErr := resolvePathForWrite(t.root, args.To)
	if toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	if filepath.Clean(target) == filepath.Clean(source) {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "from and to are the same path",
		}, started), nil
	}

	if _, statErr := os.Lstat(target); statErr == nil {
		if !args.Overwrite {
			return newFailure(call, &core.ToolError{
				Code:    errConflict,
				Message: fmt.Sprintf("%q already exists", args.To),
				Hint:    "pass overwrite=true if replacing it is intended",
			}, started), nil
		}
		if err := os.RemoveAll(target); err != nil {
			return newFailure(call, classify(err, args.To), started), nil
		}
	} else if !os.IsNotExist(statErr) {
		return newFailure(call, classify(statErr, args.To), started), nil
	}

	if err := fsx.EnsureParent(target); err != nil {
		return newFailure(call, classify(err, args.To), started), nil
	}
	if err := os.Rename(source, target); err != nil {
		// A rename across volumes fails; copy the file and remove the source. Directories
		// stay in the workspace, so this fallback only needs to handle files.
		if info.IsDir() {
			return newFailure(call, &core.ToolError{
				Code:    errIO,
				Message: fmt.Sprintf("move %q to %q: %v", args.From, args.To, err),
			}, started), nil
		}
		if copyErr := copyFile(source, target, info.Mode().Perm()); copyErr != nil {
			return newFailure(call, &core.ToolError{
				Code:    errIO,
				Message: fmt.Sprintf("move %q to %q: %v", args.From, args.To, copyErr),
			}, started), nil
		}
		if removeErr := os.Remove(source); removeErr != nil {
			return newFailure(call, classify(removeErr, args.From), started), nil
		}
	}

	size := info.Size()
	return newResult(call, moveData{
		From:  relativeTo(t.root, source),
		To:    relativeTo(t.root, target),
		Bytes: size,
	}, false, started), nil
}

// copyFile is the cross-volume fallback for fs_move.
func copyFile(source, target string, perm os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

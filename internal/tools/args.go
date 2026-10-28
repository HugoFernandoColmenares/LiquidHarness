// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/fsx"
)

// Tool error codes. They are part of the tool protocol and stable across releases.
const (
	errInvalidArguments = "INVALID_ARGUMENT"
	errNotFound         = "NOT_FOUND"
	errOutsideWorkspace = "OUTSIDE_WORKSPACE"
	errNotDirectory     = "NOT_DIRECTORY"
	errIsDirectory      = "IS_DIRECTORY"
	errFileTooLarge     = "FILE_TOO_LARGE"
	errBinaryFile       = "BINARY_FILE"
	errInvalidPattern   = "INVALID_PATTERN"
	errIO               = "IO_ERROR"
	errConflict         = "CONFLICT"
	errTooLarge         = "TOO_LARGE"
	errPolicyDenied     = "POLICY_DENIED"
)

// isOutsideError reports whether err came from the workspace confinement.
func isOutsideError(err error) bool {
	return errors.Is(err, fsx.ErrOutsideRoot)
}

// decodeArgs unmarshals call.Arguments into dst. Empty arguments decode as an empty object.
func decodeArgs(call core.ToolCall, dst any) *core.ToolError {
	raw := bytes.TrimSpace(call.Arguments)
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return &core.ToolError{
			Code:    errInvalidArguments,
			Message: fmt.Sprintf("%s received invalid arguments: %v", call.Name, err),
			Hint:    "send a JSON object that matches the tool input schema",
		}
	}
	return nil
}

// resolvePath maps a model-supplied path to an absolute path inside root.
func resolvePath(root, rel string) (string, *core.ToolError) {
	abs, err := fsx.ResolveWithin(root, rel)
	if err == nil {
		return abs, nil
	}
	if errors.Is(err, fsx.ErrOutsideRoot) {
		return "", &core.ToolError{
			Code:    errOutsideWorkspace,
			Message: fmt.Sprintf("%q is outside the workspace", rel),
			Hint:    "use a path relative to the workspace root",
		}
	}
	return "", &core.ToolError{
		Code:    errInvalidArguments,
		Message: err.Error(),
		Hint:    "use a plain relative path",
	}
}

// classify maps filesystem errors to tool errors.
func classify(err error, rel string) *core.ToolError {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &core.ToolError{Code: errNotFound, Message: fmt.Sprintf("%q does not exist", rel)}
	case errors.Is(err, fs.ErrPermission):
		return &core.ToolError{Code: errIO, Message: fmt.Sprintf("permission denied: %q", rel)}
	default:
		return &core.ToolError{Code: errIO, Message: err.Error()}
	}
}

// relativeTo returns a slash-separated path relative to root for display in results.
func relativeTo(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

// newResult marshals data and attaches the duration and size metadata.
func newResult(call core.ToolCall, data any, truncated bool, started time.Time) core.ToolResult {
	encoded, err := json.Marshal(data)
	if err != nil {
		return core.ToolResult{
			CallID: call.ID,
			Error: &core.ToolError{
				Code:    errIO,
				Message: fmt.Sprintf("encode result: %v", err),
			},
			Meta: core.ToolMeta{DurationMS: time.Since(started).Milliseconds()},
		}
	}
	return core.ToolResult{
		CallID: call.ID,
		OK:     true,
		Data:   encoded,
		Meta: core.ToolMeta{
			DurationMS: time.Since(started).Milliseconds(),
			Bytes:      len(encoded),
			Truncated:  truncated,
		},
	}
}

// newFailure builds a failed result.
func newFailure(call core.ToolCall, toolErr *core.ToolError, started time.Time) core.ToolResult {
	return core.ToolResult{
		CallID: call.ID,
		Error:  toolErr,
		Meta:   core.ToolMeta{DurationMS: time.Since(started).Milliseconds()},
	}
}

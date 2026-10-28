// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

const (
	defaultListDepth = 1
	maxListDepth     = 3
	defaultListLimit = 200
	maxListLimit     = 1000
)

// fsList walks a directory inside the workspace and returns a bounded, sorted tree.
type fsList struct {
	root string
	spec core.ToolSpec
}

func newFSList(root string) *fsList {
	return &fsList{
		root: root,
		spec: core.ToolSpec{
			Name: "fs_list",
			Description: "List files and directories inside the workspace. Results are sorted " +
				"by path and bounded; the .git directory is skipped.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Directory relative to the workspace root; defaults to ."},
    "depth": {"type": "integer", "minimum": 1, "maximum": 3, "description": "Recursion depth; defaults to 1"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 1000, "description": "Maximum entries to return"}
  },
  "additionalProperties": false
}`),
			Risk: core.RiskR0,
		},
	}
}

func (t *fsList) Spec() core.ToolSpec { return t.spec }

type listArgs struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
	Limit int    `json:"limit"`
}

type listEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size,omitempty"`
}

type listData struct {
	Path      string      `json:"path"`
	Entries   []listEntry `json:"entries"`
	Truncated bool        `json:"truncated"`
}

func (t *fsList) Execute(ctx context.Context, call core.ToolCall) (core.ToolResult, error) {
	started := time.Now()

	var args listArgs
	if toolErr := decodeArgs(call, &args); toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	if args.Depth <= 0 {
		args.Depth = defaultListDepth
	}
	if args.Depth > maxListDepth {
		args.Depth = maxListDepth
	}
	if args.Limit <= 0 {
		args.Limit = defaultListLimit
	}
	if args.Limit > maxListLimit {
		args.Limit = maxListLimit
	}

	abs, toolErr := resolvePath(t.root, args.Path)
	if toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return newFailure(call, classify(err, args.Path), started), nil
	}
	if !info.IsDir() {
		return newFailure(call, &core.ToolError{
			Code:    errNotDirectory,
			Message: fmt.Sprintf("%q is not a directory", args.Path),
			Hint:    "use fs_read to read a file",
		}, started), nil
	}

	entries := make([]listEntry, 0, min(args.Limit, 64))
	truncated := false

	var walk func(dir string, depth int) bool
	walk = func(dir string, depth int) bool {
		if err := ctx.Err(); err != nil {
			return false
		}
		children, readErr := os.ReadDir(dir)
		if readErr != nil {
			return true
		}
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			if child.Name() == ".git" {
				continue
			}
			childPath := filepath.Join(dir, child.Name())
			entry := listEntry{
				Path: relativeTo(t.root, childPath),
				Name: child.Name(),
				Type: "file",
			}
			if child.IsDir() {
				entry.Type = "dir"
			} else if childInfo, statErr := child.Info(); statErr == nil {
				entry.Size = childInfo.Size()
			}
			if len(entries) >= args.Limit {
				truncated = true
				return false
			}
			entries = append(entries, entry)
			if child.IsDir() && depth < args.Depth {
				if !walk(childPath, depth+1) {
					return false
				}
			}
		}
		return true
	}
	walk(abs, 1)

	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, core.Wrap(core.CodeCanceled, err, "fs_list canceled")
	}

	return newResult(call, listData{
		Path:      relativeTo(t.root, abs),
		Entries:   entries,
		Truncated: truncated,
	}, truncated, started), nil
}

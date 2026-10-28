// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

const (
	defaultSearchResults = 50
	maxSearchResults     = 200
	maxSearchFileBytes   = 2 << 20
	maxPatternLength     = 512
	maxMatchLineRunes    = 400
)

// fsSearch greps the workspace for a regular expression.
type fsSearch struct {
	root string
	spec core.ToolSpec
}

func newFSSearch(root string) *fsSearch {
	return &fsSearch{
		root: root,
		spec: core.ToolSpec{
			Name: "fs_search",
			Description: "Search text files in the workspace with a Go regular expression. " +
				"Returns file, line number and matched line. Skips .git and binary files.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Go regular expression, at most 512 characters"},
    "path": {"type": "string", "description": "File or directory relative to the workspace root; defaults to ."},
    "glob": {"type": "string", "description": "Optional file name filter, for example *.go"},
    "max_results": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Maximum matches; defaults to 50"},
    "case_sensitive": {"type": "boolean", "description": "Match case-sensitively; defaults to false"}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`),
			Risk: core.RiskR0,
		},
	}
}

func (t *fsSearch) Spec() core.ToolSpec { return t.spec }

type searchArgs struct {
	Pattern       string `json:"pattern"`
	Path          string `json:"path"`
	Glob          string `json:"glob"`
	MaxResults    int    `json:"max_results"`
	CaseSensitive bool   `json:"case_sensitive"`
}

type searchMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type searchData struct {
	Pattern      string        `json:"pattern"`
	Path         string        `json:"path"`
	Matches      []searchMatch `json:"matches"`
	FilesScanned int           `json:"files_scanned"`
	Truncated    bool          `json:"truncated"`
}

func (t *fsSearch) Execute(ctx context.Context, call core.ToolCall) (core.ToolResult, error) {
	started := time.Now()

	var args searchArgs
	if toolErr := decodeArgs(call, &args); toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "fs_search requires a pattern",
			Hint:    "pass a Go regular expression",
		}, started), nil
	}
	if len(args.Pattern) > maxPatternLength {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: fmt.Sprintf("pattern is longer than %d characters", maxPatternLength),
		}, started), nil
	}
	if args.MaxResults <= 0 {
		args.MaxResults = defaultSearchResults
	}
	if args.MaxResults > maxSearchResults {
		args.MaxResults = maxSearchResults
	}

	expression := args.Pattern
	if !args.CaseSensitive {
		expression = "(?i)" + expression
	}
	re, err := regexp.Compile(expression)
	if err != nil {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidPattern,
			Message: fmt.Sprintf("invalid regular expression: %v", err),
			Hint:    "escape special characters or simplify the pattern",
		}, started), nil
	}
	if args.Glob != "" {
		if _, globErr := filepath.Match(args.Glob, "probe"); globErr != nil {
			return newFailure(call, &core.ToolError{
				Code:    errInvalidPattern,
				Message: fmt.Sprintf("invalid glob: %v", globErr),
				Hint:    "use a file name pattern such as *.go",
			}, started), nil
		}
	}

	abs, toolErr := resolvePath(t.root, args.Path)
	if toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return newFailure(call, classify(err, args.Path), started), nil
	}

	data := searchData{
		Pattern: args.Pattern,
		Path:    relativeTo(t.root, abs),
		Matches: make([]searchMatch, 0, min(args.MaxResults, 16)),
	}

	searchFile := func(path string) bool {
		if data.Truncated {
			return false
		}
		fileInfo, statErr := os.Stat(path)
		if statErr != nil || fileInfo.Size() > maxSearchFileBytes {
			return true
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil || isBinary(raw) {
			return true
		}
		data.FilesScanned++
		scanner := bufio.NewScanner(strings.NewReader(string(raw)))
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for lineNumber := 1; scanner.Scan(); lineNumber++ {
			line := scanner.Text()
			if !re.MatchString(line) {
				continue
			}
			if len(data.Matches) >= args.MaxResults {
				data.Truncated = true
				return false
			}
			data.Matches = append(data.Matches, searchMatch{
				Path: relativeTo(t.root, path),
				Line: lineNumber,
				Text: truncateRunes(strings.TrimSuffix(line, "\r"), maxMatchLineRunes),
			})
		}
		return true
	}

	if info.IsDir() {
		walkErr := filepath.WalkDir(abs, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				// An entry that cannot be read is skipped: a single permission error must
				// not abort the search.
				return nil //nolint:nilerr // skip unreadable entries and keep walking
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if entry.IsDir() {
				if entry.Name() == ".git" && path != abs {
					return filepath.SkipDir
				}
				return nil
			}
			if args.Glob != "" {
				if matched, _ := filepath.Match(args.Glob, entry.Name()); !matched {
					return nil
				}
			}
			if !searchFile(path) {
				return filepath.SkipAll
			}
			return nil
		})
		if walkErr != nil && ctx.Err() != nil {
			return core.ToolResult{}, core.Wrap(core.CodeCanceled, walkErr, "fs_search canceled")
		}
	} else {
		if args.Glob != "" {
			if matched, _ := filepath.Match(args.Glob, info.Name()); !matched {
				data.Matches = nil
				return newResult(call, data, false, started), nil
			}
		}
		searchFile(abs)
	}

	return newResult(call, data, data.Truncated, started), nil
}

// truncateRunes shortens s to at most limit runes, marking the cut.
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "..."
}

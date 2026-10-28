// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	// ErrOutsideRoot means the requested path resolves outside the workspace root.
	ErrOutsideRoot = errors.New("path escapes the workspace root")
	// ErrInvalidPath means the path is malformed: empty root, NUL byte, UNC share or a
	// Windows alternate data stream.
	ErrInvalidPath = errors.New("invalid path")
)

// ResolveWithin resolves rel inside root and returns a cleaned absolute path that is safe to
// read. It rejects relative escapes (".."), absolute paths outside root, symlinks that point
// outside root and, on Windows, UNC paths and alternate data streams.
//
// root must exist. rel may not exist: the parent chain is resolved as far as possible and the
// missing suffix is appended, so callers can report "not found" themselves. Write-side helpers
// land in F3 on top of this function.
func ResolveWithin(root, rel string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%w: empty workspace root", ErrInvalidPath)
	}
	if rel == "" {
		rel = "."
	}
	if strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("%w: NUL byte in %q", ErrInvalidPath, rel)
	}
	if err := rejectPlatformPath(rel); err != nil {
		return "", err
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: resolve root: %w", ErrInvalidPath, err)
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("%w: resolve root symlinks: %w", ErrInvalidPath, err)
	}

	candidate := rel
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(rootResolved, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("%w: resolve %q: %w", ErrInvalidPath, rel, err)
	}
	if !within(rootResolved, candidate) {
		return "", fmt.Errorf("%w: %q", ErrOutsideRoot, rel)
	}

	resolved, err := evalExisting(candidate)
	if err != nil {
		return "", fmt.Errorf("%w: resolve symlinks of %q: %w", ErrInvalidPath, rel, err)
	}
	if !within(rootResolved, resolved) {
		return "", fmt.Errorf("%w: %q points outside through a symlink", ErrOutsideRoot, rel)
	}
	return resolved, nil
}

// rejectPlatformPath blocks Windows-only path shapes that do not mean what they look like.
func rejectPlatformPath(rel string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	if strings.HasPrefix(rel, `\\`) || strings.HasPrefix(rel, `//`) {
		return fmt.Errorf("%w: UNC path %q", ErrInvalidPath, rel)
	}
	// A colon is only legal as the drive separator of an absolute path ("C:\dir").
	if i := strings.IndexByte(rel, ':'); i >= 0 {
		absolute := i == 1 && isDriveLetter(rel[0]) && len(rel) > 2 &&
			(rel[2] == '\\' || rel[2] == '/')
		if !absolute {
			return fmt.Errorf("%w: alternate data stream or drive-relative path %q", ErrInvalidPath, rel)
		}
	}
	return nil
}

func isDriveLetter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// evalExisting resolves symlinks in the longest existing prefix of path and appends the
// missing suffix untouched. That way a caller can resolve a path that is about to be created.
func evalExisting(path string) (string, error) {
	current := path
	suffix := ""
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if suffix == "" {
				return resolved, nil
			}
			return filepath.Join(resolved, suffix), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path, nil
		}
		suffix = filepath.Join(filepath.Base(current), suffix)
		current = parent
	}
}

// within reports whether target is root or lives below it. Windows compares case-insensitively
// because NTFS does.
func within(root, target string) bool {
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
		target = strings.ToLower(target)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

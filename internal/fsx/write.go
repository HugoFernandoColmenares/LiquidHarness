// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package fsx

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ResolveForWrite resolves rel inside root and returns the absolute path of a file that may be
// created or replaced. It rejects the workspace root itself, paths that resolve to a directory,
// and every escape that ResolveWithin rejects.
//
// The check and the later write are not atomic: a symlink swapped between them is a TOCTOU
// window that F3 accepts on Windows and that the sandbox in F7 will close with handles.
func ResolveForWrite(root, rel string) (string, error) {
	if rel == "" || filepath.Clean(rel) == "." {
		return "", fmt.Errorf("%w: refusing to write the workspace root", ErrInvalidPath)
	}
	abs, err := ResolveWithin(root, rel)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: resolve root: %w", ErrInvalidPath, err)
	}
	if samePath(abs, rootAbs) {
		return "", fmt.Errorf("%w: refusing to write the workspace root", ErrInvalidPath)
	}
	if info, statErr := os.Lstat(abs); statErr == nil && info.IsDir() {
		return "", fmt.Errorf("%w: %q is a directory", ErrInvalidPath, rel)
	}
	return abs, nil
}

// EnsureParent creates the parent directory of path inside the workspace. It returns an error
// when the parent is not a directory.
func EnsureParent(path string) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("stat parent directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", parent)
	}
	return nil
}

// AtomicWrite writes data to path through a temporary file in the same directory and renames it
// into place, so a crash never leaves a half-written file.
func AtomicWrite(path string, data []byte, perm fs.FileMode) error {
	if err := EnsureParent(path); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tempName := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}

	if _, err := temp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Chmod(tempName, perm); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("chmod temporary file: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}

// samePath compares two absolute paths using the platform's case rules.
func samePath(a, b string) bool {
	cleanA, errA := filepath.Abs(a)
	cleanB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	if os.PathSeparator == '\\' {
		return strings.EqualFold(cleanA, cleanB)
	}
	return cleanA == cleanB
}

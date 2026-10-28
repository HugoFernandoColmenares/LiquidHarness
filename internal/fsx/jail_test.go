// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package fsx_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/fsx"
)

// adversarialPaths are the escape vectors of the F3 jail suite. The test asserts the invariant,
// not a specific error: a vector may be rejected or resolve to a path inside the root, but it
// must never resolve outside.
func adversarialPaths(t *testing.T) []string {
	t.Helper()
	vectors := []string{
		"..",
		"../",
		"../..",
		"../../etc/passwd",
		"./..",
		"docs/../../etc/passwd",
		"docs/./../../etc/passwd",
		"docs/../..",
		"....//....//etc/passwd",
		"..;/etc/passwd",
		"..%2f..%2fetc/passwd",
		"..%2F..%2Fetc%2Fpasswd",
		"..\\..\\etc\\passwd",
		`..\..\windows\win.ini`,
		"docs/..\\..\\windows\\win.ini",
		"~/.ssh/id_rsa",
		"%USERPROFILE%\\.ssh\\id_rsa",
		"$HOME/.ssh/id_rsa",
		"/etc/passwd",
		"/",
		"/tmp/../../etc/passwd",
		"file.txt:stream",
		"file.txt::$DATA",
		"file.txt:stream:$DATA",
		`\\server\share\file`,
		`\\?\C:\Windows\win.ini`,
		`\\?\UNC\server\share\file`,
		`\\.\PhysicalDrive0`,
		`\\.\C:\Windows\win.ini`,
		`C:\Windows\win.ini`,
		"C:/Windows/win.ini",
		"C:relative",
		"C:",
		`\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1\Windows\win.ini`,
		"docs/\x00note.txt",
		".. ",
		".. .",
		"docs/.. /.. /etc/passwd",
		"CON",
		"NUL.txt",
		"aux",
		"docs/COM1",
		"trailing.",
		"trailing ",
		"．．/etc/passwd",
		"..%c0%af..%c0%afetc/passwd",
		"%2e%2e/%2e%2e/etc/passwd",
	}
	if runtime.GOOS == "windows" {
		vectors = append(vectors, outsidePath(t))
	}
	return vectors
}

func TestJailAdversarialVectors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, rel := range adversarialPaths(t) {
		t.Run(sanitizeName(rel), func(t *testing.T) {
			got, err := fsx.ResolveWithin(root, rel)
			if err != nil {
				if errors.Is(err, fsx.ErrOutsideRoot) || errors.Is(err, fsx.ErrInvalidPath) {
					return
				}
				t.Fatalf("ResolveWithin(%q): unexpected error %v", rel, err)
			}
			if !isWithin(root, got) {
				t.Fatalf("ResolveWithin(%q) escaped to %q", rel, got)
			}
		})
	}
}

func TestJailAdversarialVectorsForWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, rel := range adversarialPaths(t) {
		t.Run(sanitizeName(rel), func(t *testing.T) {
			got, err := fsx.ResolveForWrite(root, rel)
			if err != nil {
				if errors.Is(err, fsx.ErrOutsideRoot) || errors.Is(err, fsx.ErrInvalidPath) {
					return
				}
				t.Fatalf("ResolveForWrite(%q): unexpected error %v", rel, err)
			}
			if !isWithin(root, got) {
				t.Fatalf("ResolveForWrite(%q) escaped to %q", rel, got)
			}
			if samePath(got, root) {
				t.Fatalf("ResolveForWrite(%q) returned the workspace root", rel)
			}
		})
	}

	if _, err := fsx.ResolveForWrite(root, "."); !errors.Is(err, fsx.ErrInvalidPath) {
		t.Errorf("ResolveForWrite(\".\") error = %v, want ErrInvalidPath", err)
	}
	if _, err := fsx.ResolveForWrite(root, ""); !errors.Is(err, fsx.ErrInvalidPath) {
		t.Errorf("ResolveForWrite(\"\") error = %v, want ErrInvalidPath", err)
	}
	if _, err := fsx.ResolveForWrite(root, "dir"); !errors.Is(err, fsx.ErrInvalidPath) {
		t.Errorf("ResolveForWrite(\"dir\") error = %v, want ErrInvalidPath", err)
	}
}

func TestJailRejectsSymlinkFileEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "secret-link.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if _, err := fsx.ResolveWithin(root, "secret-link.txt"); !errors.Is(err, fsx.ErrOutsideRoot) {
		t.Fatalf("ResolveWithin error = %v, want ErrOutsideRoot", err)
	}
	if _, err := fsx.ResolveForWrite(root, "secret-link.txt"); !errors.Is(err, fsx.ErrOutsideRoot) {
		t.Fatalf("ResolveForWrite error = %v, want ErrOutsideRoot", err)
	}
}

func TestJailRejectsSymlinkParentEscapeForWrite(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if _, err := fsx.ResolveForWrite(root, "escape/new.txt"); !errors.Is(err, fsx.ErrOutsideRoot) {
		t.Fatalf("ResolveForWrite error = %v, want ErrOutsideRoot", err)
	}
}

// TestJailHardlinkStaysInside documents the F3 boundary: a hardlink created inside the
// workspace resolves to an inside path, but its content can be shared with a file outside. The
// path jail cannot detect that without file-identity checks, which the F7 sandbox will add.
func TestJailHardlinkStaysInside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "hardlink.txt")
	if err := os.Link(secret, link); err != nil {
		t.Skipf("hardlinks are unavailable: %v", err)
	}
	got, err := fsx.ResolveWithin(root, "hardlink.txt")
	if err != nil {
		t.Fatalf("ResolveWithin: %v", err)
	}
	if !isWithin(root, got) {
		t.Fatalf("hardlink resolved outside: %q", got)
	}
}

func isWithin(root, target string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		rootResolved = rootAbs
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		rootResolved = strings.ToLower(rootResolved)
		targetAbs = strings.ToLower(targetAbs)
	}
	rel, err := filepath.Rel(rootResolved, targetAbs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func sanitizeName(rel string) string {
	name := strings.NewReplacer("/", "_", `\`, "_", ":", "_", "*", "_", "?", "_", `"`, "_",
		"<", "_", ">", "_", "|", "_", "\x00", "_", " ", "_").Replace(rel)
	if name == "" {
		name = "empty"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}

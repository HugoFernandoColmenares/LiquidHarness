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

func TestResolveWithin(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "nested"), 0o750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "docs", "nested", "note.txt")
	if err := os.WriteFile(file, []byte("hola"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		rel     string
		want    string
		wantErr error
	}{
		{name: "dot", rel: ".", want: root},
		{name: "relative file", rel: "docs/nested/note.txt", want: file},
		{name: "cleaned", rel: "./docs/../docs/nested/note.txt", want: file},
		{name: "missing file", rel: "docs/missing.txt", want: filepath.Join(root, "docs", "missing.txt")},
		{name: "absolute inside", rel: file, want: file},
		{name: "parent escape", rel: "..", wantErr: fsx.ErrOutsideRoot},
		{name: "nested escape", rel: "docs/../../etc/passwd", wantErr: fsx.ErrOutsideRoot},
		{name: "absolute outside", rel: outsidePath(t), wantErr: fsx.ErrOutsideRoot},
		{name: "nul byte", rel: "docs/\x00note.txt", wantErr: fsx.ErrInvalidPath},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := fsx.ResolveWithin(root, test.rel)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("ResolveWithin(%q) error = %v, want %v", test.rel, err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveWithin(%q): %v", test.rel, err)
			}
			// ResolveWithin returns symlink-resolved paths; t.TempDir may sit under a symlink
			// (macOS /var -> /private/var, Windows 8.3 short names), so compare against the
			// resolved spelling of the expected path.
			want := test.want
			if rel, relErr := filepath.Rel(root, want); relErr == nil {
				if resolvedRoot, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
					want = filepath.Join(resolvedRoot, rel)
				}
			}
			if !samePath(got, want) {
				t.Errorf("ResolveWithin(%q) = %q, want %q", test.rel, got, want)
			}
		})
	}
}

func TestResolveWithinRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	if _, err := fsx.ResolveWithin(root, "escape/secret.txt"); !errors.Is(err, fsx.ErrOutsideRoot) {
		t.Fatalf("error = %v, want ErrOutsideRoot", err)
	}
}

func TestResolveWithinRejectsWindowsPathShapes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only path shapes")
	}
	root := t.TempDir()

	for _, rel := range []string{`\\server\share\file`, `file.txt:stream`, `C:relative`} {
		if _, err := fsx.ResolveWithin(root, rel); !errors.Is(err, fsx.ErrInvalidPath) {
			t.Errorf("ResolveWithin(%q) error = %v, want ErrInvalidPath", rel, err)
		}
	}
}

func TestResolveWithinRequiresExistingRoot(t *testing.T) {
	if _, err := fsx.ResolveWithin(filepath.Join(t.TempDir(), "missing"), "."); !errors.Is(err, fsx.ErrInvalidPath) {
		t.Fatalf("error = %v, want ErrInvalidPath", err)
	}
	if _, err := fsx.ResolveWithin("", "."); !errors.Is(err, fsx.ErrInvalidPath) {
		t.Fatalf("empty root error = %v, want ErrInvalidPath", err)
	}
}

// outsidePath returns an absolute path that is guaranteed not to be inside any t.TempDir root.
func outsidePath(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(os.Getenv("SystemRoot"))
		if volume == "" {
			volume = `C:`
		}
		return filepath.Join(volume+string(filepath.Separator), "Windows", "win.ini")
	}
	return "/etc/passwd"
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := `# comment
PLAIN=value
QUOTED="hello world"
SINGLE='literal # not a comment'
EXPORTED=exported
INLINE=value # trailing comment
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	values, err := loadDotEnv(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := map[string]string{
		"PLAIN":    "value",
		"QUOTED":   "hello world",
		"SINGLE":   "literal # not a comment",
		"EXPORTED": "exported",
		"INLINE":   "value",
	}
	for key, expected := range want {
		if values[key] != expected {
			t.Errorf("%s = %q, want %q", key, values[key], expected)
		}
	}
}

func TestLoadDotEnvRejectsMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("NOT_A_PAIR\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadDotEnv(path)
	if err == nil || !core.Is(err, core.CodeConfigInvalid) {
		t.Fatalf("error = %v, want CONFIG_INVALID", err)
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	_, err := loadDotEnv(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil || !core.Is(err, core.CodeConfigInvalid) {
		t.Fatalf("error = %v, want CONFIG_INVALID", err)
	}
}

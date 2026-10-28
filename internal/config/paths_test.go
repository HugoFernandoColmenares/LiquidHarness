// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"path/filepath"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/config"
)

func TestResolvePathsLinux(t *testing.T) {
	base := t.TempDir()
	configBase := filepath.Join(base, "config")
	cacheBase := filepath.Join(base, "cache")
	home := filepath.Join(base, "home")
	dataBase := filepath.Join(base, "xdg-data")

	paths, err := config.ResolvePaths(config.PathOptions{
		GOOS: "linux",
		Getenv: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataBase
			}
			return ""
		},
		UserConfigDir: func() (string, error) { return configBase, nil },
		UserCacheDir:  func() (string, error) { return cacheBase, nil },
		HomeDir:       func() (string, error) { return home, nil },
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if want := filepath.Join(configBase, "liquidharness"); paths.ConfigDir != want {
		t.Errorf("config dir = %q, want %q", paths.ConfigDir, want)
	}
	if want := filepath.Join(dataBase, "liquidharness"); paths.DataDir != want {
		t.Errorf("data dir = %q, want %q", paths.DataDir, want)
	}
	if want := filepath.Join(configBase, "liquidharness", "config.toml"); paths.UserConfig != want {
		t.Errorf("user config = %q, want %q", paths.UserConfig, want)
	}
	if want := filepath.Join(dataBase, "liquidharness", "logs"); paths.LogsDir != want {
		t.Errorf("logs dir = %q, want %q", paths.LogsDir, want)
	}
	if want := filepath.Join(cacheBase, "liquidharness", "models"); paths.ModelsDir != want {
		t.Errorf("models dir = %q, want %q", paths.ModelsDir, want)
	}
}

func TestResolvePathsWindows(t *testing.T) {
	base := t.TempDir()
	local := filepath.Join(base, "local")
	configBase := filepath.Join(base, "roaming")

	paths, err := config.ResolvePaths(config.PathOptions{
		GOOS: "windows",
		Getenv: func(key string) string {
			if key == "LOCALAPPDATA" {
				return local
			}
			return ""
		},
		UserConfigDir: func() (string, error) { return configBase, nil },
		UserCacheDir:  func() (string, error) { return local, nil },
		HomeDir:       func() (string, error) { return base, nil },
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if want := filepath.Join(configBase, "LiquidHarness"); paths.ConfigDir != want {
		t.Errorf("config dir = %q, want %q", paths.ConfigDir, want)
	}
	if want := filepath.Join(local, "LiquidHarness", "data"); paths.DataDir != want {
		t.Errorf("data dir = %q, want %q", paths.DataDir, want)
	}
}

func TestResolvePathsDarwin(t *testing.T) {
	base := t.TempDir()
	configBase := filepath.Join(base, "application-support")
	cacheBase := filepath.Join(base, "caches")

	paths, err := config.ResolvePaths(config.PathOptions{
		GOOS:          "darwin",
		Getenv:        func(string) string { return "" },
		UserConfigDir: func() (string, error) { return configBase, nil },
		UserCacheDir:  func() (string, error) { return cacheBase, nil },
		HomeDir:       func() (string, error) { return base, nil },
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if want := filepath.Join(configBase, "LiquidHarness", "data"); paths.DataDir != want {
		t.Errorf("data dir = %q, want %q", paths.DataDir, want)
	}
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/liquidharness/liquidharness/internal/core"
)

// Paths are the OS-specific locations the harness reads and writes.
type Paths struct {
	ConfigDir     string `json:"config_dir"`
	DataDir       string `json:"data_dir"`
	CacheDir      string `json:"cache_dir"`
	ModelsDir     string `json:"models_dir"`
	LogsDir       string `json:"logs_dir"`
	UserConfig    string `json:"user_config"`
	ProjectConfig string `json:"project_config"`
	DBPath        string `json:"db_path"`
}

// PathOptions allows tests to inject the OS and the user directories.
type PathOptions struct {
	GOOS          string
	Getenv        func(string) string
	UserConfigDir func() (string, error)
	UserCacheDir  func() (string, error)
	HomeDir       func() (string, error)
}

func (o *PathOptions) withDefaults() {
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	if o.UserConfigDir == nil {
		o.UserConfigDir = os.UserConfigDir
	}
	if o.UserCacheDir == nil {
		o.UserCacheDir = os.UserCacheDir
	}
	if o.HomeDir == nil {
		o.HomeDir = os.UserHomeDir
	}
}

func appDirName(goos string) string {
	if goos == "windows" || goos == "darwin" {
		return "LiquidHarness"
	}
	return "liquidharness"
}

// ResolvePaths computes the default locations for the current user and OS.
func ResolvePaths(opts PathOptions) (Paths, error) {
	opts.withDefaults()

	configBase, err := opts.UserConfigDir()
	if err != nil {
		return Paths{}, core.Wrap(core.CodeInternal, err, "resolve user config directory")
	}
	cacheBase, err := opts.UserCacheDir()
	if err != nil {
		return Paths{}, core.Wrap(core.CodeInternal, err, "resolve user cache directory")
	}
	home, err := opts.HomeDir()
	if err != nil {
		return Paths{}, core.Wrap(core.CodeInternal, err, "resolve home directory")
	}

	appName := appDirName(opts.GOOS)

	dataBase := configBase
	switch opts.GOOS {
	case "windows":
		if local := opts.Getenv("LOCALAPPDATA"); local != "" {
			dataBase = local
		} else {
			dataBase = cacheBase
		}
	case "linux":
		if xdg := opts.Getenv("XDG_DATA_HOME"); xdg != "" {
			dataBase = xdg
		} else {
			dataBase = filepath.Join(home, ".local", "share")
		}
	}

	dataDir := filepath.Join(dataBase, appName)
	if opts.GOOS == "windows" || opts.GOOS == "darwin" {
		dataDir = filepath.Join(dataDir, "data")
	}

	paths := Paths{
		ConfigDir:  filepath.Join(configBase, appName),
		DataDir:    dataDir,
		CacheDir:   filepath.Join(cacheBase, appName),
		LogsDir:    filepath.Join(dataDir, "logs"),
		UserConfig: filepath.Join(configBase, appName, "config.toml"),
		DBPath:     filepath.Join(dataDir, "liquidharness.db"),
	}
	paths.ModelsDir = filepath.Join(paths.CacheDir, "models")
	return paths, nil
}

// RuntimePaths applies the data directory and database overrides from the configuration and
// records the effective values back into it. Relative overrides resolve against workDir.
func RuntimePaths(base Paths, cfg *Config, workDir string) (Paths, error) {
	dataDir := base.DataDir
	if cfg.Storage.DataDir != "" {
		resolved, err := resolveUserPath(cfg.Storage.DataDir, workDir)
		if err != nil {
			return Paths{}, err
		}
		dataDir = resolved
	}

	dbPath := filepath.Join(dataDir, "liquidharness.db")
	if cfg.Storage.DBPath != "" {
		resolved, err := resolveUserPath(cfg.Storage.DBPath, workDir)
		if err != nil {
			return Paths{}, err
		}
		dbPath = resolved
	}

	base.DataDir = dataDir
	base.LogsDir = filepath.Join(dataDir, "logs")
	base.DBPath = dbPath
	base.ProjectConfig = filepath.Join(cfg.General.Workspace, ".liquidharness", "config.toml")
	cfg.Storage.DataDir = dataDir
	cfg.Storage.DBPath = dbPath
	return base, nil
}

func resolveUserPath(value, workDir string) (string, error) {
	path := value
	switch {
	case path == "~":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", core.Wrap(core.CodeConfigInvalid, err, "expand %q", value)
		}
		path = home
	case strings.HasPrefix(path, "~/"), strings.HasPrefix(path, `~\`):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", core.Wrap(core.CodeConfigInvalid, err, "expand %q", value)
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	return filepath.Clean(path), nil
}

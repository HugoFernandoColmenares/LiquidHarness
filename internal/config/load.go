// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

// Flags are one-shot overrides coming from the command line. A nil pointer means "not set",
// which is different from an explicit empty value.
type Flags struct {
	ConfigFile *string
	EnvFile    *string
	Workspace  *string
	Profile    *string
	LogLevel   *string
	LogFormat  *string
	PolicyMode *string
	BaseURL    *string
	Model      *string
	DataDir    *string
	JSON       *bool
}

// Options controls loading. All fields are injectable so tests never touch the real user.
type Options struct {
	Flags     Flags
	LookupEnv func(string) (string, bool)
	WorkDir   string
}

// Load merges every configuration layer and validates the result. It also returns the paths
// with the effective data directory and database location.
func Load(base Paths, opts Options) (Config, Paths, error) {
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.LookupEnv
	}
	if opts.WorkDir == "" {
		workDir, err := os.Getwd()
		if err != nil {
			return Config{}, Paths{}, core.Wrap(core.CodeInternal, err, "resolve working directory")
		}
		opts.WorkDir = workDir
	}

	cfg := Defaults()

	envFile := ""
	if flag := opts.Flags.EnvFile; flag != nil {
		envFile = *flag
	} else if value, ok := opts.LookupEnv("LIQUIDHARNESS_ENV_FILE"); ok {
		envFile = value
	}
	envValues := map[string]string{}
	if envFile != "" {
		if !filepath.IsAbs(envFile) {
			envFile = filepath.Join(opts.WorkDir, envFile)
		}
		values, err := loadDotEnv(envFile)
		if err != nil {
			return Config{}, Paths{}, err
		}
		envValues = values
	}

	userConfig := base.UserConfig
	explicitUserConfig := false
	if flag := opts.Flags.ConfigFile; flag != nil && *flag != "" {
		userConfig = *flag
		explicitUserConfig = true
	}
	if err := mergeFile(&cfg, userConfig, explicitUserConfig); err != nil {
		return Config{}, Paths{}, err
	}

	// The workspace decides where the project config lives, so it must be resolved from flags,
	// environment, .env, user config and cwd before the project layer is read.
	workspace, err := resolveWorkspace(cfg, opts, envValues)
	if err != nil {
		return Config{}, Paths{}, err
	}
	cfg.General.Workspace = workspace

	projectConfig := filepath.Join(workspace, ".liquidharness", "config.toml")
	if err := mergeFile(&cfg, projectConfig, false); err != nil {
		return Config{}, Paths{}, err
	}
	base.ProjectConfig = projectConfig

	for key, value := range envValues {
		if err := applyEnvValue(&cfg, key, value); err != nil {
			return Config{}, Paths{}, err
		}
	}
	if err := applyEnv(&cfg, opts.LookupEnv); err != nil {
		return Config{}, Paths{}, err
	}
	if err := applyFlags(&cfg, opts.Flags); err != nil {
		return Config{}, Paths{}, err
	}

	// A workspace declared in any layer is resolved once; a raw "." from .env or TOML must not
	// leak into the validated configuration.
	cfg.General.Workspace = workspace

	paths, err := RuntimePaths(base, &cfg, opts.WorkDir)
	if err != nil {
		return Config{}, Paths{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, Paths{}, err
	}

	return cfg, paths, nil
}

// mergeFile decodes a TOML file over the current configuration. A missing optional file is not
// an error; a missing explicit file is. Unknown keys are rejected to catch typos early.
func mergeFile(cfg *Config, path string, required bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if required {
				return core.Wrap(core.CodeConfigInvalid, err, "config file not found: %s", path)
			}
			return nil
		}
		return core.Wrap(core.CodeConfigInvalid, err, "read config file %s", path)
	}

	// Windows editors often prepend a UTF-8 BOM; accept it instead of failing with a cryptic
	// TOML parse error.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	metadata, err := toml.Decode(string(data), cfg)
	if err != nil {
		return core.Wrap(core.CodeConfigInvalid, err, "parse config file %s", path)
	}

	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			keys = append(keys, key.String())
		}
		return core.E(core.CodeConfigInvalid, "unknown configuration key(s) in %s: %s",
			path, strings.Join(keys, ", "))
	}
	return nil
}

func resolveWorkspace(cfg Config, opts Options, envValues map[string]string) (string, error) {
	workspace := cfg.General.Workspace
	if value := envValues["LIQUIDHARNESS_WORKSPACE"]; value != "" {
		workspace = value
	}
	if value, ok := opts.LookupEnv("LIQUIDHARNESS_WORKSPACE"); ok && value != "" {
		workspace = value
	}
	if flag := opts.Flags.Workspace; flag != nil && *flag != "" {
		workspace = *flag
	}
	if workspace == "" {
		workspace = opts.WorkDir
	}

	expanded, err := resolveUserPath(workspace, opts.WorkDir)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", core.Wrap(core.CodeConfigInvalid, err, "resolve workspace %q", workspace)
	}
	return abs, nil
}

// Source describes one configuration layer that was found on disk.
type Source struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// Sources lists the files that contributed to the configuration.
func Sources(paths Paths, flags Flags, lookupEnv func(string) (string, bool)) []Source {
	var sources []Source
	userConfig := paths.UserConfig
	if flag := flags.ConfigFile; flag != nil && *flag != "" {
		userConfig = *flag
	}
	if _, err := os.Stat(userConfig); err == nil {
		sources = append(sources, Source{Kind: "user", Path: userConfig})
	}
	if _, err := os.Stat(paths.ProjectConfig); err == nil {
		sources = append(sources, Source{Kind: "project", Path: paths.ProjectConfig})
	}

	envFile := ""
	if flag := flags.EnvFile; flag != nil {
		envFile = *flag
	} else if lookupEnv != nil {
		if value, ok := lookupEnv("LIQUIDHARNESS_ENV_FILE"); ok {
			envFile = value
		}
	}
	if envFile != "" {
		sources = append(sources, Source{Kind: "env_file", Path: envFile})
	}
	return sources
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/config"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/policy"
)

func testPaths(t *testing.T) config.Paths {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	return config.Paths{
		ConfigDir:  filepath.Join(root, "config"),
		DataDir:    dataDir,
		CacheDir:   filepath.Join(root, "cache"),
		ModelsDir:  filepath.Join(root, "cache", "models"),
		LogsDir:    filepath.Join(dataDir, "logs"),
		UserConfig: filepath.Join(root, "config", "config.toml"),
		DBPath:     filepath.Join(dataDir, "liquidharness.db"),
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func load(t *testing.T, paths config.Paths, workDir string, env map[string]string, flags config.Flags) (config.Config, config.Paths, error) {
	t.Helper()
	return config.Load(paths, config.Options{
		Flags: flags,
		LookupEnv: func(key string) (string, bool) {
			value, ok := env[key]
			return value, ok
		},
		WorkDir: workDir,
	})
}

func TestLoadDefaults(t *testing.T) {
	workspace := t.TempDir()
	cfg, paths, err := load(t, testPaths(t), workspace, nil, config.Flags{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Model.BaseURL != "http://127.0.0.1:8080/v1" {
		t.Errorf("base_url = %q", cfg.Model.BaseURL)
	}
	if cfg.Policy.Mode != policy.ModeAsk {
		t.Errorf("policy mode = %q", cfg.Policy.Mode)
	}
	if cfg.General.Workspace != workspace {
		t.Errorf("workspace = %q, want %q", cfg.General.Workspace, workspace)
	}
	if !filepath.IsAbs(paths.DBPath) {
		t.Errorf("db path is not absolute: %q", paths.DBPath)
	}
	if cfg.Storage.DataDir != paths.DataDir {
		t.Errorf("storage.data_dir = %q, want %q", cfg.Storage.DataDir, paths.DataDir)
	}
}

func TestLoadPrecedence(t *testing.T) {
	workspace := t.TempDir()
	paths := testPaths(t)

	writeFile(t, paths.UserConfig, `
[model]
base_url = "http://127.0.0.1:9001/v1"
name = "user-model"
`)
	writeFile(t, filepath.Join(workspace, ".liquidharness", "config.toml"), `
[model]
base_url = "http://127.0.0.1:9002/v1"
name = "project-model"
`)

	// Project config overrides the user config.
	cfg, _, err := load(t, paths, workspace, nil, config.Flags{})
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	if cfg.Model.Name != "project-model" {
		t.Errorf("project layer: name = %q", cfg.Model.Name)
	}

	// Environment overrides both files.
	env := map[string]string{
		"LIQUIDHARNESS_MODEL_BASE_URL": "http://127.0.0.1:9003/v1",
		"LIQUIDHARNESS_MODEL_NAME":     "env-model",
	}
	cfg, _, err = load(t, paths, workspace, env, config.Flags{})
	if err != nil {
		t.Fatalf("load env: %v", err)
	}
	if cfg.Model.Name != "env-model" || cfg.Model.BaseURL != "http://127.0.0.1:9003/v1" {
		t.Errorf("env layer: name = %q, base_url = %q", cfg.Model.Name, cfg.Model.BaseURL)
	}

	// Flags override the environment.
	name := "flag-model"
	baseURL := "http://127.0.0.1:9004/v1"
	cfg, _, err = load(t, paths, workspace, env, config.Flags{Model: &name, BaseURL: &baseURL})
	if err != nil {
		t.Fatalf("load flags: %v", err)
	}
	if cfg.Model.Name != "flag-model" || cfg.Model.BaseURL != baseURL {
		t.Errorf("flags layer: name = %q, base_url = %q", cfg.Model.Name, cfg.Model.BaseURL)
	}
}

func TestLoadEnvFile(t *testing.T) {
	workspace := t.TempDir()
	paths := testPaths(t)
	envFile := filepath.Join(workspace, ".env")
	writeFile(t, envFile, `
# comment
export LIQUIDHARNESS_MODEL_NAME="dotenv-model"
LIQUIDHARNESS_MODEL_TEMPERATURE=0.4
`)

	cfg, _, err := load(t, paths, workspace, nil, config.Flags{EnvFile: &envFile})
	if err != nil {
		t.Fatalf("load env file: %v", err)
	}
	if cfg.Model.Name != "dotenv-model" {
		t.Errorf("name = %q, want dotenv-model", cfg.Model.Name)
	}
	if cfg.Model.Temperature != 0.4 {
		t.Errorf("temperature = %v, want 0.4", cfg.Model.Temperature)
	}

	// Process environment wins over .env.
	env := map[string]string{"LIQUIDHARNESS_MODEL_NAME": "process-model"}
	cfg, _, err = load(t, paths, workspace, env, config.Flags{EnvFile: &envFile})
	if err != nil {
		t.Fatalf("load with process env: %v", err)
	}
	if cfg.Model.Name != "process-model" {
		t.Errorf("name = %q, want process-model", cfg.Model.Name)
	}
}

func TestLoadEnvFileUnknownVariable(t *testing.T) {
	workspace := t.TempDir()
	paths := testPaths(t)
	envFile := filepath.Join(workspace, ".env")
	writeFile(t, envFile, "LIQUIDHARNESS_NOT_A_SETTING=1\n")

	_, _, err := load(t, paths, workspace, nil, config.Flags{EnvFile: &envFile})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !core.Is(err, core.CodeConfigInvalid) {
		t.Errorf("error = %v, want CONFIG_INVALID", err)
	}
}

func TestLoadEnvFileIgnoresForeignVariables(t *testing.T) {
	workspace := t.TempDir()
	paths := testPaths(t)
	envFile := filepath.Join(workspace, ".env")
	writeFile(t, envFile, "PATH=/usr/bin\nLIQUIDHARNESS_MODEL_NAME=dotenv-model\n")

	cfg, _, err := load(t, paths, workspace, nil, config.Flags{EnvFile: &envFile})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Model.Name != "dotenv-model" {
		t.Errorf("name = %q", cfg.Model.Name)
	}
}

func TestEnvExampleIsLoadable(t *testing.T) {
	example, err := filepath.Abs(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	cfg, _, err := load(t, testPaths(t), workspace, nil, config.Flags{EnvFile: &example})
	if err != nil {
		t.Fatalf("load .env.example: %v", err)
	}
	if cfg.Model.BaseURL != "http://127.0.0.1:8080/v1" {
		t.Errorf("base_url = %q", cfg.Model.BaseURL)
	}
	if cfg.General.LogFormat != "text" {
		t.Errorf("log_format = %q", cfg.General.LogFormat)
	}
}

func TestWorkspaceSelectsProjectConfig(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	paths := testPaths(t)

	writeFile(t, filepath.Join(first, ".liquidharness", "config.toml"), "[model]\nname = \"first-model\"\n")
	writeFile(t, filepath.Join(second, ".liquidharness", "config.toml"), "[model]\nname = \"second-model\"\n")

	// The flag wins over the working directory.
	cfg, _, err := load(t, paths, first, nil, config.Flags{Workspace: &second})
	if err != nil {
		t.Fatalf("load with workspace flag: %v", err)
	}
	if cfg.Model.Name != "second-model" {
		t.Errorf("name = %q, want second-model", cfg.Model.Name)
	}
	if cfg.General.Workspace != second {
		t.Errorf("workspace = %q, want %q", cfg.General.Workspace, second)
	}

	// The environment is the next layer.
	env := map[string]string{"LIQUIDHARNESS_WORKSPACE": first}
	cfg, _, err = load(t, paths, second, env, config.Flags{})
	if err != nil {
		t.Fatalf("load with workspace env: %v", err)
	}
	if cfg.Model.Name != "first-model" {
		t.Errorf("name = %q, want first-model", cfg.Model.Name)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	workspace := t.TempDir()
	paths := testPaths(t)
	writeFile(t, paths.UserConfig, "[model]\nbase_urll = \"http://127.0.0.1:8080/v1\"\n")

	_, _, err := load(t, paths, workspace, nil, config.Flags{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "unknown configuration key") {
		t.Errorf("error = %v", err)
	}
	if !core.Is(err, core.CodeConfigInvalid) {
		t.Errorf("error code = %v, want CONFIG_INVALID", err)
	}
}

func TestLoadExplicitConfigMustExist(t *testing.T) {
	workspace := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing.toml")
	_, _, err := load(t, testPaths(t), workspace, nil, config.Flags{ConfigFile: &missing})
	if err == nil || !core.Is(err, core.CodeConfigInvalid) {
		t.Fatalf("error = %v, want CONFIG_INVALID", err)
	}
}

func TestValidateRejectsNonLoopbackServerWithoutToken(t *testing.T) {
	cfg := config.Defaults()
	cfg.General.Workspace = t.TempDir()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Storage.DBPath = filepath.Join(cfg.Storage.DataDir, "state.db")
	cfg.Server.Addr = "0.0.0.0:8787"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "server.token") {
		t.Fatalf("error = %v, want server.token failure", err)
	}

	cfg.Server.Token = "secret"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate with token: %v", err)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	cfg := config.Defaults()
	cfg.General.Workspace = t.TempDir()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Storage.DBPath = filepath.Join(cfg.Storage.DataDir, "state.db")
	cfg.Policy.Mode = "yolo"
	cfg.Model.ContextWindow = 10
	cfg.Model.MaxOutputTokens = 5000

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, fragment := range []string{"policy.mode", "model.context_window", "model.max_output_tokens"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("error %q does not mention %s", err, fragment)
		}
	}
}

func TestRuntimePathsRelativeOverride(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Defaults()
	cfg.General.Workspace = workspace
	cfg.Storage.DataDir = "custom-data"

	paths, err := config.RuntimePaths(testPaths(t), &cfg, workspace)
	if err != nil {
		t.Fatalf("runtime paths: %v", err)
	}
	want := filepath.Join(workspace, "custom-data")
	if paths.DataDir != want {
		t.Errorf("data dir = %q, want %q", paths.DataDir, want)
	}
	if paths.DBPath != filepath.Join(want, "liquidharness.db") {
		t.Errorf("db path = %q", paths.DBPath)
	}
}

func TestWriteDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	created, err := config.WriteDefault(path, false)
	if err != nil || !created {
		t.Fatalf("write default: created=%v err=%v", created, err)
	}

	_, err = config.WriteDefault(path, false)
	if !core.Is(err, core.CodeConflict) {
		t.Fatalf("second write error = %v, want CONFLICT", err)
	}

	if _, err := config.WriteDefault(path, true); err != nil {
		t.Fatalf("force write: %v", err)
	}

	// The generated template must load and validate as-is.
	cfg, _, err := config.Load(testPaths(t), config.Options{
		Flags:     config.Flags{ConfigFile: &path},
		LookupEnv: func(string) (string, bool) { return "", false },
		WorkDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("load template: %v", err)
	}
	if cfg.ConfigVersion != config.CurrentVersion {
		t.Errorf("config version = %d", cfg.ConfigVersion)
	}
}

func TestLoadWrapsInvalidEnv(t *testing.T) {
	workspace := t.TempDir()
	env := map[string]string{"LIQUIDHARNESS_MODEL_TEMPERATURE": "hot"}
	_, _, err := load(t, testPaths(t), workspace, env, config.Flags{})
	if err == nil || !core.Is(err, core.CodeConfigInvalid) {
		t.Fatalf("error = %v, want CONFIG_INVALID", err)
	}
}

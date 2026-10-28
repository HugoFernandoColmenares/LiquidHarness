// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package app is the composition root. It wires configuration, logging, diagnostics and, in
// later phases, the agent loop, tools, storage and TUI. The CLI depends on this package; this
// package never depends on the CLI.
package app

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/config"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/telemetry"
)

// Options injects the process environment for tests.
type Options struct {
	Stdout      io.Writer
	Stderr      io.Writer
	LookupEnv   func(string) (string, bool)
	Getwd       func() (string, error)
	PathOptions config.PathOptions
	// NewModel overrides the model factory. Tests use it to inject a fake model.
	NewModel func(config.Model) (ports.Model, error)
}

// App holds the configured state shared by every command.
type App struct {
	Stdout io.Writer
	Stderr io.Writer

	Paths  config.Paths
	Config config.Config
	Logger *slog.Logger
	JSON   bool

	lookupEnv   func(string) (string, bool)
	getwd       func() (string, error)
	pathOptions config.PathOptions
	newModel    func(config.Model) (ports.Model, error)
	customModel bool
	flags       config.Flags
	configured  bool
}

// New builds an unconfigured App.
func New(opts Options) *App {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.LookupEnv
	}
	if opts.Getwd == nil {
		opts.Getwd = os.Getwd
	}
	customModel := opts.NewModel != nil
	if opts.NewModel == nil {
		opts.NewModel = defaultModelFactory
	}
	return &App{
		Stdout:      opts.Stdout,
		Stderr:      opts.Stderr,
		lookupEnv:   opts.LookupEnv,
		getwd:       opts.Getwd,
		pathOptions: opts.PathOptions,
		newModel:    opts.NewModel,
		customModel: customModel,
		Logger:      slog.New(slog.DiscardHandler),
	}
}

// SetJSON selects machine-readable output. It must be called before Configure so that even a
// configuration error is rendered as a JSON envelope.
func (a *App) SetJSON(enabled bool) { a.JSON = enabled }

// Configure resolves paths, loads the configuration and builds the logger.
func (a *App) Configure(flags config.Flags) error {
	workDir, err := a.getwd()
	if err != nil {
		return core.Wrap(core.CodeInternal, err, "resolve working directory")
	}

	paths, err := config.ResolvePaths(a.pathOptions)
	if err != nil {
		return err
	}
	cfg, paths, err := config.Load(paths, config.Options{
		Flags:     flags,
		LookupEnv: a.lookupEnv,
		WorkDir:   workDir,
	})
	if err != nil {
		return err
	}

	logger, err := telemetry.New(telemetry.Options{
		Level:  cfg.General.LogLevel,
		Format: cfg.General.LogFormat,
		Output: a.Stderr,
	})
	if err != nil {
		return core.Wrap(core.CodeConfigInvalid, err, "configure logger")
	}

	a.Paths = paths
	a.Config = cfg
	a.Logger = logger
	a.flags = flags
	a.configured = true
	return nil
}

// Configured reports whether Configure has succeeded.
func (a *App) Configured() bool { return a.configured }

// ResolvePathsOnly resolves the default OS paths without loading configuration. It is used by
// commands that must work even when the configuration file is broken.
func (a *App) ResolvePathsOnly() (config.Paths, error) {
	return config.ResolvePaths(a.pathOptions)
}

// WorkDir returns the working directory captured at startup.
func (a *App) WorkDir() (string, error) {
	return a.getwd()
}

// ConfigSources lists the files that contributed to the effective configuration.
func (a *App) ConfigSources() []config.Source {
	return config.Sources(a.Paths, a.flags, a.lookupEnv)
}

// Printf writes human-readable output to stdout.
func (a *App) Printf(format string, args ...any) {
	_, _ = fmt.Fprintf(a.Stdout, format, args...)
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package cli wires the Cobra command tree to the application layer. It owns flags, output
// formatting and exit codes; it never contains business logic.
package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/app"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/config"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/version"
)

const skipConfigAnnotation = "liquidharness/skip-config"

// Execute runs the CLI and returns the process exit code. Every error is rendered exactly once.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	a := app.New(app.Options{Stdout: stdout, Stderr: stderr, Stdin: os.Stdin})
	root := NewRootCommand(ctx, a)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}

	err = normalizeError(err)
	a.ReportError(err)
	return core.ExitCode(err)
}

// ExitCode exposes the documented mapping for callers that execute commands directly.
func ExitCode(err error) int { return core.ExitCode(err) }

func normalizeError(err error) error {
	var coded *core.Error
	if errors.As(err, &coded) {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return core.Wrap(core.CodeCanceled, err, "operation canceled")
	}
	if isUsageError(err) {
		return core.Wrap(core.CodeUsage, err, "invalid invocation")
	}
	return core.Wrap(core.CodeInternal, err, "unexpected failure")
}

func isUsageError(err error) bool {
	message := err.Error()
	for _, prefix := range []string{
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"flag needs an argument",
		"invalid argument",
		"accepts ",
		"requires ",
	} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}

// NewRootCommand builds the command tree. It is exported so tests can execute commands with an
// injected App and context.
func NewRootCommand(ctx context.Context, a *app.App) *cobra.Command {
	var jsonOutput bool

	root := &cobra.Command{
		Use:           "liquidharness",
		Short:         "Local-first agent harness for LFM2.5",
		Long:          rootLong,
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The TUI lands in F5. Until then, bare invocation shows help.
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			a.SetJSON(jsonOutput)
			if cmd.Annotations[skipConfigAnnotation] == "true" {
				return nil
			}
			return a.Configure(flagsFrom(cmd))
		},
	}
	root.SetVersionTemplate("liquidharness {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return core.Wrap(core.CodeUsage, err, "invalid flags")
	})

	flags := root.PersistentFlags()
	flags.String("config", "", "config file (default: OS config directory)")
	flags.String("env-file", "", "load environment variables from this .env file")
	flags.String("workspace", "", "workspace root for the filesystem jail")
	flags.String("profile", "", "configuration profile: fast, balanced or quality")
	flags.String("log-level", "", "log level: debug, info, warn or error")
	flags.String("log-format", "", "log format: text or json")
	flags.BoolVar(&jsonOutput, "json", false, "machine-readable output (one JSON object on stdout)")

	root.AddCommand(
		newVersionCommand(a),
		newConfigCommand(a),
		newDoctorCommand(ctx, a),
		newPolicyCommand(a),
		newRunCommand(ctx, a),
		newAskCommand(ctx, a),
		newChatCommand(a),
		newServeCommand(a),
		newInitCommand(a),
		newModelsCommand(ctx, a),
		newRAGCommand(a),
		newMemoryCommand(a),
		newSessionsCommand(a),
		newEvalCommand(a),
		newUpdateCommand(a),
	)
	return root
}

const rootLong = `LiquidHarness is a local-first agent harness for Liquid AI LFM2.5.

The model runs on your machine. Risky tools require explicit approval, every path is resolved
through a workspace jail and every action is recorded locally.

Project status: F1. The version, config, doctor, policy and models commands work, and "ask" and
"run --print" can talk to an OpenAI-compatible LFM2.5 backend. The agent loop, tools, storage,
TUI and installers are planned for F2-F8.

Documentation: https://github.com/HugoFernandoColmenares/LiquidHarness/tree/main/docs/files`

// flagsFrom reads the persistent flags that were explicitly set. Nil pointers mean "not set".
func flagsFrom(cmd *cobra.Command) config.Flags {
	flags := cmd.Flags()
	out := config.Flags{}

	if flags.Changed("config") {
		value, _ := flags.GetString("config")
		out.ConfigFile = &value
	}
	if flags.Changed("env-file") {
		value, _ := flags.GetString("env-file")
		out.EnvFile = &value
	}
	if flags.Changed("workspace") {
		value, _ := flags.GetString("workspace")
		out.Workspace = &value
	}
	if flags.Changed("profile") {
		value, _ := flags.GetString("profile")
		out.Profile = &value
	}
	if flags.Changed("log-level") {
		value, _ := flags.GetString("log-level")
		out.LogLevel = &value
	}
	if flags.Changed("log-format") {
		value, _ := flags.GetString("log-format")
		out.LogFormat = &value
	}
	if flags.Changed("json") {
		value, _ := flags.GetBool("json")
		out.JSON = &value
	}
	if flags.Changed("model") {
		value, _ := flags.GetString("model")
		out.Model = &value
	}
	if flags.Changed("base-url") {
		value, _ := flags.GetString("base-url")
		out.BaseURL = &value
	}
	if flags.Changed("policy") {
		value, _ := flags.GetString("policy")
		out.PolicyMode = &value
	}
	return out
}

func skipConfig() map[string]string {
	return map[string]string{skipConfigAnnotation: "true"}
}

func noArgs(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return core.E(core.CodeUsage, "unexpected argument %q", args[0])
	}
	return nil
}

func notImplemented(name, phase string) error {
	return core.E(core.CodeInternal,
		"%s is not implemented yet (planned for %s); see docs/files/08-implementation-plan.md",
		name, phase)
}

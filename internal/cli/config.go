// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/liquidharness/liquidharness/internal/app"
	"github.com/liquidharness/liquidharness/internal/config"
	"github.com/liquidharness/liquidharness/internal/core"
)

func newConfigCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect, validate and initialize configuration",
		Long: `Inspect the effective configuration.

Precedence, highest first: flags, environment, .env file, project config
(<workspace>/.liquidharness/config.toml), user config and compiled defaults.`,
		Args: noArgs,
	}
	cmd.AddCommand(
		newConfigPathCommand(a),
		newConfigShowCommand(a),
		newConfigValidateCommand(a),
		newConfigInitCommand(a),
	)
	return cmd
}

func newConfigPathCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:         "path",
		Short:       "Print config, data, cache, log and database locations",
		Args:        noArgs,
		Annotations: skipConfig(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := a.ResolvePathsOnly()
			if err != nil {
				return err
			}
			workDir, err := a.WorkDir()
			if err != nil {
				return core.Wrap(core.CodeInternal, err, "resolve working directory")
			}
			workspace, _ := cmd.Flags().GetString("workspace")
			if workspace == "" {
				workspace = workDir
			} else if !filepath.IsAbs(workspace) {
				workspace = filepath.Join(workDir, workspace)
			}
			paths.ProjectConfig = filepath.Join(workspace, ".liquidharness", "config.toml")

			if a.JSON {
				return a.PrintJSON(paths)
			}
			a.Printf("config     %s\n", paths.UserConfig)
			a.Printf("project    %s\n", paths.ProjectConfig)
			a.Printf("data       %s\n", paths.DataDir)
			a.Printf("cache      %s\n", paths.CacheDir)
			a.Printf("models     %s\n", paths.ModelsDir)
			a.Printf("logs       %s\n", paths.LogsDir)
			a.Printf("database   %s\n", paths.DBPath)
			return nil
		},
	}
}

func newConfigShowCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration (secrets redacted)",
		Args:  noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if a.JSON {
				return a.PrintJSON(a.Config.Redacted())
			}
			if err := a.Config.EncodeTOML(a.Stdout); err != nil {
				return core.Wrap(core.CodeInternal, err, "encode configuration")
			}
			return nil
		},
	}
}

func newConfigValidateCommand(a *app.App) *cobra.Command {
	type validateOutput struct {
		Valid     bool            `json:"valid"`
		Workspace string          `json:"workspace"`
		Sources   []config.Source `json:"sources"`
	}

	return &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration and report the files that contributed",
		Args:  noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			// A successful Configure means the configuration already passed validation.
			sources := a.ConfigSources()
			if a.JSON {
				return a.PrintJSON(validateOutput{
					Valid:     true,
					Workspace: a.Config.General.Workspace,
					Sources:   sources,
				})
			}
			a.Printf("configuration valid\n")
			a.Printf("workspace  %s\n", a.Config.General.Workspace)
			if len(sources) == 0 {
				a.Printf("sources    compiled defaults\n")
				return nil
			}
			for _, source := range sources {
				a.Printf("source     %-8s %s\n", source.Kind, source.Path)
			}
			return nil
		},
	}
}

func newConfigInitCommand(a *app.App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:         "init",
		Short:       "Write a commented default configuration file",
		Args:        noArgs,
		Annotations: skipConfig(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := a.ResolvePathsOnly()
			if err != nil {
				return err
			}
			target := paths.UserConfig
			if flag, _ := cmd.Flags().GetString("config"); flag != "" {
				target = flag
			}

			created, err := config.WriteDefault(target, force)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(struct {
					Path    string `json:"path"`
					Created bool   `json:"created"`
				}{Path: target, Created: created})
			}
			a.Printf("wrote %s\n", target)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing configuration file")
	return cmd
}

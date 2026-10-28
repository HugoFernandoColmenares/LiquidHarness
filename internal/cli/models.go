// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/app"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/models"
)

func newModelsCommand(ctx context.Context, a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Manage local model weights",
		Long: `Manage the local GGUF cache.

Downloads resume from a .part file, are verified against the SHA-256 in the manifest and are
installed atomically. The manifest ships inside the binary; model.manifest and
LIQUIDHARNESS_MODEL_MANIFEST can point at a newer one.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		newModelsListCommand(ctx, a),
		newModelsPullCommand(ctx, a),
		newModelsVerifyCommand(ctx, a),
		newModelsUseCommand(ctx, a),
		newModelsRemoveCommand(ctx, a),
		newModelsPathCommand(ctx, a),
	)
	return cmd
}

func newModelsListCommand(ctx context.Context, a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available and installed models",
		Args:  noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			result, err := a.ModelList(ctx)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(result)
			}

			a.Printf("models  %s\n", result.Dir)
			if result.Current != "" {
				a.Printf("current %s\n", result.Current)
			}
			if len(result.Models) == 0 {
				a.Printf("no models in the manifest\n")
				return nil
			}
			a.Printf("\n%-24s %-8s %10s  %s\n", "ID", "QUANT", "SIZE", "STATE")
			for _, model := range result.Models {
				a.Printf("%-24s %-8s %10s  %s\n",
					model.ID, model.Quantization, formatSize(model.SizeBytes), modelState(model))
			}
			return nil
		},
	}
}

func newModelsPullCommand(ctx context.Context, a *app.App) *cobra.Command {
	var acceptLicense bool
	cmd := &cobra.Command{
		Use:   "pull <id>",
		Short: "Download and verify a model",
		Long: `Download a model from the manifest and verify its SHA-256 before installing it.

The model license must be accepted explicitly with --accept-license.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return core.E(core.CodeUsage, "model id must not be empty")
			}

			model, err := a.ModelPull(ctx, id, acceptLicense)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(model)
			}
			a.Printf("installed %s\n", model.Path)
			if !model.Current {
				a.Printf("select it with: liquidharness models use %s\n", model.ID)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&acceptLicense, "accept-license", false,
		"accept the model license shown in the manifest")
	return cmd
}

func newModelsVerifyCommand(ctx context.Context, a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "verify [id]",
		Short: "Verify installed model checksums",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := ""
			if len(args) == 1 {
				id = strings.TrimSpace(args[0])
			}

			report, err := a.ModelVerify(ctx, id)
			if err != nil {
				return err
			}

			if !report.OK {
				conflict := core.E(core.CodeConflict, "one or more models failed verification")
				if a.JSON {
					if err := a.PrintJSONReport(false, report, conflict); err != nil {
						return err
					}
					return core.Silent(core.CodeConflict, "one or more models failed verification")
				}
				printVerifyResults(a, report)
				return conflict
			}

			if a.JSON {
				return a.PrintJSON(report)
			}
			if len(report.Results) == 0 {
				a.Printf("no installed models to verify\n")
				return nil
			}
			printVerifyResults(a, report)
			return nil
		},
	}
}

func newModelsUseCommand(ctx context.Context, a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "use <id>",
		Short: "Select the default model for autostart",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			model, err := a.ModelUse(ctx, id)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(model)
			}
			a.Printf("current model %s\n", model.ID)
			return nil
		},
	}
}

func newModelsRemoveCommand(ctx context.Context, a *app.App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Delete a model from the cache",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if err := a.ModelRemove(ctx, id, force); err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(struct {
					Removed string `json:"removed"`
				}{Removed: id})
			}
			a.Printf("removed %s\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "remove the current model without confirmation")
	return cmd
}

func newModelsPathCommand(ctx context.Context, a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "path [id]",
		Short: "Print the model cache or one model file",
		Long: `Print the model cache directory.

With an id, print the GGUF file of an installed model. With no id, print the current model path
when one is selected, otherwise the cache directory.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := ""
			if len(args) == 1 {
				id = strings.TrimSpace(args[0])
			}
			result, err := a.ModelPath(ctx, id)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(result)
			}
			a.Printf("%s\n", result.Path)
			return nil
		},
	}
}

func printVerifyResults(a *app.App, report app.ModelVerifyReport) {
	for _, result := range report.Results {
		status := "OK  "
		detail := result.Path
		if !result.OK {
			status = "FAIL"
			detail = result.Path + ": " + result.Error
		}
		a.Printf("%s %s\n", status, detail)
	}
}

func modelState(model models.Model) string {
	switch {
	case model.Current:
		return "current"
	case model.Installed:
		return "installed"
	default:
		return "available"
	}
}

func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTP"[exp])
}

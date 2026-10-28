// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/app"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

func newDoctorCommand(ctx context.Context, a *app.App) *cobra.Command {
	var skipModelCheck bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose configuration, paths, permissions and model connectivity",
		Long: `Check that the harness can run on this machine.

The model endpoint is reported as a warning when unreachable: llama-server may be started later.
Use --skip-model-check in CI or offline environments.`,
		Args: noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			report := a.Doctor(ctx, skipModelCheck)

			if a.JSON {
				var err error
				if !report.OK {
					err = core.E(core.CodeInternal, "doctor found %d failing check(s)", report.Failures)
				}
				if err := a.PrintJSONReport(report.OK, report, err); err != nil {
					return core.Wrap(core.CodeInternal, err, "write doctor report")
				}
				if !report.OK {
					return core.Silent(core.CodeInternal, "doctor found %d failing check(s)", report.Failures)
				}
				return nil
			}

			report.Render(a.Stdout)
			if !report.OK {
				return core.Silent(core.CodeInternal, "doctor found %d failing check(s)", report.Failures)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&skipModelCheck, "skip-model-check", false, "do not probe the model endpoint")
	return cmd
}

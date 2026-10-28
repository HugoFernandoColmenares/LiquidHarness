// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"github.com/spf13/cobra"

	"github.com/liquidharness/liquidharness/internal/app"
	"github.com/liquidharness/liquidharness/internal/version"
)

func newVersionCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Print build metadata",
		Long:        "Print the version, commit, build date, Go version, OS and architecture.",
		Args:        noArgs,
		Annotations: skipConfig(),
		RunE: func(_ *cobra.Command, _ []string) error {
			info := version.Get()
			if a.JSON {
				return a.PrintJSON(info)
			}
			a.Printf("%s\n", info)
			return nil
		},
	}
}

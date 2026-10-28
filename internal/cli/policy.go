// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"github.com/spf13/cobra"

	"github.com/liquidharness/liquidharness/internal/app"
)

func newPolicyCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Inspect the effective security policy",
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the effective policy",
		Args:  noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			policyConfig := a.Config.Policy
			if a.JSON {
				return a.PrintJSON(struct {
					Mode          string `json:"mode"`
					AllowShell    bool   `json:"allow_shell"`
					AllowNetwork  bool   `json:"allow_network"`
					MaxWriteBytes int64  `json:"max_write_bytes"`
					Workspace     string `json:"workspace"`
				}{
					Mode:          policyConfig.Mode.String(),
					AllowShell:    policyConfig.AllowShell,
					AllowNetwork:  policyConfig.AllowNetwork,
					MaxWriteBytes: policyConfig.MaxWriteBytes,
					Workspace:     a.Config.General.Workspace,
				})
			}
			a.Printf("mode             %s\n", policyConfig.Mode)
			a.Printf("allow_shell      %t\n", policyConfig.AllowShell)
			a.Printf("allow_network    %t\n", policyConfig.AllowNetwork)
			a.Printf("max_write_bytes  %d\n", policyConfig.MaxWriteBytes)
			a.Printf("workspace        %s\n", a.Config.General.Workspace)
			return nil
		},
	})
	return cmd
}

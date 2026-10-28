// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"github.com/spf13/cobra"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/app"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/policy"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/tools"
)

// policyRow is one line of `policy test`.
type policyRow struct {
	Tool     string `json:"tool"`
	Risk     string `json:"risk"`
	Decision string `json:"decision"`
	Rule     string `json:"rule"`
}

func newPolicyCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Inspect the effective security policy",
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newPolicyShowCommand(a), newPolicyTestCommand(a))
	return cmd
}

func newPolicyShowCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
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
	}
}

// newPolicyTestCommand evaluates the engine against the F3 catalog without executing a tool.
// It is the offline way to answer "what would happen if the model called this?".
func newPolicyTestCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Evaluate the policy against the tool catalog without running tools",
		Long: `Evaluate the policy against the tool catalog without running tools.

Every registered tool is decided at its static risk class under the effective mode and
allowlist. The table answers what would happen before the model calls anything.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			engine, err := policy.NewEngine(a.Config.Policy.Mode, a.Config.Tools.Allowlist)
			if err != nil {
				return core.Wrap(core.CodeConfigInvalid, err, "build the policy engine")
			}
			registry, err := tools.NewWorkspace(a.Config.General.Workspace, tools.Options{
				MaxWriteBytes: a.Config.Policy.MaxWriteBytes,
			})
			if err != nil {
				return core.Wrap(core.CodeConfigInvalid, err, "build the tool catalog")
			}

			filter, _ := cmd.Flags().GetString("tool")
			rows := make([]policyRow, 0, registry.Len())
			for _, spec := range registry.Specs() {
				if filter != "" && spec.Name != filter {
					continue
				}
				decision := engine.Decide(spec, spec.Risk)
				rows = append(rows, policyRow{
					Tool:     spec.Name,
					Risk:     spec.Risk.String(),
					Decision: string(decision.Outcome),
					Rule:     decision.Rule,
				})
			}
			if len(rows) == 0 {
				return core.E(core.CodeUsage, "unknown tool %q", filter)
			}

			if a.JSON {
				return a.PrintJSON(struct {
					Mode string      `json:"mode"`
					Rows []policyRow `json:"rows"`
				}{
					Mode: a.Config.Policy.Mode.String(),
					Rows: rows,
				})
			}

			a.Printf("mode %s\n\n", a.Config.Policy.Mode)
			a.Printf("%-12s %-4s %-8s %s\n", "tool", "risk", "decision", "rule")
			for _, row := range rows {
				a.Printf("%-12s %-4s %-8s %s\n", row.Tool, row.Risk, row.Decision, row.Rule)
			}
			return nil
		},
	}
	cmd.Flags().String("tool", "", "only evaluate this tool")
	return cmd
}

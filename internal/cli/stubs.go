// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/app"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

// stub builds a leaf command that is part of the v1 surface but lands in a later phase. It
// keeps `--help` honest: the command and its flags are listed, and running it fails loudly.
func stub(name, use, short, phase string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Long:  short + "\n\nNot implemented yet: planned for " + phase + ".",
		RunE: func(_ *cobra.Command, _ []string) error {
			return notImplemented(name, phase)
		},
	}
}

func addModelFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("policy", "", "policy mode for this run: read-only, ask or allow")
	flags.String("model", "", "model name override")
	flags.String("base-url", "", "OpenAI-compatible base URL override")
}

func addAgentFlags(cmd *cobra.Command) {
	addModelFlags(cmd)
	flags := cmd.Flags()
	flags.StringP("print", "p", "", "run one prompt and print the answer (no TUI)")
	flags.Bool("yes", false, "auto-approve R1 actions (never R2+)")
	flags.Bool("deny", false, "deny every action that requires approval")
	flags.String("allow-risk", "", "auto-approve up to this risk class (R2, R3 or R4)")
}

func newRunCommand(ctx context.Context, a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [prompt]",
		Short: "Run a task with the agent",
		Long: `Run a task with the agent.

The agent can call the read-only tools (fs_read, fs_list, fs_search) inside the workspace and
streams its answer. With --print the answer goes to stdout and tool progress goes to stderr, so
the command is safe to use in scripts. Without a prompt the interactive TUI opens (planned for
F5).`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			interactive, _ := cmd.Flags().GetBool("interactive")
			if interactive {
				return notImplemented("run --interactive", "F5")
			}

			prompt := strings.TrimSpace(strings.Join(args, " "))
			if cmd.Flags().Changed("print") {
				if prompt != "" {
					return core.E(core.CodeUsage,
						"pass the prompt as an argument or with --print, not both")
				}
				printed, _ := cmd.Flags().GetString("print")
				prompt = strings.TrimSpace(printed)
				if prompt == "" {
					return core.E(core.CodeUsage, "--print requires a prompt")
				}
			}
			if prompt == "" {
				return notImplemented("run (interactive)", "F5")
			}
			return runTurn(ctx, a, prompt)
		},
	}
	addAgentFlags(cmd)
	cmd.Flags().BoolP("interactive", "i", false, "open the interactive TUI")
	return cmd
}

func runTurn(ctx context.Context, a *app.App, prompt string) error {
	result, err := a.RunTurn(ctx, app.RunOptions{Prompt: prompt})
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(result)
	}
	if !strings.HasSuffix(result.Answer, "\n") {
		a.Printf("\n")
	}
	return nil
}

func newAskCommand(ctx context.Context, a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ask <prompt>",
		Short: "One-shot question without tools",
		Long: `Ask the model one question and print the answer.

No tools are registered in this path: the model can only answer from the prompt and its own
knowledge. Use run once the agent loop lands in F2.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runAsk(ctx, a, strings.Join(args, " "))
		},
	}
	addModelFlags(cmd)
	return cmd
}

func runAsk(ctx context.Context, a *app.App, prompt string) error {
	result, err := a.Ask(ctx, app.AskOptions{Prompt: prompt, Stream: true})
	if err != nil {
		return err
	}
	if a.JSON {
		return a.PrintJSON(result)
	}
	if !strings.HasSuffix(result.Answer, "\n") {
		a.Printf("\n")
	}
	return nil
}

func newChatCommand(_ *app.App) *cobra.Command {
	return stub("chat", "chat", "Open an interactive chat session", "F5")
}

func newServeCommand(_ *app.App) *cobra.Command {
	cmd := stub("serve", "serve", "Serve the local HTTP API and MCP endpoint", "F7")
	cmd.Flags().String("addr", "", "listen address (default 127.0.0.1:8787)")
	cmd.Flags().String("token", "", "bearer token; required for non-loopback addresses")
	return cmd
}

func newInitCommand(_ *app.App) *cobra.Command {
	cmd := stub("init", "init", "Detect hardware, download a model and run a smoke test", "F8")
	cmd.Flags().Bool("non-interactive", false, "accept defaults without prompting")
	cmd.Flags().String("model", "", "model id to download")
	return cmd
}

func newRAGCommand(_ *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rag",
		Short: "Index and search local documents",
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		stub("rag index", "index [paths...]", "Index files into the local store", "F6"),
		stub("rag search", "search <query>", "Search indexed documents", "F6"),
		stub("rag stats", "stats", "Show index statistics", "F6"),
		stub("rag reindex", "reindex", "Rebuild the index", "F6"),
	)
	return cmd
}

func newMemoryCommand(_ *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage long-term memory",
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		stub("memory list", "list", "List memories", "F6"),
		stub("memory add", "add <text>", "Store a memory", "F6"),
		stub("memory search", "search <query>", "Search memories", "F6"),
		stub("memory forget", "forget <id>", "Delete a memory", "F6"),
		stub("memory export", "export", "Export memories as JSON", "F6"),
	)
	return cmd
}

func newSessionsCommand(_ *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sessions",
		Aliases: []string{"session"},
		Short:   "Inspect and resume sessions",
		Args:    noArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		stub("sessions list", "list", "List sessions", "F4"),
		stub("sessions show", "show <id>", "Show a session transcript", "F4"),
		stub("sessions resume", "resume <id>", "Resume an interrupted session", "F4"),
		stub("sessions delete", "delete <id>", "Delete a session", "F4"),
		stub("sessions export", "export <id>", "Export a session as JSON", "F4"),
	)
	return cmd
}

func newEvalCommand(_ *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run evaluations and compare reports",
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	run := stub("eval run", "run", "Run an evaluation dataset", "F8")
	run.Flags().String("dataset", "smoke", "dataset name: smoke, core, adversarial or regression")
	run.Flags().String("model", "local", "model under test")
	run.Flags().String("report", "", "write the JSON report to this path")
	gate := stub("eval gate", "gate", "Fail if metrics regress against a baseline", "F8")
	gate.Flags().String("baseline", "evals/baseline.json", "baseline metrics file")
	gate.Flags().String("report", "", "report to compare")
	cmd.AddCommand(
		run,
		stub("eval list", "list", "List available datasets", "F8"),
		stub("eval report", "report <file>", "Render a report", "F8"),
		stub("eval compare", "compare <a> <b>", "Compare two reports", "F8"),
		gate,
	)
	return cmd
}

func newUpdateCommand(_ *app.App) *cobra.Command {
	cmd := stub("update", "update", "Update the binary with a signed release", "F8")
	cmd.Flags().Bool("check", false, "check for updates without installing")
	cmd.Flags().Bool("rollback", false, "restore the previous version")
	return cmd
}

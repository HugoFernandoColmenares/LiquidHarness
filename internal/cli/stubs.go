// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"github.com/spf13/cobra"

	"github.com/liquidharness/liquidharness/internal/app"
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

func addAgentFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringP("print", "p", "", "run one prompt and print the answer (no TUI)")
	flags.Bool("yes", false, "auto-approve R1 actions (never R2+)")
	flags.Bool("deny", false, "deny every action that requires approval")
	flags.String("allow-risk", "", "auto-approve up to this risk class (R2, R3 or R4)")
	flags.String("policy", "", "policy mode for this run: read-only, ask or allow")
	flags.String("model", "", "model name override")
	flags.String("base-url", "", "OpenAI-compatible base URL override")
}

func newRunCommand(_ *app.App) *cobra.Command {
	cmd := stub("run", "run [prompt]", "Run a task with the agent", "F1-F3")
	cmd.Long = `Run a task with the agent.

Without arguments the interactive TUI opens (F5). With --print the answer goes to stdout and
logs go to stderr, so the command is safe to use in scripts.

Not implemented yet: planned for F1-F3.`
	addAgentFlags(cmd)
	cmd.Flags().BoolP("interactive", "i", false, "open the interactive TUI")
	return cmd
}

func newAskCommand(_ *app.App) *cobra.Command {
	cmd := stub("ask", "ask <prompt>", "One-shot question without tools", "F1")
	addAgentFlags(cmd)
	return cmd
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

func newModelsCommand(_ *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Manage local model weights",
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	list := stub("models list", "list", "List available and installed models", "F8")
	pull := stub("models pull", "pull <id>", "Download and verify a model", "F8")
	verify := stub("models verify", "verify", "Verify installed model checksums", "F8")
	use := stub("models use", "use <id>", "Select the default model", "F8")
	remove := stub("models remove", "remove <id>", "Delete a model from the cache", "F8")
	path := stub("models path", "path", "Print the model directory", "F8")
	remove.Flags().Bool("force", false, "delete without confirmation")
	cmd.AddCommand(list, pull, verify, use, remove, path)
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

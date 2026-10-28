# LiquidHarness

> A local-first agent harness for Liquid AI LFM2.5. Your files, your machine, your rules.

[![CI](https://github.com/HugoFernandoColmenares/LiquidHarness/actions/workflows/ci.yml/badge.svg)](https://github.com/HugoFernandoColmenares/LiquidHarness/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.27-00ADD8)](go.mod)

LiquidHarness is a single-binary agent runtime that runs LFM2.5 on your computer. It reads,
searches, summarizes and edits the files you point it at — with explicit approvals, a workspace
jail and a full audit trail. No cloud account. No data leaves your machine unless you ask.

> **Project status: F3 — safe execution (complete).** The CLI skeleton (`version`, `config`,
> `doctor`, `policy`), the configuration loader, the build and the CI pipeline are implemented.
> `ask` talks to an OpenAI-compatible LFM2.5 backend, and `run --print` drives a full agent turn:
> SSE streaming, budget and loop detection, context trimming and extractive compaction, parallel
> read-only tools (`fs_read`, `fs_list`, `fs_search`) and, in `ask` or `allow` mode, the write
> tools (`fs_create`, `fs_edit`, `fs_delete`, `fs_move`, `todo_write`) confined to the workspace.
> Every tool call is authorized by the policy engine, approvals are explicit (`--yes`,
> `--allow-risk`, `--deny` or an interactive prompt) and every decision is appended to a JSONL
> audit log. The LFM tool-call parser accepts both the JSON and Pythonic formats, and the model
> manager can download, resume, verify and select GGUF weights. Storage, the TUI and installers
> are **not** implemented yet; `serve`, `rag`, `memory`, `eval` and the other product commands
> currently return `not implemented` and exit with code 1. See the [roadmap](#roadmap) and
> [`docs/files/08-implementation-plan.md`](docs/files/08-implementation-plan.md).

## Why

- **Local by default.** The model runs on your hardware; the harness never phones home.
- **Safe by construction.** Risky tools require approval; the filesystem jail is enforced in code.
- **Small and fast.** One Go binary, no runtime dependencies, starts in milliseconds.
- **Inspectable.** Every tool call, approval and result is recorded and queryable.

## Requirements

- Windows 10/11, macOS 13+, or Linux (x64/arm64)
- 8 GB RAM minimum; 16 GB recommended
- ~3 GB disk for the default model (LFM2.5-2.6B Q5_K_M)
- Go 1.27 to build from source

## Install

Prebuilt installers and package-manager formulae are published from `v1.0.0` onwards. Until then,
build from source:

```bash
git clone https://github.com/HugoFernandoColmenares/LiquidHarness.git
cd LiquidHarness
make build          # or: go build -o bin/liquidharness ./cmd/liquidharness
./bin/liquidharness version   # bin\liquidharness.exe on Windows
```

Once released:

```bash
# macOS / Linux
curl -fsSL https://get.liquidharness.dev | sh

# Windows (PowerShell)
irm https://get.liquidharness.dev/install.ps1 | iex

# Package managers
brew install liquidharness/tap/liquidharness
winget install LiquidHarness.LiquidHarness
scoop install liquidharness
```

## Quick start

These commands work today:

```bash
liquidharness version                 # build metadata
liquidharness version --json          # stable machine-readable envelope
liquidharness config path             # where config, data, cache and logs live
liquidharness config init             # write a commented default config
liquidharness config validate         # validate file + environment + flags
liquidharness config show             # effective configuration as TOML
liquidharness doctor                  # environment diagnostics
liquidharness models list             # manifest + installed models
liquidharness models pull lfm25-2.6b-q5km --accept-license
liquidharness models verify           # re-hash installed weights
liquidharness models use lfm25-2.6b-q5km
liquidharness ask "What is a workspace jail?"   # one-shot, no tools
liquidharness run --print "Responde OK"         # one-shot with SSE streaming
liquidharness run --print "Read README.md and summarize the project"  # agent loop + fs_read
liquidharness run --print "Create docs/notes.md" --yes                 # approve R1 writes
liquidharness run --print "Delete docs/old.md" --allow-risk R2         # approve up to R2
liquidharness run --print "Delete docs/old.md" --deny                  # deny every approval
liquidharness policy test                       # policy decisions without running tools
```

`run --print` drives the agent loop: the model can call `fs_read`, `fs_list` and `fs_search`
inside the workspace, plus `fs_create`, `fs_edit`, `fs_delete`, `fs_move` and `todo_write` when
the mode is `ask` or `allow`. Results are fed back, and the turn ends with a streamed answer.
Tool progress goes to stderr so stdout stays scriptable; `--json` returns a single envelope with
`stop_reason`, `steps`, `tool_calls` and `usage`. The turn is bounded by
`agent.max_steps`, `agent.max_tool_calls`, `agent.max_turn_duration` and the model context window;
old history is trimmed and compacted instead of overflowing.

Writes follow the policy: `read-only` hides them entirely, `ask` prompts before every R1/R2 call,
and `allow` runs them without prompting but still refuses R3+. `--yes` auto-approves R1 only,
`--allow-risk Rn` auto-approves up to Rn, and `--deny` refuses every call that needs approval. In
a non-interactive process without a shortcut, an `ask` decision becomes a denial. Every decision
is appended to `<workspace>/.liquidharness/audit/audit-YYYYMMDD.jsonl` with the tool, risk class,
argument hash, redacted arguments, decision, approver, result, duration and byte count.

`models pull` resumes interrupted downloads, verifies the SHA-256 from the manifest and installs
the weights atomically. `models use` selects the model that `ask` and `run --print` start through
`llama-server` when `model.autostart` is enabled.

### Model latency

The 2.6B chat template always starts in thinking mode, so a one-shot answer costs 12–17 s on a
CPU-only machine (~11 tok/s, 180–440 completion tokens). The 1.2B Instruct model answers the same
prompt in ~0.6 s with a warm server, and in ~3.5 s when the CLI has to start and stop
`llama-server` for a single run. Point the harness at your own weights with
`LIQUIDHARNESS_MODEL_PATH` or `model.path` in the config file:

```bash
LIQUIDHARNESS_MODEL_PATH=/path/to/LFM2.5-1.2B-Instruct-Q8_0.gguf liquidharness run --print "Responde OK"
```

Product commands land in later phases:

```bash
liquidharness init                    # hardware detection + model download (F8)
liquidharness run                     # interactive TUI (F5)
liquidharness rag index ./docs        # F6
liquidharness eval run --dataset smoke # F8
```

## How it works

```text
you → TUI/CLI → agent loop → LFM2.5 (llama-server)
                     ↓
             policy + approvals
                     ↓
           tools (fs, rag, memory) → workspace
                     ↓
                SQLite (state, audit)
```

## Safety

- The model is untrusted input; its tool calls are validated against JSON Schema.
- Writes and deletes require approval by default (`ask` policy).
- `fs_edit` requires the SHA-256 of the file it is editing, so a stale write fails with `CONFLICT`.
- Deletes move the entry to `.liquidharness/trash/` unless `permanent=true` is explicit.
- Shell and network tools are disabled unless you enable them.
- Every action is audited locally as append-only JSONL.

See [SECURITY.md](SECURITY.md) for the threat model and reporting process.

## Configuration

Config lives in the OS config directory (`liquidharness config path`). Environment variables use
the `LIQUIDHARNESS_` prefix and override file values; flags override both. See
[`.env.example`](.env.example) and
[docs/files/04-architecture.md](docs/files/04-architecture.md#14-configuración-paths-y-lifecycle).

## Documentation

- [Documentation index](docs/files/README.md)
- [Architecture](docs/files/04-architecture.md)
- [Tool protocol](docs/files/05-tool-protocol.md)
- [Design guide](docs/files/06-design-guide.md)
- [Installer](docs/files/07-installer.md)
- [Implementation plan](docs/files/08-implementation-plan.md)
- [Testing & evals](docs/files/11-testing-eval.md)

## Roadmap

| Phase | Scope | Status |
| :--- | :--- | :--- |
| F0 | CLI skeleton, config, doctor, CI | Done |
| F1 | Model client, streaming, tool-call parser | Done |
| F2 | Agent loop and context manager | Done |
| F3 | Tools, policy, approvals, workspace jail | Done |
| F4 | SQLite persistence and sessions | Planned |
| F5 | Bubble Tea TUI | Planned |
| F6 | RAG and memory | Planned |
| F7 | MCP client/server and `serve` | Planned |
| F8 | Installers, evals and `v1.0.0` | Planned |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). We use trunk-based development, Conventional Commits and
require tests with every change. By participating you agree to the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Author

Created and maintained by [Hugo Colmenares](https://github.com/HugoFernandoColmenares).

## License

The harness is [Apache-2.0](LICENSE). LFM2.5 model weights are distributed under the
[LFM Open License v1.0](https://www.liquid.ai/lfm-open-license) and are **not** covered by the
Apache-2.0 license. See [NOTICE](NOTICE).

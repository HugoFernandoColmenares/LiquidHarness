# LiquidHarness

> A local-first agent harness for Liquid AI LFM2.5. Your files, your machine, your rules.

[![CI](https://github.com/liquidharness/liquidharness/actions/workflows/ci.yml/badge.svg)](https://github.com/liquidharness/liquidharness/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.27-00ADD8)](go.mod)

LiquidHarness is a single-binary agent runtime that runs LFM2.5 on your computer. It reads,
searches, summarizes and edits the files you point it at — with explicit approvals, a workspace
jail and a full audit trail. No cloud account. No data leaves your machine unless you ask.

> **Project status: F0 — foundations.** The CLI skeleton (`version`, `config`, `doctor`), the
> configuration loader, the build and the CI pipeline are implemented. The agent loop, tools,
> storage and TUI are **not** implemented yet; `run`, `serve`, `rag`, `memory`, `eval` and the
> other product commands currently return `not implemented` and exit with code 1. See the
> [roadmap](#roadmap) and [`docs/files/08-implementation-plan.md`](docs/files/08-implementation-plan.md).

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
git clone https://github.com/liquidharness/liquidharness.git
cd liquidharness
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
```

Product commands land in later phases:

```bash
liquidharness init                    # hardware detection + model download (F8)
liquidharness run --print "Summarize docs/README.md"   # F1–F3
liquidharness rag index ./docs        # F6
liquidharness eval run --dataset smoke # F8
liquidharness                         # TUI (F5)
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
- Shell and network tools are disabled unless you enable them.
- Every action is audited locally.

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
| F1 | Model client, streaming, tool-call parser | Planned |
| F2 | Agent loop and context manager | Planned |
| F3 | Tools, policy, approvals, workspace jail | Planned |
| F4 | SQLite persistence and sessions | Planned |
| F5 | Bubble Tea TUI | Planned |
| F6 | RAG and memory | Planned |
| F7 | MCP client/server and `serve` | Planned |
| F8 | Installers, evals and `v1.0.0` | Planned |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). We use trunk-based development, Conventional Commits and
require tests with every change. By participating you agree to the
[Code of Conduct](CODE_OF_CONDUCT.md).

## License

The harness is [Apache-2.0](LICENSE). LFM2.5 model weights are distributed under the
[LFM Open License v1.0](https://www.liquid.ai/lfm-open-license) and are **not** covered by the
Apache-2.0 license. See [NOTICE](NOTICE).

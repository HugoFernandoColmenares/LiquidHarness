# LiquidHarness — Documentation Set

> **Status:** Documentation baseline (v1.0) · **Date:** 2026-10-28 · **Owner:** Architecture & Product
> **Source of truth:** [`project.md`](./project.md) defines the product intent. The documents in this
> folder refine, challenge and operationalize that intent. When a conflict exists, `project.md` wins
> on scope and this set wins on technical detail, until an ADR supersedes it.

---

## 1. What this documentation is

This folder contains the research, architecture, design and delivery documentation required to build
**LiquidHarness** (commercial name) / **LFM-Harness** (technical name): a local-first, single-binary
agentic harness written in Go for the **Liquid AI LFM2.5** model family.

The documentation is deliberately split into **research** (what exists and why), **design** (what we
will build and how), and **delivery** (how we will build, ship and verify it). Every document is
versioned in the repository and reviewed like code.

---

## 2. Document map

| # | Document | Purpose | Audience | Status |
| :- | :--- | :--- | :--- | :--- |
| 00 | [`README.md`](./README.md) | Index, conventions, glossary and reading paths | Everyone | ✅ |
| 01 | [`01-harness-research.md`](./01-harness-research.md) | State of the art of agent harnesses, components, protocols and failure modes | Product, architecture | ✅ |
| 02 | [`02-lfm25-runtime-research.md`](./02-lfm25-runtime-research.md) | LFM2.5 capabilities, chat/tool format, quantization and inference backends | Architecture, ML | ✅ |
| 03 | [`03-go-stack-research.md`](./03-go-stack-research.md) | Go 1.27 stack selection: TUI, storage, vector search, MCP, CLI, tooling | Engineering | ✅ |
| 04 | [`04-architecture.md`](./04-architecture.md) | Architecture design: layers, ports, agent loop, persistence, concurrency | Engineering | ✅ |
| 05 | [`05-tool-protocol.md`](./05-tool-protocol.md) | Canonical tool contract, risk model, approvals, MCP bridge | Engineering, security | ✅ |
| 06 | [`06-design-guide.md`](./06-design-guide.md) | TUI design system: components, tokens, spacing, color, keybindings | Design, frontend | ✅ |
| 07 | [`07-installer.md`](./07-installer.md) | Distribution, installers, first-run bootstrap, updates and uninstall | Release, DevOps | ✅ |
| 08 | [`08-implementation-plan.md`](./08-implementation-plan.md) | Phased plan, WBS, root-file specifications, risks and release gates | Everyone | ✅ |
| 09 | [`09-adr.md`](./09-adr.md) | Architecture Decision Records (ADR-0001 … ADR-0010) | Engineering | ✅ |
| 10 | [`10-security.md`](./10-security.md) | Threat model, prompt injection, path jail, sandboxing, supply chain | Security, engineering | ✅ |
| 11 | [`11-testing-eval.md`](./11-testing-eval.md) | Test pyramid, TUI tests, LLM evaluation framework and quality gates | Engineering, QA | ✅ |

### Screenshots

All UI captures referenced by the design guide live in [`../screenshots/`](../screenshots/). The
naming convention, required viewports and capture checklist are defined in
[§16 of the design guide](./06-design-guide.md#16-inventario-de-capturas-docsscreenshots).

---

## 3. Reading paths

### 3.1 New contributor (≈ 3 h)

1. [`project.md`](./project.md) — product intent.
2. [`04-architecture.md`](./04-architecture.md) — how the system is shaped.
3. [`05-tool-protocol.md`](./05-tool-protocol.md) — how tools are declared, gated and executed.
4. [`11-testing-eval.md`](./11-testing-eval.md) — how we prove it works.
5. [`08-implementation-plan.md`](./08-implementation-plan.md) — what to pick up next.

### 3.2 Architect / tech lead (≈ 5 h)

1. `01-harness-research.md` → `02-lfm25-runtime-research.md` → `03-go-stack-research.md`.
2. `04-architecture.md` + `09-adr.md`.
3. `10-security.md` + `05-tool-protocol.md`.
4. `07-installer.md` + `08-implementation-plan.md`.

### 3.3 Designer (≈ 2 h)

1. [`06-design-guide.md`](./06-design-guide.md) — tokens, components, layouts, accessibility.
2. `04-architecture.md` §4 and §18.3 (TUI package and adding screens) — how the UI talks to the agent.
3. `05-tool-protocol.md` §6 (approval flow) — the safety-critical interaction.

### 3.4 Release engineer (≈ 1.5 h)

1. [`07-installer.md`](./07-installer.md).
2. [`08-implementation-plan.md`](./08-implementation-plan.md) §11–§13 and §15 (root files, testing, gates and release).
3. `10-security.md` §9 (supply chain).

---

## 4. Conventions used across the set

| Convention | Meaning |
| :--- | :--- |
| **MUST / SHOULD / MAY** | Interpreted as in RFC 2119. `MUST` is a release blocker if violated. |
| **v1 / post-1.0** | v1 is the first public release; post-1.0 is explicitly out of the critical path. |
| **Model** | The LFM2.5 weights served by an inference backend. The model is **untrusted input**. |
| **Harness** | The deterministic Go runtime that owns the loop, tools, policy, state and UI. |
| **Tool** | A typed capability exposed to the model, always executed by the harness. |
| **Turn** | One user request and everything the harness does until a final answer or abort. |
| **Step** | One model call or one tool execution inside a turn. |
| **Workspace** | The only directory tree a session may write to by default. |
| **Paths** | POSIX-style in docs; Windows separators are handled by `internal/paths`. |
| **Code** | Go 1.27, `gofmt`/`gofumpt`, `golangci-lint`, standard project layout. |
| **Units** | Bytes are binary (KiB/MiB/GiB); latency is p50/p95 unless stated. |
| **Secrets** | Never in docs, logs, traces, prompts or screenshots. Placeholders only. |

### 4.1 Identifier conventions

- Tool names: `snake_case`, verb-first, ≤ 48 chars, globally unique (`fs_read`, `rag_search`).
- Go packages: short, lowercase, no stutter (`internal/agent`, not `internal/agentloop`).
- Interfaces: capability nouns (`Model`, `Store`, `Retriever`), not `IThing`.
- Config keys: `snake_case`, nested by section (`model.context_window`).
- Env vars: `LIQUIDHARNESS_` prefix (`LIQUIDHARNESS_MODEL_BASE_URL`).
- ADRs: `ADR-NNNN`, immutable once accepted; superseded, never edited silently.
- Screenshots: `NN-area--state--WxH.png` (see design guide §16).

---

## 5. Glossary

| Term | Definition |
| :--- | :--- |
| **Agentic loop** | The cycle *observe → think → act (tool) → verify* driven by the harness. |
| **Harness** | Runtime layer that wraps the model: loop, context, tools, permissions, state, UI. |
| **Context window** | Maximum tokens the model can attend to (LFM2.5: 131,072). |
| **Compaction** | Summarizing/pruning history to stay within the context budget. |
| **KV cache** | Key/value attention cache reused across turns for faster decoding. |
| **Tool call** | A structured request emitted by the model that the harness validates and executes. |
| **MCP** | Model Context Protocol: JSON-RPC 2.0 standard for tools/resources/prompts. |
| **RAG** | Retrieval-Augmented Generation: retrieve local chunks, inject as context. |
| **Chunk** | A retrievable unit of a document (typically 300–800 tokens with overlap). |
| **Embedding** | Dense vector representation of text used for semantic search. |
| **Golden set** | Curated evaluation dataset used as a regression baseline. |
| **LLM-as-judge** | Using a model to score outputs against a rubric; calibrated against humans. |
| **Risk class** | R0–R5 classification of a tool's side effects (see tool protocol §4). |
| **Approval** | Explicit human decision required before a risky tool executes. |
| **Workspace jail** | Path-resolution policy that forbids escaping the workspace root. |
| **Headless mode** | Non-interactive execution (`--print`, `--json`) for CI and automation. |
| **Design token** | Named value (color, space, border) that components reference instead of literals. |
| **Profile** | Named configuration preset (`cpu`, `balanced`, `quality`, `remote`). |

---

## 6. Decision log (summary)

The full records are in [`09-adr.md`](./09-adr.md). Accepted decisions at the time of writing:

| ADR | Decision |
| :--- | :--- |
| 0001 | Go 1.27, single binary, hexagonal modular monolith, `cmd/` + `internal/`. |
| 0002 | HTTP OpenAI-compatible runtime as default; embedded llama.cpp as opt-in build. |
| 0003 | SQLite (`modernc.org/sqlite`, pure Go) + FTS5 + `sqlite-vec` for state and RAG. |
| 0004 | Bubble Tea v2 + Lip Gloss v2 + Bubbles v2 for the TUI. |
| 0005 | Canonical internal message/tool model with provider adapters. |
| 0006 | Policy engine + explicit approvals + workspace jail as non-bypassable defaults. |
| 0007 | MCP support in both directions (client and server). |
| 0008 | Evaluation as a first-class package and release gate. |
| 0009 | TOML config + `.env` overrides + flags; XDG/AppData paths. |
| 0010 | Apache-2.0 for the harness; LFM model license handled separately. |

---

## 7. Maintenance rules

1. **Docs are code.** Changes to behavior require a docs change in the same pull request.
2. **One source of truth.** Token values live in `internal/tui/theme/tokens.go`; the design guide
   mirrors them and CI checks drift (see design guide §11.6).
3. **ADRs are immutable.** To change a decision, add a new ADR that supersedes the old one.
4. **Research gets dated.** Every research document carries a `Last verified` date; links are
   re-checked at each minor release.
5. **No secrets, no customer data.** Examples use synthetic paths (`/home/dev/project`).
6. **English for public artifacts.** `README.md`, `LICENSE`, `SECURITY.md`, CLI help and code
   comments are English. Internal design docs may be Spanish; identifiers remain English.

---

## 8. Open questions (tracked)

| ID | Question | Owner | Needed by |
| :--- | :--- | :--- | :--- |
| Q-01 | Embedded runtime: CGO `llama.cpp` vs pure-Go `goccy/go-llama` vs subprocess only? | Architecture | Phase 2 |
| Q-02 | Minimum supported LFM2.5 variant for v1 (2.6B only, or 8B-A1B too)? | Product | Phase 1 |
| Q-03 | Is `shell_exec` in v1 or opt-in post-1.0? | Security | Phase 3 |
| Q-04 | Commercial license terms for LFM2.5 weights vs Apache-2.0 harness | Legal | Before 1.0 |
| Q-05 | Telemetry: none vs opt-in anonymous metrics | Product | Phase 0 |
| Q-06 | Windows sandbox: Job Objects now or post-1.0? | Security | Phase 8 |

Answers become ADRs; this table is updated when they close.

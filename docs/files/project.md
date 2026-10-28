# LiquidHarness: Lightweight Agentic Harness for Liquid AI LFM-2.5

This document outlines the architectural specifications, core agentic loop design, tool-use execution layer, and integrated advanced capabilities (RAG, Shared Memory, and Prompt EVALs) for **LFM-Harness** or LiquidHarness (commercial name), a high-performance local AI harness written in **Go (Golang)**.

---

## 1. Architectural Vision & Scope

LFM-Harness is designed as a standalone, ultra-optimized local agentic runtime. It wraps the **Liquid AI LFM-2.5** model weights directly using native Go bindings or lightweight HTTP local inference, completely bypassing heavy external desktop dependencies like LM Studio or Ollama where possible, while enforcing strict local execution rules.

* **Language Stack:** Written entirely in **Go** to ensure minimal memory overhead, lightning-fast compilation, and concurrency safety via goroutines.
* **Core Agentic Loop:** Implements the reasoning cycle: **Observe $\rightarrow$ Think $\rightarrow$ Execute Tool $\rightarrow$ Verify**.
* **Integrated Modules:** Extends the basic file-system agent with an embedded Vector RAG engine, an isolated Shared Memory manager, and an automated Prompt EVAL suite.

---

## 2. Technology Stack & Minimal Requirements

| Component | Technology | Purpose |
| :--- | :--- | :--- |
| **Backend / Core Engine** | Go (Golang 1.22+) | Agent loop execution, concurrent tool dispatching, file system IO. |
| **Model Runtime** | Direct local weight loading (llama.go / CGO bindings) or optimized local HTTP inference | Executes Liquid AI LFM-2.5 locally with minimal RAM footprint. |
| **Local Persistence** | `mattn/go-sqlite3` or bbolt | Embedded key-value/relational storage for memories, RAG index metadata, and test logs. |
| **Vector Indexing** | In-memory or lightweight disk-based vector search | Powers the local RAG document retrieval pipeline. |

---

## 3. Modular Directory Structure

```text
lfm-harness/
├── cmd/
│   └── harness/
│       └── main.go              # Application entry point & CLI orchestrator
├── internal/
│   ├── agent/
│   │   ├── loop.go              # Core reasoning & execution loop
│   │   └── prompt.go            # System prompts and state formatting
│   ├── llm/
│   │   └── client.go            # Direct local runtime connection for LFM-2.5
│   ├── tools/
│   │   ├── files.go             # File system operations (CRUD)
│   │   ├── rag.go               # Local Vector RAG search module
│   │   └── memory.go            # Shared memory manager (Mem0 style)
│   └── eval/
│       └── runner.go            # Automated prompt evaluation framework
├── data/                        # Local SQLite databases and vector stores
└── go.mod

```

---

## 4. Core Toolset: File System CRUD Operations

The harness exposes native file system capabilities to the LFM-2.5 model through structured JSON tool declarations. The Go backend safely validates paths to prevent directory traversal attacks.

1. **Read File (`fs_read`):** Safely reads textual contents of a designated local path.
2. **Create File (`fs_create`):** Generates new files with validated directory structures.
3. **Edit File (`fs_edit`):** Replaces or patches file blocks selectively.
4. **Delete File (`fs_delete`):** Removes target files following safety confirmation checks.

---

## 5. Advanced Integrated Features

### A. Contextual RAG Engine (`internal/tools/rag.go`)

* **Functionality:** Ingests local markdown documentation, codebase files, or notes, chunking them into embedded vectors stored locally.
* **Integration:** Before the agent processes a complex request, the RAG tool runs a semantic similarity search, injecting precise contextual snippets directly into the LFM-2.5 context window.

### B. Shared Memory Manager (`internal/tools/memory.go`)

* **Functionality:** Implements an external key-value and semantic memory ledger (similar to Mem0 principles).
* **Integration:** The agent automatically logs technical decisions, recurring user preferences, and error corrections into this isolated database across different sessions, keeping long-term context intact.

### C. Prompt Evaluation Framework (`internal/eval/runner.go`)

* **Functionality:** A built-in testing suite running unit tests specifically for LLM outputs.
* **Integration:** Combines real development prompts with expected assertion criteria, allowing developers to execute automated regression runs whenever system prompts or model parameters are adjusted.

---

## 6. Coding Standards & Engineering Rules

* **Concurrency Safety:** Leverage Go routines and channels exclusively for parallel tool-use evaluation or batch EVAL execution.
* **Error Resilience:** Gracefully catch file permission errors or malformed model JSON outputs, feeding errors back into the agent loop so LFM-2.5 can self-correct.
* **Zero Bloat:** Avoid heavy third-party web frameworks for the core runtime; rely on Go's standard library (`os`, `io`, `net/http`, `encoding/json`).

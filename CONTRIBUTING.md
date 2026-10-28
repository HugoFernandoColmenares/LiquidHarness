# Contributing to LiquidHarness

Thanks for helping. This project is a local-first agent harness; correctness, safety and
inspectability matter more than feature velocity.

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## Requirements

- Go 1.27 (`go version` must report `go1.27.x`).
- `make` (optional but recommended) and a POSIX shell for the helper scripts.
- `make tools` to install `gofumpt`, `goimports`, `golangci-lint`, `govulncheck` and `goreleaser`.

## Getting started

```bash
git clone https://github.com/liquidharness/liquidharness.git
cd liquidharness
make tools
make check          # fmt-check + vet + lint + test
make build
./bin/liquidharness version
```

## Workflow

1. Open an issue describing the problem, or pick an existing one.
2. Create a short-lived branch: `feat/…`, `fix/…`, `docs/…`, `refactor/…`.
3. Keep pull requests small (≤ 400 changed lines, excluding generated code and migrations).
4. Add tests in the same pull request. Domain coverage must stay ≥ 85 %.
5. Update documentation when behavior changes (README, guides, ADR).
6. Sign off your commits (`git commit -s`). We use DCO, not a CLA.
7. Open the pull request and fill in the template. One approval is required; two for
   `internal/policy/`, `internal/sandbox/` and `.github/workflows/`.

We use trunk-based development and squash merges. Branches should live ≤ 2 days.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/):

```text
feat(cli): add config validate command
fix(policy): reject symlinks that escape the workspace
docs(architecture): document the context budget
```

Allowed types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`.
Breaking changes use `!` and a `BREAKING CHANGE:` footer.

## Code style

- Format with `gofumpt` and `goimports`; `make fmt` does both.
- Every `.go` file starts with:

  ```go
  // Copyright 2026 The LiquidHarness Authors
  // SPDX-License-Identifier: Apache-2.0
  ```

- Package comments are mandatory (`doc.go` is fine).
- Errors are wrapped with context and mapped to a stable `core.Code`; never `panic` for user input.
- Dependencies: each new direct dependency needs a justification in the PR and a license check.
  The direct dependency budget is ≤ 15 (ADR-0001).
- Respect the dependency table in
  [`docs/files/04-architecture.md`](docs/files/04-architecture.md#41-responsabilidades-y-límites).
  `depguard` enforces it.

## Adding a tool

Read [`docs/files/04-architecture.md`](docs/files/04-architecture.md#181-añadir-una-tool) and
[`docs/files/05-tool-protocol.md`](docs/files/05-tool-protocol.md). A tool needs:

- A JSON Schema 2020-12 input schema and a risk class (R0–R5).
- A handler that resolves every path through the workspace jail.
- Unit tests, including adversarial paths and a denied-policy case.
- An entry in the tool registry documentation.

## Adding a model provider

Implement the ports in `internal/ports`, keep wire DTOs inside the adapter, and add a contract test
against a fake server. Do not leak provider-specific fields into `internal/core`.

## Reporting bugs

Use the issue template and include `liquidharness version --json` plus `liquidharness doctor
--json`. Redact paths, tokens and personal data before posting.

Security issues go to [SECURITY.md](SECURITY.md), never to the public tracker.

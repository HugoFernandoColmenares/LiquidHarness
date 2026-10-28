# Security Policy

LiquidHarness runs an AI model on your machine and lets it act on your files. We take the boundary
between the model and your filesystem seriously.

## Supported versions

| Version | Supported |
| :--- | :--- |
| Latest minor | Yes |
| Previous minor | Yes (security fixes only) |
| Older | No |

Until `v1.0.0` the project is pre-release and only `main` is supported.

## Reporting a vulnerability

**Do not open a public issue.** Report privately through
[GitHub Security Advisories](https://github.com/liquidharness/liquidharness/security/advisories/new).
If you cannot use GitHub, email `security@liquidharness.dev` with the subject
`[SECURITY] LiquidHarness`.

Please include:

- Affected version or commit, OS and architecture.
- A minimal reproduction (commands, config, files).
- Impact: what an attacker gains and which trust boundary is crossed.
- Redacted logs (`liquidharness doctor --json` and `--log-level=debug`), never real secrets or
  personal files.

## Response targets

| Stage | Target |
| :--- | :--- |
| Acknowledgement | ≤ 72 hours |
| Triage and severity | ≤ 7 days |
| Critical fix | ≤ 7 days |
| High fix | ≤ 30 days |
| Medium fix | ≤ 90 days |

We follow coordinated disclosure: we agree on a publication date with the reporter, credit them
unless they prefer otherwise, and publish an advisory with the fixed version.

## Scope

In scope:

- The harness binary, CLI, TUI and configuration loader.
- The workspace jail, policy engine and approval flow.
- Installers, update channel, release signing and supply chain.
- The local HTTP API and MCP bridge.

Out of scope:

- Model behavior, hallucinations or prompt-injection text that does not lead to an unauthorized
  tool call.
- Denial of service against your own machine with local access.
- Issues that require an attacker to already control your user account or workspace.
- Third-party model weights; report those to the model publisher.

## Practices

- Releases are built by CI from tagged commits, with checksums, SBOM and Sigstore signatures.
- `govulncheck`, CodeQL, `golangci-lint` and dependency review run on every change.
- Dependencies require justification and license review; GPL/AGPL/SSPL are not accepted without a
  documented exception.
- The default configuration disables shell and network tools, and asks before writes or deletes.
- No telemetry is sent by default; `telemetry = "off"` unless you opt in.

See [`docs/files/10-security.md`](docs/files/10-security.md) for the full threat model.

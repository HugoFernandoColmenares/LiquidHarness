// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package sandbox executes untrusted child processes with resource limits, a restricted
// environment, no inherited handles and platform-specific isolation. It is only reachable from
// the opt-in shell tool, after policy allows R3.
//
// Planned for F3. On unsupported platforms the shell tool stays disabled rather than silently
// running without limits.
package sandbox

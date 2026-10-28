// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package fsx contains safe filesystem primitives: the workspace jail, atomic writes, text and
// binary detection and bounded directory walking. Every path that reaches the filesystem goes
// through Jail.Resolve first.
//
// Planned for F3. Tests cover traversal, symlinks, UNC paths, NTFS alternate data streams and
// TOCTOU revalidation.
package fsx

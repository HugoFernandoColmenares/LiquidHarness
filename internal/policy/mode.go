// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package policy turns a tool call plus configuration into a decision: allow, ask or deny.
// It is pure: no filesystem, network, clock or terminal access. The workspace jail lives in
// package fsx and is evaluated before the policy engine sees a path.
package policy

import (
	"fmt"
	"strings"
)

// Mode is the global posture of the harness.
type Mode string

const (
	// ModeReadOnly denies every write and every side effect.
	ModeReadOnly Mode = "read-only"
	// ModeAsk allows reads and asks before writes or destruction. This is the default.
	ModeAsk Mode = "ask"
	// ModeAllow allows everything the policy rules permit without prompting.
	ModeAllow Mode = "allow"
)

// ParseMode validates and normalizes a mode string.
func ParseMode(s string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(s)))
	if !mode.Valid() {
		return "", fmt.Errorf("invalid policy mode %q: expected read-only, ask or allow", s)
	}
	return mode, nil
}

// Valid reports whether the mode is one of the three supported postures.
func (m Mode) Valid() bool {
	switch m {
	case ModeReadOnly, ModeAsk, ModeAllow:
		return true
	default:
		return false
	}
}

// String returns the canonical mode name.
func (m Mode) String() string { return string(m) }

// AllowsWrites reports whether writes are permitted at all. In ask mode they still need
// approval; this only answers whether the mode forbids them outright.
func (m Mode) AllowsWrites() bool { return m == ModeAsk || m == ModeAllow }

// MarshalText implements encoding.TextMarshaler for TOML and JSON.
func (m Mode) MarshalText() ([]byte, error) {
	if !m.Valid() {
		return nil, fmt.Errorf("invalid policy mode %q", string(m))
	}
	return []byte(m), nil
}

// UnmarshalText implements encoding.TextUnmarshaler for TOML and JSON.
func (m *Mode) UnmarshalText(text []byte) error {
	parsed, err := ParseMode(string(text))
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

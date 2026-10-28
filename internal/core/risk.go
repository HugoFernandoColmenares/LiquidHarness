// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"fmt"
	"strings"
)

// RiskClass is the blast radius of a tool call, from R0 (pure read) to R5 (secrets).
// The policy engine, not the model, decides what each class may do.
type RiskClass int

const (
	// RiskR0 is a pure read with no side effects.
	RiskR0 RiskClass = iota
	// RiskR1 is a reversible local write.
	RiskR1
	// RiskR2 is destruction or overwrite.
	RiskR2
	// RiskR3 is code execution.
	RiskR3
	// RiskR4 is network access with exfiltration potential.
	RiskR4
	// RiskR5 is access to secrets and credentials.
	RiskR5
)

// String returns the canonical R0–R5 label.
func (r RiskClass) String() string {
	if r < RiskR0 || r > RiskR5 {
		return fmt.Sprintf("R?%d", int(r))
	}
	return fmt.Sprintf("R%d", int(r))
}

// Valid reports whether the class is inside the defined range.
func (r RiskClass) Valid() bool { return r >= RiskR0 && r <= RiskR5 }

// ParseRisk parses an R0–R5 label, case-insensitively.
func ParseRisk(s string) (RiskClass, error) {
	normalized := strings.ToUpper(strings.TrimSpace(s))
	if len(normalized) == 2 && normalized[0] == 'R' && normalized[1] >= '0' && normalized[1] <= '5' {
		return RiskClass(normalized[1] - '0'), nil
	}
	return 0, fmt.Errorf("invalid risk class %q: expected R0–R5", s)
}

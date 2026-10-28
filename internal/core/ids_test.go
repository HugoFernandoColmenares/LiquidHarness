// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

func TestNewID(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		id := core.NewID()
		if len(id) != 26 {
			t.Fatalf("id %q has length %d, want 26", id, len(id))
		}
		if strings.ContainsAny(id, "ILOU") {
			t.Fatalf("id %q contains a character outside Crockford base32", id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}
}

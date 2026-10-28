// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package fsx_test

import (
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/fsx"
)

func TestFreeBytes(t *testing.T) {
	free, err := fsx.FreeBytes(t.TempDir())
	if err != nil {
		t.Skipf("free space is not available on this platform: %v", err)
	}
	if free == 0 {
		t.Error("expected a non-zero free space")
	}
}

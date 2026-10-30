// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package fsx

import (
	"fmt"
	"syscall"
)

func freeBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	// Statfs_t field types differ per platform (Bsize is int64 on linux, uint32 on darwin;
	// Bavail is int64 on some BSDs), so convert through a checked helper.
	avail, ok := nonNegative(stat.Bavail)
	if !ok {
		return 0, fmt.Errorf("statfs %s: negative available blocks", path)
	}
	bsize, ok := nonNegative(stat.Bsize)
	if !ok {
		return 0, fmt.Errorf("statfs %s: negative block size", path)
	}
	return avail * bsize, nil
}

// nonNegative converts v to uint64, reporting false when v is negative.
func nonNegative[T ~int32 | ~int64 | ~uint32 | ~uint64](v T) (uint64, bool) {
	if v < 0 {
		return 0, false
	}
	return uint64(v), true
}

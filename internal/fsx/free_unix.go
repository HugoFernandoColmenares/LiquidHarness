// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package fsx

import "syscall"

func freeBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

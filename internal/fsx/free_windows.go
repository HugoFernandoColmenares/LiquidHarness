// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package fsx

import (
	"syscall"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

func freeBytes(path string) (uint64, error) {
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}

	var freeToCaller, total, totalFree uint64
	//nolint:gosec // GetDiskFreeSpaceExW requires pointers to uint64 values.
	result, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(target)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if result == 0 {
		return 0, callErr
	}
	return freeToCaller, nil
}

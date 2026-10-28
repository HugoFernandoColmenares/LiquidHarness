// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package fsx

// FreeBytes reports the free space available to the current user on the volume that contains
// path. The path must exist; callers that only have a directory that may not exist yet should
// create it or walk up to the nearest existing parent.
func FreeBytes(path string) (uint64, error) {
	return freeBytes(path)
}

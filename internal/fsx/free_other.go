// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows && !unix

package fsx

import "errors"

func freeBytes(string) (uint64, error) {
	return 0, errors.New("free disk space is not supported on this platform")
}

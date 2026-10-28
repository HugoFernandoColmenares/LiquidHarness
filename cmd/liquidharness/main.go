// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Command liquidharness is the single entry point of the harness.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/liquidharness/liquidharness/internal/cli"
)

func main() {
	os.Exit(run())
}

// run keeps the deferred cleanup inside a function so os.Exit does not skip it.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

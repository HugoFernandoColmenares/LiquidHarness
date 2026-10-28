// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package version exposes build metadata injected at link time.
package version

import (
	"fmt"
	"runtime"
)

// Build metadata. Overridden with -ldflags by the Makefile and GoReleaser.
var (
	// Version is the release version, for example "1.0.0" or "dev".
	Version = "dev"
	// Commit is the short git commit the binary was built from.
	Commit = "none"
	// Date is the UTC build timestamp in RFC 3339 format.
	Date = "unknown"
)

// Info is the stable, machine-readable description of the running binary.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Get returns the build metadata of the running binary.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// String renders a single human-readable line.
func (i Info) String() string {
	return fmt.Sprintf("liquidharness %s (%s) built %s · %s %s/%s",
		i.Version, i.Commit, i.Date, i.GoVersion, i.OS, i.Arch)
}

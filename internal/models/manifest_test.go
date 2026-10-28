// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package models_test

import (
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/models"
)

func TestDefaultManifestIsValid(t *testing.T) {
	manifest, err := models.DefaultManifest()
	if err != nil {
		t.Fatalf("default manifest: %v", err)
	}
	if len(manifest.Models) == 0 {
		t.Fatal("default manifest has no models")
	}

	seen := make(map[string]struct{}, len(manifest.Models))
	for _, entry := range manifest.Models {
		if _, duplicate := seen[entry.ID]; duplicate {
			t.Errorf("duplicate model id %q", entry.ID)
		}
		seen[entry.ID] = struct{}{}
	}
}

func TestParseManifestRejectsUnknownFields(t *testing.T) {
	_, err := models.ParseManifest([]byte(`{"schema":1,"models":[],"extra":true}`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !core.Is(err, core.CodeConfigInvalid) {
		t.Errorf("error = %v, want CONFIG_INVALID", err)
	}
}

func TestManifestFind(t *testing.T) {
	manifest, err := models.DefaultManifest()
	if err != nil {
		t.Fatalf("default manifest: %v", err)
	}

	entry, ok := manifest.Find("lfm25-2.6b-q5km")
	if !ok {
		t.Fatal("Q5_K_M is missing from the default manifest")
	}
	if entry.Quantization != "Q5_K_M" {
		t.Errorf("quantization = %q, want Q5_K_M", entry.Quantization)
	}
	if !strings.HasSuffix(entry.FileName(), ".gguf") {
		t.Errorf("file name = %q", entry.FileName())
	}
	if _, ok := manifest.Find("does-not-exist"); ok {
		t.Error("unexpected match")
	}
}

func TestEntryDownloadURLUsesMirror(t *testing.T) {
	entry := models.Entry{URL: "https://example.com/models/weights.gguf"}

	if got := entry.DownloadURL(""); got != entry.URL {
		t.Errorf("download URL = %q, want %q", got, entry.URL)
	}
	if got := entry.DownloadURL("https://mirror.example.com/lfm/"); got != "https://mirror.example.com/lfm/weights.gguf" {
		t.Errorf("mirror URL = %q", got)
	}
}

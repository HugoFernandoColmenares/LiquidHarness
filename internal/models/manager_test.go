// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package models_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/models"
)

var testPayload = []byte(strings.Repeat("liquidharness-gguf-", 64))

func TestPullInstallsVerifiesAndSelects(t *testing.T) {
	server, requests := newModelServer(t, testPayload)
	manifest, entry := testManifest(t, testPayload, "", server.URL+"/test-model.gguf")

	dir := t.TempDir()
	manager := newTestManager(t, dir, manifest, server.Client(), nil)

	model, err := manager.Pull(context.Background(), entry.ID, models.PullOptions{AcceptLicense: true})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if !model.Installed {
		t.Error("model is not installed")
	}
	if model.Current {
		t.Error("a fresh pull must not be current")
	}
	if requests.Load() == 0 {
		t.Error("server was never called")
	}

	data, err := os.ReadFile(filepath.Join(dir, entry.ID, models.ModelFileName))
	if err != nil {
		t.Fatalf("read model: %v", err)
	}
	if string(data) != string(testPayload) {
		t.Error("installed model does not match the payload")
	}

	for _, name := range []string{models.ManifestFileName, models.SHA256FileName, models.LicenseFileName} {
		if _, err := os.Stat(filepath.Join(dir, entry.ID, name)); err != nil {
			t.Errorf("%s is missing: %v", name, err)
		}
	}

	report, err := manager.Verify(context.Background(), entry.ID)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !report.OK {
		t.Errorf("verify failed: %+v", report)
	}

	selected, err := manager.Use(entry.ID)
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if !selected.Current {
		t.Error("selected model is not current")
	}
	if _, err := manager.Use(entry.ID); err != nil {
		t.Fatalf("second use: %v", err)
	}

	list, err := manager.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || !list[0].Current || !list[0].Installed {
		t.Errorf("list = %+v", list)
	}
}

func TestPullRequiresLicense(t *testing.T) {
	server, requests := newModelServer(t, testPayload)
	manifest, entry := testManifest(t, testPayload, "", server.URL+"/test-model.gguf")

	manager := newTestManager(t, t.TempDir(), manifest, server.Client(), nil)
	_, err := manager.Pull(context.Background(), entry.ID, models.PullOptions{})
	if err == nil {
		t.Fatal("expected a license error")
	}
	if !core.Is(err, core.CodeUsage) {
		t.Errorf("error = %v, want USAGE", err)
	}
	if requests.Load() != 0 {
		t.Error("download started before the license was accepted")
	}
}

func TestPullResumesFromPartFile(t *testing.T) {
	const resumeAt = 100

	var sawRange atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			sawRange.Store(true)
			start, err := parseRangeStart(rangeHeader)
			if err != nil || start >= len(testPayload) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(testPayload)-1, len(testPayload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(testPayload[start:])
			return
		}
		_, _ = w.Write(testPayload)
	}))
	defer server.Close()

	manifest, entry := testManifest(t, testPayload, "", server.URL+"/test-model.gguf")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, entry.ID), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	part := filepath.Join(dir, entry.ID, models.ModelFileName+models.PartSuffix)
	if err := os.WriteFile(part, testPayload[:resumeAt], 0o600); err != nil {
		t.Fatalf("write part: %v", err)
	}

	manager := newTestManager(t, dir, manifest, server.Client(), nil)
	if _, err := manager.Pull(context.Background(), entry.ID, models.PullOptions{AcceptLicense: true}); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if !sawRange.Load() {
		t.Error("pull did not send a Range header")
	}

	data, err := os.ReadFile(filepath.Join(dir, entry.ID, models.ModelFileName))
	if err != nil {
		t.Fatalf("read model: %v", err)
	}
	if string(data) != string(testPayload) {
		t.Error("resumed model does not match the payload")
	}
}

func TestPullRejectsCorruptDownload(t *testing.T) {
	server, _ := newModelServer(t, testPayload)
	manifest, entry := testManifest(t, testPayload, strings.Repeat("0", 64), server.URL+"/test-model.gguf")

	dir := t.TempDir()
	manager := newTestManager(t, dir, manifest, server.Client(), nil)

	_, err := manager.Pull(context.Background(), entry.ID, models.PullOptions{AcceptLicense: true})
	if err == nil {
		t.Fatal("expected a checksum error")
	}
	if !core.Is(err, core.CodeConflict) {
		t.Errorf("error = %v, want CONFLICT", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, entry.ID, models.ModelFileName)); !os.IsNotExist(statErr) {
		t.Error("a corrupt model was installed")
	}
	if _, statErr := os.Stat(filepath.Join(dir, entry.ID, models.ModelFileName+models.PartSuffix)); !os.IsNotExist(statErr) {
		t.Error("the corrupt .part file was not deleted")
	}
}

func TestPullRetriesTransientFailures(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(testPayload)
	}))
	defer server.Close()

	manifest, entry := testManifest(t, testPayload, "", server.URL+"/test-model.gguf")
	manager := newTestManager(t, t.TempDir(), manifest, server.Client(), nil)

	if _, err := manager.Pull(context.Background(), entry.ID, models.PullOptions{AcceptLicense: true}); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if requests.Load() != 2 {
		t.Errorf("requests = %d, want 2", requests.Load())
	}
}

func TestPullChecksFreeSpace(t *testing.T) {
	server, requests := newModelServer(t, testPayload)
	manifest, entry := testManifest(t, testPayload, "", server.URL+"/test-model.gguf")

	manager := newTestManager(t, t.TempDir(), manifest, server.Client(),
		func(string) (uint64, error) { return 1, nil })

	_, err := manager.Pull(context.Background(), entry.ID, models.PullOptions{AcceptLicense: true})
	if err == nil {
		t.Fatal("expected a disk space error")
	}
	if !core.Is(err, core.CodeModelUnavailable) {
		t.Errorf("error = %v, want MODEL_UNAVAILABLE", err)
	}
	if requests.Load() != 0 {
		t.Error("download started without enough disk space")
	}
}

func TestPullRejectsConcurrentLock(t *testing.T) {
	server, _ := newModelServer(t, testPayload)
	manifest, entry := testManifest(t, testPayload, "", server.URL+"/test-model.gguf")

	dir := t.TempDir()
	modelDir := filepath.Join(dir, entry.ID)
	if err := os.MkdirAll(modelDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, models.LockSuffix), []byte("other"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	manager := newTestManager(t, dir, manifest, server.Client(), nil)
	_, err := manager.Pull(context.Background(), entry.ID, models.PullOptions{AcceptLicense: true})
	if err == nil {
		t.Fatal("expected a lock conflict")
	}
	if !core.Is(err, core.CodeConflict) {
		t.Errorf("error = %v, want CONFLICT", err)
	}
}

func TestVerifyDetectsCorruption(t *testing.T) {
	server, _ := newModelServer(t, testPayload)
	manifest, entry := testManifest(t, testPayload, "", server.URL+"/test-model.gguf")

	dir := t.TempDir()
	manager := newTestManager(t, dir, manifest, server.Client(), nil)
	if _, err := manager.Pull(context.Background(), entry.ID, models.PullOptions{AcceptLicense: true}); err != nil {
		t.Fatalf("pull: %v", err)
	}

	path := filepath.Join(dir, entry.ID, models.ModelFileName)
	file, err := os.OpenFile(path, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open model: %v", err)
	}
	if _, err := file.WriteAt([]byte("X"), 0); err != nil {
		t.Fatalf("corrupt model: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close model: %v", err)
	}

	result, err := manager.Verify(context.Background(), entry.ID)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.OK {
		t.Fatal("corruption was not detected")
	}
	if !strings.Contains(result.Error, "sha256") {
		t.Errorf("error = %q", result.Error)
	}
}

func TestRemoveCurrentModelRequiresForce(t *testing.T) {
	server, _ := newModelServer(t, testPayload)
	manifest, entry := testManifest(t, testPayload, "", server.URL+"/test-model.gguf")

	dir := t.TempDir()
	manager := newTestManager(t, dir, manifest, server.Client(), nil)
	if _, err := manager.Pull(context.Background(), entry.ID, models.PullOptions{AcceptLicense: true}); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if _, err := manager.Use(entry.ID); err != nil {
		t.Fatalf("use: %v", err)
	}

	if err := manager.Remove(entry.ID, false); err == nil {
		t.Fatal("expected a conflict for the current model")
	} else if !core.Is(err, core.CodeConflict) {
		t.Errorf("error = %v, want CONFLICT", err)
	}

	if err := manager.Remove(entry.ID, true); err != nil {
		t.Fatalf("forced remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, entry.ID)); !os.IsNotExist(err) {
		t.Error("model directory still exists")
	}
	current, err := manager.Current()
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current != "" {
		t.Errorf("current = %q, want empty", current)
	}
}

func TestPullInstalledModelDoesNotDownload(t *testing.T) {
	server, requests := newModelServer(t, testPayload)
	manifest, entry := testManifest(t, testPayload, "", server.URL+"/test-model.gguf")

	manager := newTestManager(t, t.TempDir(), manifest, server.Client(), nil)
	if _, err := manager.Pull(context.Background(), entry.ID, models.PullOptions{AcceptLicense: true}); err != nil {
		t.Fatalf("pull: %v", err)
	}
	before := requests.Load()

	if _, err := manager.Pull(context.Background(), entry.ID, models.PullOptions{}); err != nil {
		t.Fatalf("second pull: %v", err)
	}
	if requests.Load() != before {
		t.Error("an installed model was downloaded again")
	}
}

func newModelServer(t *testing.T, payload []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
			start, err := parseRangeStart(rangeHeader)
			if err != nil || start >= len(payload) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start:])
			return
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func testManifest(t *testing.T, payload []byte, sha, url string) (models.Manifest, models.Entry) {
	t.Helper()

	if sha == "" {
		sum := sha256.Sum256(payload)
		sha = hex.EncodeToString(sum[:])
	}
	entry := models.Entry{
		ID:           "test-model",
		Name:         "Test Model",
		Quantization: "Q4_0",
		Context:      2048,
		SizeBytes:    int64(len(payload)),
		SHA256:       sha,
		URL:          url,
		License:      "Test License",
		LicenseURL:   "https://example.invalid/LICENSE",
	}
	manifest := models.Manifest{Schema: models.ManifestSchema, Models: []models.Entry{entry}}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("test manifest: %v", err)
	}
	return manifest, entry
}

func newTestManager(
	t *testing.T,
	dir string,
	manifest models.Manifest,
	client *http.Client,
	free func(string) (uint64, error),
) *models.Manager {
	t.Helper()

	manager, err := models.New(models.Options{
		Dir:        dir,
		Manifest:   manifest,
		HTTPClient: client,
		FreeBytes:  free,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return manager
}

func parseRangeStart(header string) (int, error) {
	header = strings.TrimPrefix(header, "bytes=")
	header = strings.TrimSuffix(header, "-")
	return strconv.Atoi(header)
}

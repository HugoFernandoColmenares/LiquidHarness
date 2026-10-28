// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/app"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/models"
)

type jsonEnvelope struct {
	SchemaVersion int             `json:"schema_version"`
	OK            bool            `json:"ok"`
	Data          json.RawMessage `json:"data"`
	Error         *struct {
		Code     string `json:"code"`
		ExitCode int    `json:"exit_code"`
	} `json:"error"`
}

func TestModelsListPathAndPull(t *testing.T) {
	payload := []byte(strings.Repeat("cli-model-payload-", 32))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	configPath, a := modelsTestApp(t, payload, server.URL+"/test-model.gguf")
	run := func(args ...string) (string, error) {
		return execute(t, a, append([]string{"--config", configPath}, args...)...)
	}

	output, err := run("models", "list", "--json")
	if err != nil {
		t.Fatalf("models list: %v", err)
	}
	var listed jsonEnvelope
	if err := json.Unmarshal([]byte(output), &listed); err != nil {
		t.Fatalf("decode list: %v\n%s", err, output)
	}
	var listData struct {
		Dir    string         `json:"dir"`
		Models []models.Model `json:"models"`
	}
	if err := json.Unmarshal(listed.Data, &listData); err != nil {
		t.Fatalf("decode list data: %v", err)
	}
	if len(listData.Models) != 1 || listData.Models[0].Installed {
		t.Errorf("unexpected list: %+v", listData.Models)
	}

	output, err = run("models", "path", "--json")
	if err != nil {
		t.Fatalf("models path: %v", err)
	}
	var pathData struct {
		Dir  string `json:"dir"`
		Path string `json:"path"`
	}
	var pathEnvelope jsonEnvelope
	if err := json.Unmarshal([]byte(output), &pathEnvelope); err != nil {
		t.Fatalf("decode path: %v\n%s", err, output)
	}
	if err := json.Unmarshal(pathEnvelope.Data, &pathData); err != nil {
		t.Fatalf("decode path data: %v", err)
	}
	if pathData.Path != pathData.Dir {
		t.Errorf("path without id = %q, want the cache dir %q", pathData.Path, pathData.Dir)
	}

	if _, err := run("models", "pull", "test-model"); err == nil {
		t.Fatal("pull without --accept-license succeeded")
	} else if !core.Is(err, core.CodeUsage) {
		t.Errorf("error = %v, want USAGE", err)
	}

	if _, err := run("models", "pull", "test-model", "--accept-license"); err != nil {
		t.Fatalf("models pull: %v", err)
	}

	if _, err := run("models", "use", "test-model", "--json"); err != nil {
		t.Fatalf("models use: %v", err)
	}

	output, err = run("models", "verify", "--json")
	if err != nil {
		t.Fatalf("models verify: %v", err)
	}
	var verifyEnvelope jsonEnvelope
	if err := json.Unmarshal([]byte(output), &verifyEnvelope); err != nil {
		t.Fatalf("decode verify: %v\n%s", err, output)
	}
	var verifyData struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(verifyEnvelope.Data, &verifyData); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	if !verifyData.OK {
		t.Errorf("verify did not pass: %s", output)
	}

	// Corrupt the installed weights and make sure verify reports a conflict.
	modelPath := filepath.Join(a.Paths.ModelsDir, "test-model", models.ModelFileName)
	file, err := os.OpenFile(modelPath, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open model: %v", err)
	}
	if _, err := file.WriteAt([]byte("X"), 0); err != nil {
		t.Fatalf("corrupt model: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close model: %v", err)
	}

	output, err = run("models", "verify", "--json")
	if err == nil {
		t.Fatal("verify accepted a corrupt model")
	}
	if !core.Is(err, core.CodeConflict) {
		t.Errorf("error = %v, want CONFLICT", err)
	}
	var failed jsonEnvelope
	if err := json.Unmarshal([]byte(output), &failed); err != nil {
		t.Fatalf("decode failed verify: %v\n%s", err, output)
	}
	if failed.OK || failed.Error == nil || failed.Error.Code != string(core.CodeConflict) {
		t.Errorf("unexpected failure envelope: %s", output)
	}

	if _, err := run("models", "remove", "test-model", "--force", "--json"); err != nil {
		t.Fatalf("models remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.Paths.ModelsDir, "test-model")); !os.IsNotExist(err) {
		t.Error("model directory still exists after remove")
	}
}

func TestModelsPullUnknownModel(t *testing.T) {
	configPath, a := modelsTestApp(t, []byte("payload"), "https://example.invalid/model.gguf")

	_, err := execute(t, a, "--config", configPath, "models", "pull", "missing", "--accept-license")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !core.Is(err, core.CodeNotFound) {
		t.Errorf("error = %v, want NOT_FOUND", err)
	}
}

func TestModelsUseRequiresInstalledModel(t *testing.T) {
	configPath, a := modelsTestApp(t, []byte("payload"), "https://example.invalid/model.gguf")

	_, err := execute(t, a, "--config", configPath, "models", "use", "test-model")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !core.Is(err, core.CodeNotFound) {
		t.Errorf("error = %v, want NOT_FOUND", err)
	}
}

func modelsTestApp(t *testing.T, payload []byte, url string) (string, *app.App) {
	t.Helper()

	sum := sha256.Sum256(payload)
	manifest := models.Manifest{
		Schema: models.ManifestSchema,
		Models: []models.Entry{{
			ID:           "test-model",
			Name:         "Test Model",
			Quantization: "Q4_0",
			Context:      2048,
			SizeBytes:    int64(len(payload)),
			SHA256:       hex.EncodeToString(sum[:]),
			URL:          url,
			License:      "Test License",
			LicenseURL:   "https://example.invalid/LICENSE",
		}},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("test manifest: %v", err)
	}

	dir := t.TempDir()
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	manifestPath := filepath.Join(dir, "models.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	configPath := filepath.Join(dir, "config.toml")
	configData := "config_version = 1\n\n[model]\nmanifest = \"" + filepath.ToSlash(manifestPath) + "\"\n"
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return configPath, newTestApp(t)
}

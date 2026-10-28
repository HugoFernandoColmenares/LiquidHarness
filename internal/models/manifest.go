// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package models manages local GGUF weights: the embedded manifest, the on-disk cache, resumable
// downloads and SHA-256 verification. It never loads a model; it only prepares files for the
// llama-server supervisor.
package models

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

// ManifestSchema is the only manifest schema understood by this binary.
const ManifestSchema = 1

// maxManifestSize bounds a remote manifest. The real file is a few kilobytes.
const maxManifestSize = 4 << 20

//go:embed models.json
var defaultManifestJSON []byte

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Manifest is a versioned list of downloadable models.
type Manifest struct {
	Schema int     `json:"schema"`
	Models []Entry `json:"models"`
}

// Entry describes one model artifact.
type Entry struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Quantization string `json:"quantization"`
	Context      int    `json:"context"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	URL          string `json:"url"`
	License      string `json:"license"`
	LicenseURL   string `json:"license_url"`
	MinRAMBytes  int64  `json:"min_ram_bytes"`
}

// DefaultManifest returns the manifest embedded in the binary.
func DefaultManifest() (Manifest, error) {
	return ParseManifest(defaultManifestJSON)
}

// ParseManifest decodes and validates a manifest.
func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, core.Wrap(core.CodeConfigInvalid, err, "decode model manifest")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// LoadManifest loads a manifest from a local file or an HTTP(S) URL. An empty source returns the
// embedded manifest.
func LoadManifest(ctx context.Context, source string, client *http.Client) (Manifest, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return DefaultManifest()
	}

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		if client == nil {
			client = &http.Client{}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return Manifest{}, core.Wrap(core.CodeConfigInvalid, err, "build manifest request")
		}
		resp, err := client.Do(req)
		if err != nil {
			return Manifest{}, core.Wrap(core.CodeModelUnavailable, err, "download model manifest")
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return Manifest{}, core.E(core.CodeModelUnavailable,
				"model manifest returned HTTP %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestSize))
		if err != nil {
			return Manifest{}, core.Wrap(core.CodeModelUnavailable, err, "read model manifest")
		}
		return ParseManifest(data)
	}

	data, err := os.ReadFile(source)
	if err != nil {
		return Manifest{}, core.Wrap(core.CodeConfigInvalid, err, "read model manifest %s", source)
	}
	return ParseManifest(data)
}

// Validate checks every invariant in one pass.
func (m Manifest) Validate() error {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if m.Schema != ManifestSchema {
		add("schema must be %d, got %d", ManifestSchema, m.Schema)
	}
	if len(m.Models) == 0 {
		add("models must not be empty")
	}

	seen := make(map[string]struct{}, len(m.Models))
	for index, entry := range m.Models {
		if err := entry.Validate(); err != nil {
			add("models[%d]: %v", index, err)
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			add("duplicate model id %q", entry.ID)
		}
		seen[entry.ID] = struct{}{}
	}

	if len(problems) == 0 {
		return nil
	}
	return core.E(core.CodeConfigInvalid, "invalid model manifest: %s", strings.Join(problems, "; "))
}

// Validate checks one entry.
func (e Entry) Validate() error {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if !idPattern.MatchString(e.ID) {
		add("id %q must match %s", e.ID, idPattern)
	}
	if strings.TrimSpace(e.Name) == "" {
		add("name must not be empty")
	}
	if strings.TrimSpace(e.Quantization) == "" {
		add("quantization must not be empty")
	}
	if e.Context < 1024 {
		add("context must be at least 1024, got %d", e.Context)
	}
	if e.SizeBytes <= 0 {
		add("size_bytes must be positive, got %d", e.SizeBytes)
	}
	if !sha256Pattern.MatchString(e.SHA256) {
		add("sha256 must be 64 lowercase hexadecimal characters")
	}
	if err := validateURL(e.URL); err != nil {
		add("url %v", err)
	}
	if strings.TrimSpace(e.License) == "" {
		add("license must not be empty")
	}
	if err := validateURL(e.LicenseURL); err != nil {
		add("license_url %v", err)
	}
	if e.MinRAMBytes < 0 {
		add("min_ram_bytes must be zero or positive, got %d", e.MinRAMBytes)
	}

	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// Find returns the entry with the given id.
func (m Manifest) Find(id string) (Entry, bool) {
	for _, entry := range m.Models {
		if entry.ID == id {
			return entry, true
		}
	}
	return Entry{}, false
}

// FileName is the base name of the artifact URL.
func (e Entry) FileName() string {
	parsed, err := url.Parse(e.URL)
	if err != nil {
		return "model.gguf"
	}
	name := path.Base(parsed.Path)
	if name == "." || name == "/" || name == "" {
		return "model.gguf"
	}
	return name
}

// DownloadURL applies the optional corporate mirror. The mirror replaces the origin and keeps
// the artifact file name, so a mirror only has to host the same files.
func (e Entry) DownloadURL(mirror string) string {
	mirror = strings.TrimSpace(mirror)
	if mirror == "" {
		return e.URL
	}
	return strings.TrimRight(mirror, "/") + "/" + e.FileName()
}

func validateURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("is not a valid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("must use http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return errors.New("must include a host")
	}
	if parsed.User != nil {
		return errors.New("must not embed credentials")
	}
	return nil
}

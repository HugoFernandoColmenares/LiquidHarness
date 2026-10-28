// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/fsx"
)

const (
	// ModelFileName is the GGUF file inside every installed model directory.
	ModelFileName = "model.gguf"
	// ManifestFileName records the entry that produced an installed model.
	ManifestFileName = "manifest.json"
	// SHA256FileName records the expected digest next to the weights.
	SHA256FileName = "model.gguf.sha256"
	// LicenseFileName records the license that applies to the weights.
	LicenseFileName = "LICENSE"
	// CurrentFileName stores the id selected by `models use`.
	CurrentFileName = "current"

	minFreeSpaceMargin = 256 << 20
)

// Options configures a Manager. Dir is required.
type Options struct {
	Dir        string
	Manifest   Manifest
	Mirror     string
	HTTPClient *http.Client
	Progress   io.Writer
	FreeBytes  func(string) (uint64, error)
}

// Manager owns the model cache. It is safe for concurrent reads; Pull and Remove must not run
// concurrently for the same model.
type Manager struct {
	dir       string
	manifest  Manifest
	mirror    string
	client    *http.Client
	progress  io.Writer
	freeBytes func(string) (uint64, error)
}

// Model is one manifest entry plus its local state.
type Model struct {
	Entry
	Installed      bool   `json:"installed"`
	Current        bool   `json:"current"`
	Path           string `json:"path,omitempty"`
	InstalledBytes int64  `json:"installed_bytes,omitempty"`
}

// VerifyResult is the outcome of one checksum verification.
type VerifyResult struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	OK       bool   `json:"ok"`
	Expected string `json:"expected_sha256"`
	Actual   string `json:"actual_sha256,omitempty"`
	Size     int64  `json:"size_bytes,omitempty"`
	Error    string `json:"error,omitempty"`
}

// New validates the options and creates a manager.
func New(opts Options) (*Manager, error) {
	if strings.TrimSpace(opts.Dir) == "" {
		return nil, core.E(core.CodeConfigInvalid, "models directory must not be empty")
	}
	if err := opts.Manifest.Validate(); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, core.Wrap(core.CodeConfigInvalid, err, "resolve models directory")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = defaultHTTPClient()
	}
	if opts.FreeBytes == nil {
		opts.FreeBytes = fsx.FreeBytes
	}
	return &Manager{
		dir:       dir,
		manifest:  opts.Manifest,
		mirror:    opts.Mirror,
		client:    opts.HTTPClient,
		progress:  opts.Progress,
		freeBytes: opts.FreeBytes,
	}, nil
}

// Dir returns the absolute model cache directory.
func (m *Manager) Dir() string { return m.dir }

// Manifest returns the active manifest.
func (m *Manager) Manifest() Manifest { return m.manifest }

// List returns every manifest entry plus any installed model that is no longer advertised.
func (m *Manager) List() ([]Model, error) {
	current, err := m.Current()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(m.manifest.Models))
	out := make([]Model, 0, len(m.manifest.Models))
	for _, entry := range m.manifest.Models {
		model, err := m.describe(entry, current)
		if err != nil {
			return nil, err
		}
		seen[entry.ID] = struct{}{}
		out = append(out, model)
	}

	entries, err := os.ReadDir(m.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, core.Wrap(core.CodeInternal, err, "list models directory")
	}
	for _, dirEntry := range entries {
		if !dirEntry.IsDir() {
			continue
		}
		id := dirEntry.Name()
		if _, ok := seen[id]; ok {
			continue
		}
		entry, ok, err := m.readManifestEntry(id)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		model, err := m.describe(entry, current)
		if err != nil {
			return nil, err
		}
		out = append(out, model)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Installed returns one installed model.
func (m *Manager) Installed(id string) (Model, error) {
	entry, ok := m.manifest.Find(id)
	if !ok {
		var err error
		entry, ok, err = m.readManifestEntry(id)
		if err != nil {
			return Model{}, err
		}
		if !ok {
			return Model{}, core.E(core.CodeNotFound, "model %q is not installed", id)
		}
	}
	current, err := m.Current()
	if err != nil {
		return Model{}, err
	}
	model, err := m.describe(entry, current)
	if err != nil {
		return Model{}, err
	}
	if !model.Installed {
		return Model{}, core.E(core.CodeNotFound, "model %q is not installed", id)
	}
	return model, nil
}

// Current returns the id selected by `models use`, or an empty string.
func (m *Manager) Current() (string, error) {
	data, err := os.ReadFile(filepath.Join(m.dir, CurrentFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", core.Wrap(core.CodeInternal, err, "read current model")
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", nil
	}
	if !idPattern.MatchString(id) {
		return "", core.E(core.CodeConflict, "current model file contains an invalid id %q", id)
	}
	return id, nil
}

// Use selects an installed model as the default for autostart.
func (m *Manager) Use(id string) (Model, error) {
	model, err := m.Installed(id)
	if err != nil {
		return Model{}, err
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return Model{}, core.Wrap(core.CodeInternal, err, "create models directory")
	}
	if err := writeFileAtomic(filepath.Join(m.dir, CurrentFileName), []byte(id+"\n")); err != nil {
		return Model{}, err
	}
	model.Current = true
	return model, nil
}

// Remove deletes one installed model. The current model requires force.
func (m *Manager) Remove(id string, force bool) error {
	if !idPattern.MatchString(id) {
		return core.E(core.CodeNotFound, "model %q does not exist", id)
	}
	current, err := m.Current()
	if err != nil {
		return err
	}
	if current == id && !force {
		return core.E(core.CodeConflict, "model %q is current; pass --force to remove it", id)
	}

	target := filepath.Join(m.dir, id)
	if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
		return core.E(core.CodeNotFound, "model %q is not installed", id)
	} else if err != nil {
		return core.Wrap(core.CodeInternal, err, "inspect model %q", id)
	}

	if err := os.RemoveAll(target); err != nil {
		return core.Wrap(core.CodeInternal, err, "remove model %q", id)
	}
	if current == id {
		if err := os.Remove(filepath.Join(m.dir, CurrentFileName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return core.Wrap(core.CodeInternal, err, "clear current model")
		}
	}
	return nil
}

// Verify checks one installed model. A checksum mismatch is reported in the result, not as an
// error, so callers can render every failure at once.
func (m *Manager) Verify(ctx context.Context, id string) (VerifyResult, error) {
	if !idPattern.MatchString(id) {
		return VerifyResult{}, core.E(core.CodeNotFound, "model %q is not installed", id)
	}

	entry, ok, err := m.readManifestEntry(id)
	if err != nil {
		return VerifyResult{}, err
	}
	if !ok {
		entry, ok = m.manifest.Find(id)
	}
	if !ok {
		return VerifyResult{}, core.E(core.CodeNotFound, "model %q is not installed", id)
	}

	path := filepath.Join(m.dir, id, ModelFileName)
	result := VerifyResult{ID: id, Path: path, Expected: entry.SHA256}
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		result.Error = "model file is missing"
		return result, nil
	}
	if err != nil {
		return VerifyResult{}, core.Wrap(core.CodeInternal, err, "inspect model %q", id)
	}
	result.Size = info.Size()
	if entry.SizeBytes > 0 && info.Size() != entry.SizeBytes {
		result.Error = fmt.Sprintf("size mismatch: expected %d bytes, found %d", entry.SizeBytes, info.Size())
		return result, nil
	}

	actual, err := hashFile(ctx, path)
	if err != nil {
		if ctx.Err() != nil {
			return VerifyResult{}, core.Wrap(core.CodeCanceled, ctx.Err(), "verify model %q", id)
		}
		return VerifyResult{}, core.Wrap(core.CodeInternal, err, "hash model %q", id)
	}
	result.Actual = actual
	result.OK = actual == entry.SHA256
	if !result.OK {
		result.Error = "sha256 mismatch"
	}
	return result, nil
}

// VerifyAll verifies every installed model directory.
func (m *Manager) VerifyAll(ctx context.Context) ([]VerifyResult, error) {
	entries, err := os.ReadDir(m.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, core.Wrap(core.CodeInternal, err, "list models directory")
	}

	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !idPattern.MatchString(entry.Name()) {
			continue
		}
		if _, ok, err := m.readManifestEntry(entry.Name()); err != nil {
			return nil, err
		} else if !ok {
			if _, advertised := m.manifest.Find(entry.Name()); !advertised {
				continue
			}
		}
		ids = append(ids, entry.Name())
	}
	sort.Strings(ids)

	results := make([]VerifyResult, 0, len(ids))
	for _, id := range ids {
		result, err := m.Verify(ctx, id)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (m *Manager) describe(entry Entry, current string) (Model, error) {
	model := Model{Entry: entry, Current: entry.ID == current}
	path := filepath.Join(m.dir, entry.ID, ModelFileName)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return model, nil
	}
	if err != nil {
		return Model{}, core.Wrap(core.CodeInternal, err, "inspect model %q", entry.ID)
	}
	model.Installed = true
	model.Path = path
	model.InstalledBytes = info.Size()
	return model, nil
}

func (m *Manager) readManifestEntry(id string) (Entry, bool, error) {
	if !idPattern.MatchString(id) {
		return Entry{}, false, nil
	}
	data, err := os.ReadFile(filepath.Join(m.dir, id, ManifestFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, core.Wrap(core.CodeInternal, err, "read installed manifest for %q", id)
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return Entry{}, false, core.Wrap(core.CodeConflict, err,
			"installed manifest for %q is corrupt", id)
	}
	if entry.ID != id {
		return Entry{}, false, core.E(core.CodeConflict,
			"installed manifest for %q declares id %q", id, entry.ID)
	}
	if err := entry.Validate(); err != nil {
		return Entry{}, false, core.Wrap(core.CodeConflict, err,
			"installed manifest for %q is invalid", id)
	}
	return entry, true, nil
}

func (m *Manager) writeInstalledManifest(entry Entry) error {
	dir := filepath.Join(m.dir, entry.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return core.Wrap(core.CodeInternal, err, "create model directory")
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return core.Wrap(core.CodeInternal, err, "encode installed manifest")
	}
	data = append(data, '\n')
	if err := writeFileAtomic(filepath.Join(dir, ManifestFileName), data); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, SHA256FileName), []byte(entry.SHA256+"\n")); err != nil {
		return err
	}
	license := fmt.Sprintf("%s\n\nLicense: %s\n\nThese weights are not covered by the Apache-2.0 license of LiquidHarness.\n",
		entry.License, entry.LicenseURL)
	return writeFileAtomic(filepath.Join(dir, LicenseFileName), []byte(license))
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return core.Wrap(core.CodeInternal, err, "create temporary file")
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return core.Wrap(core.CodeInternal, err, "chmod temporary file")
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return core.Wrap(core.CodeInternal, err, "write temporary file")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return core.Wrap(core.CodeInternal, err, "sync temporary file")
	}
	if err := tmp.Close(); err != nil {
		return core.Wrap(core.CodeInternal, err, "close temporary file")
	}
	if err := replaceFile(tmpName, path); err != nil {
		return core.Wrap(core.CodeInternal, err, "replace %s", path)
	}
	return nil
}

// replaceFile renames tmpName over path. Unix renames replace the destination atomically;
// Windows does not, so the destination is removed and the rename retried.
func replaceFile(tmpName, path string) error {
	err := os.Rename(tmpName, path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrExist) && runtime.GOOS != "windows" {
		return err
	}
	if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
		return removeErr
	}
	return os.Rename(tmpName, path)
}

func defaultHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.IdleConnTimeout = 90 * time.Second
	return &http.Client{Transport: transport}
}

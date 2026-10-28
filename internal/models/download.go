// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

const (
	// PartSuffix marks an in-progress download.
	PartSuffix = ".part"
	// LockSuffix marks a model directory that another pull is using.
	LockSuffix = ".lock"

	downloadAttempts   = 3
	downloadBackoff    = 500 * time.Millisecond
	downloadMaxBackoff = 8 * time.Second
	progressInterval   = 2 * time.Second
	// staleLockAge is the age after which a lock left by a crashed process is ignored.
	staleLockAge = 24 * time.Hour
)

// PullOptions controls one download.
type PullOptions struct {
	// AcceptLicense acknowledges the model license. It is required before downloading.
	AcceptLicense bool
}

// Pull downloads, verifies and installs one model. Downloads resume from a .part file when the
// server supports Range requests. The final file only appears after its SHA-256 matches the
// manifest, so an interrupted pull never leaves a usable-but-corrupt model.
func (m *Manager) Pull(ctx context.Context, id string, opts PullOptions) (Model, error) {
	entry, ok := m.manifest.Find(id)
	if !ok {
		return Model{}, core.E(core.CodeNotFound, "model %q is not in the manifest", id)
	}

	current, err := m.Current()
	if err != nil {
		return Model{}, err
	}
	if installed, err := m.describe(entry, current); err != nil {
		return Model{}, err
	} else if installed.Installed {
		return installed, nil
	}

	if !opts.AcceptLicense {
		return Model{}, core.E(core.CodeUsage,
			"model %q is licensed under %s (%s); re-run with --accept-license to accept it",
			entry.ID, entry.License, entry.LicenseURL)
	}

	dir := filepath.Join(m.dir, entry.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Model{}, core.Wrap(core.CodeInternal, err, "create model directory")
	}
	release, err := m.lock(dir)
	if err != nil {
		return Model{}, err
	}
	defer release()

	partPath := filepath.Join(dir, ModelFileName+PartSuffix)
	if err := m.checkFreeSpace(dir, partPath, entry.SizeBytes); err != nil {
		return Model{}, err
	}
	if err := m.download(ctx, entry, partPath); err != nil {
		return Model{}, err
	}
	if err := m.verifyPart(ctx, entry, partPath); err != nil {
		return Model{}, err
	}

	// Write the metadata first: if the final rename fails, the next pull can recover and a
	// verification reports the missing weights instead of silently trusting a bare file.
	if err := m.writeInstalledManifest(entry); err != nil {
		return Model{}, err
	}
	target := filepath.Join(dir, ModelFileName)
	if err := os.Rename(partPath, target); err != nil {
		return Model{}, core.Wrap(core.CodeInternal, err, "install model %q", entry.ID)
	}
	m.reportProgress(entry.ID, entry.SizeBytes, entry.SizeBytes, 0, true)

	return m.describe(entry, current)
}

// lock creates an exclusive lock file for one model directory. A lock older than staleLockAge is
// treated as abandoned so a crashed process cannot block the cache forever.
func (m *Manager) lock(dir string) (func(), error) {
	path := filepath.Join(dir, LockSuffix)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, core.Wrap(core.CodeInternal, err, "lock model directory")
		}
		info, statErr := os.Stat(path)
		if statErr == nil && time.Since(info.ModTime()) > staleLockAge {
			if removeErr := os.Remove(path); removeErr == nil {
				return m.lock(dir)
			}
		}
		return nil, core.E(core.CodeConflict,
			"another pull is already running for this model (lock: %s)", path)
	}

	_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, core.Wrap(core.CodeInternal, err, "close model lock")
	}
	return func() { _ = os.Remove(path) }, nil
}

func (m *Manager) checkFreeSpace(dir, partPath string, size int64) error {
	if size <= 0 || m.freeBytes == nil {
		return nil
	}

	var existing int64
	if info, err := os.Stat(partPath); err == nil {
		existing = info.Size()
	}
	needed := size - existing + minFreeSpaceMargin
	if needed <= 0 {
		return nil
	}

	free, err := m.freeBytes(dir)
	if err == nil && free < uint64(needed) {
		return core.E(core.CodeModelUnavailable,
			"not enough disk space for model %q: need %s, have %s",
			filepath.Base(dir), humanBytes(needed), humanBytesU(free))
	}
	// Free space is a guard rail, not a requirement: an unsupported platform must not stop a
	// download that would otherwise work.
	return nil
}

func (m *Manager) download(ctx context.Context, entry Entry, partPath string) error {
	target := entry.DownloadURL(m.mirror)

	var offset int64
	if info, err := os.Stat(partPath); err == nil {
		offset = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return core.Wrap(core.CodeInternal, err, "inspect partial download")
	}
	if offset > entry.SizeBytes {
		if err := os.Remove(partPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return core.Wrap(core.CodeInternal, err, "reset partial download")
		}
		offset = 0
	}

	var lastErr error
	for attempt := 1; attempt <= downloadAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return core.Wrap(core.CodeCanceled, err, "download model %q", entry.ID)
		}

		size, err := m.fetch(ctx, entry, target, partPath, offset)
		offset = size
		if err == nil {
			if offset == entry.SizeBytes {
				return nil
			}
			err = fmt.Errorf("download ended at %d of %d bytes", offset, entry.SizeBytes)
		}
		lastErr = err

		if ctx.Err() != nil {
			return core.Wrap(core.CodeCanceled, ctx.Err(), "download model %q", entry.ID)
		}
		if !retryable(err) || attempt == downloadAttempts {
			break
		}
		if err := sleepContext(ctx, backoff(attempt)); err != nil {
			return core.Wrap(core.CodeCanceled, err, "download model %q", entry.ID)
		}
	}

	return core.Wrap(core.CodeModelUnavailable, lastErr,
		"download model %q from %s", entry.ID, target)
}

func (m *Manager) fetch(ctx context.Context, entry Entry, target, partPath string, offset int64) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return offset, core.Wrap(core.CodeConfigInvalid, err, "build download request")
	}
	req.Header.Set("User-Agent", "liquidharness/model-manager")
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return offset, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		// Append to the partial file.
	case http.StatusOK:
		// The server ignored Range or the request started at zero: restart cleanly.
		offset = 0
	case http.StatusRequestedRangeNotSatisfiable:
		if info, statErr := os.Stat(partPath); statErr == nil && info.Size() == entry.SizeBytes {
			return info.Size(), nil
		}
		if err := os.Remove(partPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return offset, err
		}
		return 0, markRetryable(errors.New("server rejected the resume range; restarting download"))
	default:
		err := fmt.Errorf("server returned HTTP %d", resp.StatusCode)
		if retryableStatus(resp.StatusCode) {
			return offset, markRetryable(err)
		}
		return offset, err
	}

	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if offset == 0 {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	file, err := os.OpenFile(partPath, flags, 0o600)
	if err != nil {
		return offset, core.Wrap(core.CodeInternal, err, "open partial download")
	}

	writer := &progressWriter{
		writer:  file,
		id:      entry.ID,
		total:   entry.SizeBytes,
		done:    offset,
		started: time.Now(),
		report:  m.reportProgress,
	}
	copied, copyErr := io.Copy(writer, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return offset + copied, copyErr
	}
	if closeErr != nil {
		return offset + copied, closeErr
	}
	return offset + copied, nil
}

func (m *Manager) verifyPart(ctx context.Context, entry Entry, partPath string) error {
	info, err := os.Stat(partPath)
	if err != nil {
		return core.Wrap(core.CodeInternal, err, "inspect downloaded model")
	}
	if info.Size() != entry.SizeBytes {
		if info.Size() > entry.SizeBytes {
			// A file larger than the manifest cannot be resumed safely.
			_ = os.Remove(partPath)
		}
		return core.E(core.CodeConflict,
			"model %q has %d bytes, expected %d", entry.ID, info.Size(), entry.SizeBytes)
	}

	actual, err := hashFile(ctx, partPath)
	if err != nil {
		if ctx.Err() != nil {
			return core.Wrap(core.CodeCanceled, ctx.Err(), "verify model %q", entry.ID)
		}
		return core.Wrap(core.CodeInternal, err, "hash model %q", entry.ID)
	}
	if actual != entry.SHA256 {
		// A corrupt download is not usable and must not be resumed: delete it so the next pull
		// starts from zero.
		_ = os.Remove(partPath)
		return core.E(core.CodeConflict,
			"model %q failed verification: expected sha256 %s, got %s",
			entry.ID, entry.SHA256, actual)
	}
	return nil
}

func (m *Manager) reportProgress(id string, done, total int64, elapsed time.Duration, final bool) {
	if m.progress == nil {
		return
	}
	if total <= 0 {
		total = 1
	}
	percent := float64(done) / float64(total) * 100
	line := fmt.Sprintf("%s: %s / %s (%.1f%%)", id, humanBytes(done), humanBytes(total), percent)
	if elapsed > 0 && done > 0 && done < total {
		speed := float64(done) / elapsed.Seconds()
		if speed > 0 {
			eta := time.Duration(float64(total-done)/speed) * time.Second
			line += fmt.Sprintf(" %s/s ETA %s", humanBytesU(uint64(speed)), eta.Round(time.Second))
		}
	}
	if final {
		_, _ = fmt.Fprintf(m.progress, "\r%s\n", line)
		return
	}
	_, _ = fmt.Fprintf(m.progress, "\r%s", line)
}

type progressWriter struct {
	writer  io.Writer
	id      string
	total   int64
	done    int64
	started time.Time
	report  func(string, int64, int64, time.Duration, bool)
	last    time.Time
}

func (w *progressWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	w.done += int64(written)
	if w.report != nil && (w.last.IsZero() || time.Since(w.last) >= progressInterval) {
		w.last = time.Now()
		w.report(w.id, w.done, w.total, time.Since(w.started), false)
	}
	return written, err
}

// retryableError marks a failure that another attempt may fix.
type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

func markRetryable(err error) error { return &retryableError{err: err} }

func retryable(err error) bool {
	var marked *retryableError
	if errors.As(err, &marked) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func backoff(attempt int) time.Duration {
	delay := downloadBackoff << (attempt - 1)
	if delay > downloadMaxBackoff {
		delay = downloadMaxBackoff
	}
	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func hashFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, &contextReader{ctx: ctx, reader: file}); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func humanBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTP"[exp])
}

func humanBytesU(size uint64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := uint64(unit), 0
	for n := size / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTP"[exp])
}

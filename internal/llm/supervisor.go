// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

const (
	defaultStartupTimeout  = 120 * time.Second
	defaultShutdownTimeout = 10 * time.Second
	defaultHealthInterval  = 250 * time.Millisecond
	defaultHealthTimeout   = 2 * time.Second
	maxSupervisorOutput    = 4 << 10
)

// SupervisorConfig describes one managed llama-server process. Every field has a safe default
// except ModelPath, which is required.
type SupervisorConfig struct {
	// Binary is the llama-server executable. A bare name is resolved with exec.LookPath; a path
	// is used as-is. Empty means "llama-server".
	Binary string
	// ModelPath is the GGUF file served by the process.
	ModelPath string
	// Addr is the listen address, for example 127.0.0.1:8080.
	Addr string
	// Alias is the model name advertised through /v1/models. It should match model.name.
	Alias string
	// ContextWindow is passed as --ctx-size when positive.
	ContextWindow int
	// Threads is passed as --threads when positive.
	Threads int
	// ArgsPrefix is inserted before the standard llama-server flags. It exists for wrappers
	// such as nice or taskset and for the test helper process.
	ArgsPrefix []string
	// ExtraArgs are appended after the standard flags.
	ExtraArgs []string
	// Env is appended to the inherited process environment.
	Env []string
	// StartupTimeout bounds WaitHealthy.
	StartupTimeout time.Duration
	// ShutdownTimeout bounds Stop.
	ShutdownTimeout time.Duration
	// HealthInterval is the delay between health probes.
	HealthInterval time.Duration
	// HTTPClient is used for health probes. It must not have a global timeout shorter than one
	// probe; the supervisor applies its own per-probe deadline.
	HTTPClient *http.Client
	// Stdout and Stderr receive the process output. Stderr is also kept in a small tail buffer
	// so startup failures can explain what went wrong.
	Stdout io.Writer
	Stderr io.Writer
}

// Supervisor owns the lifecycle of one llama-server child process. It is safe for concurrent
// use, but a caller should serialize Start and Stop.
type Supervisor struct {
	cfg    SupervisorConfig
	client *http.Client

	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
	tail    *tailWriter
}

// NewSupervisor validates the configuration. It does not start anything.
func NewSupervisor(cfg SupervisorConfig) (*Supervisor, error) {
	if strings.TrimSpace(cfg.ModelPath) == "" {
		return nil, core.E(core.CodeConfigInvalid, "model.path must be set to autostart llama-server")
	}
	addr, err := normalizeAddr(cfg.Addr)
	if err != nil {
		return nil, err
	}
	cfg.Addr = addr
	if cfg.Binary == "" {
		cfg.Binary = "llama-server"
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = defaultStartupTimeout
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}
	if cfg.HealthInterval <= 0 {
		cfg.HealthInterval = defaultHealthInterval
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{}
	}

	return &Supervisor{cfg: cfg, client: cfg.HTTPClient, tail: newTailWriter(maxSupervisorOutput)}, nil
}

// BaseURL returns the versioned OpenAI-compatible root of the managed server.
func (s *Supervisor) BaseURL() string {
	return "http://" + s.cfg.Addr + "/v1"
}

// Addr returns the listen address.
func (s *Supervisor) Addr() string { return s.cfg.Addr }

// PID returns the process id, or zero when the supervisor has not started a process.
func (s *Supervisor) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// Running reports whether the supervised process is still alive. A process that exited is
// reported as not running even if Stop has not been called yet.
func (s *Supervisor) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runningLocked()
}

func (s *Supervisor) runningLocked() bool {
	if s.cmd == nil || s.cmd.Process == nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// Start launches the process. It returns immediately; use StartAndWait to wait for readiness.
func (s *Supervisor) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return core.Wrap(core.CodeCanceled, err, "start llama-server")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.runningLocked() {
		return nil
	}

	binary, err := resolveBinary(s.cfg.Binary)
	if err != nil {
		return err
	}
	modelPath, err := filepath.Abs(s.cfg.ModelPath)
	if err != nil {
		return core.Wrap(core.CodeConfigInvalid, err, "resolve model.path")
	}
	info, err := os.Stat(modelPath)
	if err != nil {
		return core.Wrap(core.CodeConfigInvalid, err, "model.path %s is not readable", modelPath)
	}
	if info.IsDir() {
		return core.E(core.CodeConfigInvalid, "model.path %s is a directory, not a GGUF file", modelPath)
	}

	args := s.args(modelPath)
	// The process is owned by the supervisor and stopped explicitly; binding it to ctx would
	// kill llama-server as soon as the startup context is canceled.
	//nolint:gosec // the binary is resolved from configuration and checked before use.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), binary, args...)
	cmd.Env = append(os.Environ(), s.cfg.Env...)
	cmd.Stdout = s.cfg.Stdout
	if s.cfg.Stderr != nil {
		cmd.Stderr = io.MultiWriter(s.cfg.Stderr, s.tail)
	} else {
		cmd.Stderr = s.tail
	}

	if err := cmd.Start(); err != nil {
		return core.Wrap(core.CodeModelUnavailable, err, "start llama-server")
	}

	done := make(chan struct{})
	s.cmd = cmd
	s.done = done
	s.waitErr = nil
	s.tail.Reset()
	go func() {
		waitErr := cmd.Wait()
		s.mu.Lock()
		if s.cmd == cmd {
			s.waitErr = waitErr
		}
		s.mu.Unlock()
		close(done)
	}()

	return nil
}

// StartAndWait starts the process and waits until it answers a health probe. If the process
// exits or never becomes healthy, it is stopped before the error is returned.
func (s *Supervisor) StartAndWait(ctx context.Context) error {
	if err := s.Start(ctx); err != nil {
		return err
	}
	if err := s.WaitHealthy(ctx); err != nil {
		_ = s.Stop(context.WithoutCancel(ctx))
		return err
	}
	return nil
}

// WaitHealthy polls the server until it is ready. A 503 is treated as "still loading"; a 404
// falls back to /v1/models for backends that do not expose /health.
func (s *Supervisor) WaitHealthy(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, s.cfg.StartupTimeout)
	defer cancel()

	ticker := time.NewTicker(s.cfg.HealthInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		if err := s.processError(); err != nil {
			return err
		}
		if err := s.probe(probeCtx); err == nil {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-probeCtx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return core.Wrap(core.CodeCanceled, ctx.Err(), "wait for llama-server")
			}
			return s.startupError(lastErr)
		case <-ticker.C:
		}
	}
}

// Stop terminates the process and waits for it to exit. It is idempotent.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.cmd == nil || s.cmd.Process == nil {
		s.mu.Unlock()
		return nil
	}
	cmd := s.cmd
	done := s.done
	s.mu.Unlock()

	if err := cmd.Process.Signal(os.Interrupt); err == nil {
		select {
		case <-done:
			s.clear(cmd)
			return nil
		case <-time.After(s.cfg.ShutdownTimeout):
			// Fall through to the hard kill.
		case <-ctx.Done():
		}
	}

	killErr := cmd.Process.Kill()
	select {
	case <-done:
		s.clear(cmd)
		if killErr != nil {
			return core.Wrap(core.CodeInternal, killErr, "kill llama-server")
		}
		return nil
	case <-time.After(s.cfg.ShutdownTimeout):
		return core.E(core.CodeInternal, "llama-server did not stop within %s", s.cfg.ShutdownTimeout)
	}
}

func (s *Supervisor) clear(cmd *exec.Cmd) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == cmd {
		s.cmd = nil
		s.done = nil
		s.waitErr = nil
	}
}

func (s *Supervisor) args(modelPath string) []string {
	host, port, _ := net.SplitHostPort(s.cfg.Addr)
	args := make([]string, 0, len(s.cfg.ArgsPrefix)+len(s.cfg.ExtraArgs)+12)
	args = append(args, s.cfg.ArgsPrefix...)
	args = append(args, "--model", modelPath, "--host", host, "--port", port, "--jinja")
	if s.cfg.ContextWindow > 0 {
		args = append(args, "--ctx-size", strconv.Itoa(s.cfg.ContextWindow))
	}
	if s.cfg.Threads > 0 {
		args = append(args, "--threads", strconv.Itoa(s.cfg.Threads))
	}
	if s.cfg.Alias != "" {
		args = append(args, "--alias", s.cfg.Alias)
	}
	args = append(args, s.cfg.ExtraArgs...)
	return args
}

func (s *Supervisor) probe(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, defaultHealthTimeout)
	defer cancel()

	err := probeURL(probeCtx, s.client, "http://"+s.cfg.Addr+"/health")
	if err == nil {
		return nil
	}
	if !errors.Is(err, errProbeNotFound) {
		return err
	}
	return probeURL(probeCtx, s.client, "http://"+s.cfg.Addr+"/v1/models")
}

func (s *Supervisor) processError() error {
	s.mu.Lock()
	cmd := s.cmd
	done := s.done
	if cmd == nil || done == nil {
		s.mu.Unlock()
		return nil
	}

	select {
	case <-done:
		waitErr := s.waitErr
		s.cmd = nil
		s.done = nil
		s.waitErr = nil
		s.mu.Unlock()
		if waitErr == nil {
			return core.E(core.CodeModelUnavailable, "llama-server exited before becoming healthy")
		}
		return core.Wrap(core.CodeModelUnavailable, waitErr,
			"llama-server exited before becoming healthy%s", s.tailSuffix())
	default:
		s.mu.Unlock()
		return nil
	}
}

func (s *Supervisor) startupError(cause error) error {
	coded := core.E(core.CodeModelUnavailable,
		"llama-server did not become healthy within %s%s", s.cfg.StartupTimeout, s.tailSuffix())
	coded.Retry = true
	if cause != nil {
		coded.Err = cause
	}
	return coded
}

func (s *Supervisor) tailSuffix() string {
	tail := strings.TrimSpace(s.tail.String())
	if tail == "" {
		return ""
	}
	return ": " + tail
}

// ProbeHealth reports whether an OpenAI-compatible backend at baseURL is ready. It accepts a
// llama.cpp /health endpoint or a /v1/models endpoint. A nil client uses a short-lived default.
func ProbeHealth(ctx context.Context, baseURL string, client *http.Client) error {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return core.Wrap(core.CodeConfigInvalid, err, "parse model base URL")
	}
	if parsed.Host == "" {
		return core.E(core.CodeConfigInvalid, "model base URL must include a host")
	}
	root := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")

	if client == nil {
		client = &http.Client{}
	}
	probeCtx, cancel := context.WithTimeout(ctx, defaultHealthTimeout)
	defer cancel()

	err = probeURL(probeCtx, client, root+"/health")
	if err == nil {
		return nil
	}
	if !errors.Is(err, errProbeNotFound) {
		return err
	}
	return probeURL(probeCtx, client, root+"/v1/models")
}

var errProbeNotFound = errors.New("health endpoint not found")

func probeURL(ctx context.Context, client *http.Client, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))

	switch {
	case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return errProbeNotFound
	case resp.StatusCode == http.StatusServiceUnavailable:
		return fmt.Errorf("backend is starting (HTTP %d)", resp.StatusCode)
	default:
		return fmt.Errorf("backend returned HTTP %d", resp.StatusCode)
	}
}

func resolveBinary(binary string) (string, error) {
	if strings.ContainsAny(binary, `/\`) {
		info, err := os.Stat(binary)
		if err != nil {
			return "", core.Wrap(core.CodeConfigInvalid, err, "llama-server binary %s is not readable", binary)
		}
		if info.IsDir() {
			return "", core.E(core.CodeConfigInvalid, "llama-server binary %s is a directory", binary)
		}
		return binary, nil
	}

	resolved, err := exec.LookPath(binary)
	if err != nil {
		return "", core.Wrap(core.CodeConfigInvalid, err,
			"%s was not found in PATH; install llama.cpp or set model.server_path", binary)
	}
	return resolved, nil
}

func normalizeAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", core.Wrap(core.CodeConfigInvalid, err, "model address %q must be host:port", addr)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", core.E(core.CodeConfigInvalid, "model address %q has an invalid port", addr)
	}
	return net.JoinHostPort(host, port), nil
}

type tailWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func newTailWriter(max int) *tailWriter {
	return &tailWriter{max: max}
}

// Write implements io.Writer.
func (w *tailWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, _ = w.buf.Write(data)
	if w.buf.Len() > w.max {
		trimmed := w.buf.Bytes()[w.buf.Len()-w.max:]
		remaining := append([]byte(nil), trimmed...)
		w.buf.Reset()
		_, _ = w.buf.Write(remaining)
	}
	return len(data), nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *tailWriter) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Reset()
}

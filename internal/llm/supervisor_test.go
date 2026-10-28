// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

func TestSupervisorStartsAndStops(t *testing.T) {
	addr := freeAddr(t)
	model := writeModelFile(t)
	var stderr bytes.Buffer

	supervisor := newTestSupervisor(t, addr, model, &stderr, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := supervisor.StartAndWait(ctx); err != nil {
		t.Fatalf("start: %v (stderr: %s)", err, stderr.String())
	}
	if !supervisor.Running() {
		t.Fatal("supervisor is not running after StartAndWait")
	}
	if supervisor.PID() == 0 {
		t.Error("expected a process id")
	}
	if err := ProbeHealth(ctx, supervisor.BaseURL(), nil); err != nil {
		t.Errorf("probe health: %v", err)
	}

	if err := supervisor.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if supervisor.Running() {
		t.Error("supervisor is still running after Stop")
	}
}

func TestSupervisorReportsStartupFailure(t *testing.T) {
	addr := freeAddr(t)
	model := writeModelFile(t)
	var stderr bytes.Buffer

	supervisor := newTestSupervisor(t, addr, model, &stderr, []string{"GO_HELPER_FAIL=1"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := supervisor.StartAndWait(ctx)
	if err == nil {
		t.Fatal("expected a startup error")
	}
	if !core.Is(err, core.CodeModelUnavailable) {
		t.Errorf("error = %v, want MODEL_UNAVAILABLE", err)
	}
	if !strings.Contains(err.Error(), "failed to load model") {
		t.Errorf("error does not include the process output: %v", err)
	}
	if supervisor.Running() {
		t.Error("supervisor is still running after a failed start")
	}
}

func TestSupervisorTimesOutWhenUnhealthy(t *testing.T) {
	addr := freeAddr(t)
	model := writeModelFile(t)

	supervisor := newTestSupervisor(t, addr, model, nil, []string{"GO_HELPER_UNHEALTHY=1"})
	supervisor.cfg.StartupTimeout = 300 * time.Millisecond
	supervisor.cfg.HealthInterval = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := supervisor.StartAndWait(ctx)
	if err == nil {
		t.Fatal("expected a timeout")
	}
	if !core.Is(err, core.CodeModelUnavailable) {
		t.Errorf("error = %v, want MODEL_UNAVAILABLE", err)
	}
	if !strings.Contains(err.Error(), "did not become healthy") {
		t.Errorf("error = %v", err)
	}
	if supervisor.Running() {
		t.Error("supervisor is still running after a failed start")
	}
}

func TestSupervisorRejectsMissingModel(t *testing.T) {
	addr := freeAddr(t)
	supervisor, err := NewSupervisor(SupervisorConfig{
		Binary:    os.Args[0],
		ModelPath: filepath.Join(t.TempDir(), "missing.gguf"),
		Addr:      addr,
	})
	if err != nil {
		t.Fatalf("new supervisor: %v", err)
	}

	err = supervisor.Start(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !core.Is(err, core.CodeConfigInvalid) {
		t.Errorf("error = %v, want CONFIG_INVALID", err)
	}
}

func TestProbeHealthFallbacks(t *testing.T) {
	tests := map[string]http.Handler{
		"health": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, `{"status":"ok"}`)
		}),
		"models": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, `{"data":[]}`)
		}),
	}

	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()

			if err := ProbeHealth(context.Background(), server.URL+"/v1", server.Client()); err != nil {
				t.Errorf("probe health: %v", err)
			}
		})
	}
}

func TestProbeHealthReportsUnhealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := ProbeHealth(context.Background(), server.URL+"/v1", server.Client())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "starting") {
		t.Errorf("error = %v", err)
	}
}

func TestNewSupervisorRejectsInvalidAddr(t *testing.T) {
	_, err := NewSupervisor(SupervisorConfig{
		ModelPath: "model.gguf",
		Addr:      "not-an-address",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !core.Is(err, core.CodeConfigInvalid) {
		t.Errorf("error = %v, want CONFIG_INVALID", err)
	}
}

func newTestSupervisor(
	t *testing.T,
	addr, model string,
	stderr io.Writer,
	extraEnv []string,
) *Supervisor {
	t.Helper()

	env := append([]string{"GO_WANT_HELPER_PROCESS=1"}, extraEnv...)
	supervisor, err := NewSupervisor(SupervisorConfig{
		Binary:          os.Args[0],
		ModelPath:       model,
		Addr:            addr,
		Alias:           "test",
		ContextWindow:   2048,
		ArgsPrefix:      []string{"-test.run=TestHelperProcess", "--"},
		Env:             env,
		StartupTimeout:  5 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		HealthInterval:  20 * time.Millisecond,
		Stderr:          stderr,
	})
	if err != nil {
		t.Fatalf("new supervisor: %v", err)
	}
	t.Cleanup(func() { _ = supervisor.Stop(context.Background()) })
	return supervisor
}

func writeModelFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, []byte("fake gguf"), 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
	return path
}

func freeAddr(t *testing.T) string {
	t.Helper()
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

// TestHelperProcess is not a real test: it is the body of the fake llama-server process used by
// the supervisor tests. It only runs when GO_WANT_HELPER_PROCESS=1.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	if os.Getenv("GO_HELPER_FAIL") == "1" {
		_, _ = fmt.Fprintln(os.Stderr, "fake llama-server failed to load model")
		os.Exit(2)
	}

	host, port := "127.0.0.1", "8080"
	args := helperArgs(os.Args)
	for index := 0; index+1 < len(args); index++ {
		switch args[index] {
		case "--host":
			host = args[index+1]
		case "--port":
			port = args[index+1]
		}
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", net.JoinHostPort(host, port))
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		if os.Getenv("GO_HELPER_UNHEALTHY") == "1" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"test"}]}`)
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: time.Second,
	}
	_ = server.Serve(listener)
	os.Exit(0)
}

func helperArgs(args []string) []string {
	for index, arg := range args {
		if arg == "--" {
			return args[index+1:]
		}
	}
	return args
}

// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/llm"
)

const (
	probeTimeout    = 750 * time.Millisecond
	startupTimeout  = 2 * time.Minute
	shutdownTimeout = 10 * time.Second
)

// ensureModel starts llama-server when autostart is enabled, a model path is configured and the
// endpoint is not already healthy. The returned cleanup stops the process only when this call
// started it; an external server is never touched.
func (a *App) ensureModel(ctx context.Context) (func(), error) {
	noop := func() {}

	if !a.configured || a.customModel || !a.Config.Model.Autostart {
		return noop, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, core.Wrap(core.CodeCanceled, err, "start model")
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	err := llm.ProbeHealth(probeCtx, a.Config.Model.BaseURL, nil)
	cancel()
	if err == nil {
		return noop, nil
	}
	if ctx.Err() != nil {
		return nil, core.Wrap(core.CodeCanceled, ctx.Err(), "start model")
	}

	modelPath := strings.TrimSpace(a.Config.Model.Path)
	if modelPath == "" {
		selected, selectErr := a.currentModelPath()
		if selectErr != nil {
			if core.Is(selectErr, core.CodeNotFound) {
				// No local model selected: leave the endpoint alone so the model call reports
				// MODEL_UNAVAILABLE with the connection error.
				return noop, nil
			}
			return nil, selectErr
		}
		modelPath = selected
	}

	parsed, parseErr := url.Parse(a.Config.Model.BaseURL)
	if parseErr != nil || parsed.Scheme != "http" || !isLoopbackHost(parsed.Hostname()) {
		return noop, nil
	}

	a.Logger.Info("starting llama-server", "model", modelPath, "addr", parsed.Host)
	supervisor, err := llm.NewSupervisor(llm.SupervisorConfig{
		Binary:          a.Config.Model.ServerPath,
		ModelPath:       modelPath,
		Addr:            parsed.Host,
		Alias:           a.Config.Model.Name,
		ContextWindow:   a.Config.Model.ContextWindow,
		StartupTimeout:  startupTimeout,
		ShutdownTimeout: shutdownTimeout,
		Stderr:          a.Stderr,
	})
	if err != nil {
		return nil, err
	}

	startCtx, startCancel := context.WithTimeout(ctx, startupTimeout)
	defer startCancel()
	if err := supervisor.StartAndWait(startCtx); err != nil {
		return nil, err
	}
	a.Logger.Info("llama-server is ready", "pid", supervisor.PID())

	return func() {
		// The caller's context is usually done by the time cleanup runs, so keep its values but
		// detach cancellation.
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer stopCancel()
		if err := supervisor.Stop(stopCtx); err != nil && !errors.Is(err, context.Canceled) {
			a.Logger.Warn("could not stop llama-server", "error", err)
		}
	}, nil
}

// isLoopbackHost reports whether autostart can bind to this host. A remote OpenAI-compatible
// endpoint is used as-is and must never be replaced by a local process.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

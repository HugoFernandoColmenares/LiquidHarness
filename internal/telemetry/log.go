// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Package telemetry configures structured logging and provides the redaction helpers used
// before any value reaches a log, an audit record or a bug report.
package telemetry

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Options configures the process logger.
type Options struct {
	Level     string
	Format    string
	Output    io.Writer
	AddSource bool
}

// New builds a slog logger. Logs always go to the provided writer (stderr in the CLI) so that
// stdout stays clean for --json output.
func New(opts Options) (*slog.Logger, error) {
	level, err := parseLevel(opts.Level)
	if err != nil {
		return nil, err
	}
	output := opts.Output
	if output == nil {
		output = io.Discard
	}

	handlerOptions := &slog.HandlerOptions{
		Level:       level,
		AddSource:   opts.AddSource,
		ReplaceAttr: redactAttr,
	}

	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(opts.Format)) {
	case "", "text":
		handler = slog.NewTextHandler(output, handlerOptions)
	case "json":
		handler = slog.NewJSONHandler(output, handlerOptions)
	default:
		return nil, fmt.Errorf("invalid log format %q: expected text or json", opts.Format)
	}
	return slog.New(handler), nil
}

func parseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q: expected debug, info, warn or error", value)
	}
}

// redactAttr masks sensitive keys and redacts secrets embedded in string values.
func redactAttr(_ []string, attr slog.Attr) slog.Attr {
	if isSensitiveKey(attr.Key) {
		attr.Value = slog.StringValue("***")
		return attr
	}
	if attr.Value.Kind() == slog.KindString {
		attr.Value = slog.StringValue(Redact(attr.Value.String()))
	}
	return attr
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	for _, marker := range []string{"password", "secret", "token", "api_key", "apikey", "authorization", "credential"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

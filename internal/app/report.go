// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

// Envelope is the stable JSON contract for every command. stdout always contains exactly one
// JSON object; logs go to stderr.
type Envelope struct {
	SchemaVersion int           `json:"schema_version"`
	OK            bool          `json:"ok"`
	Data          any           `json:"data,omitempty"`
	Error         *ErrorPayload `json:"error,omitempty"`
}

// ErrorPayload is the machine-readable form of an error.
type ErrorPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	ExitCode  int    `json:"exit_code"`
	Retryable bool   `json:"retryable"`
}

// PrintJSONData writes a success or failure envelope to stdout.
func (a *App) PrintJSONData(ok bool, data any) error {
	envelope := Envelope{SchemaVersion: 1, OK: ok, Data: data}
	return writeEnvelope(a.Stdout, envelope)
}

// PrintJSON writes a successful envelope to stdout.
func (a *App) PrintJSON(data any) error { return a.PrintJSONData(true, data) }

// PrintJSONReport writes a report that can itself be a failure (for example `doctor`). The data
// is always included; on failure an error payload is attached so the contract is preserved.
func (a *App) PrintJSONReport(ok bool, data any, err error) error {
	envelope := Envelope{SchemaVersion: 1, OK: ok, Data: data}
	if !ok {
		envelope.Error = ErrorPayloadFor(err)
	}
	return writeEnvelope(a.Stdout, envelope)
}

// ReportError renders an error once: as a JSON envelope on stdout in --json mode, or as a
// human-readable line on stderr. Errors marked silent are skipped because the command already
// printed a report.
func (a *App) ReportError(err error) {
	if err == nil {
		return
	}
	var coded *core.Error
	if errors.As(err, &coded) && coded.Silent {
		return
	}

	if a.JSON {
		envelope := Envelope{SchemaVersion: 1, OK: false, Error: ErrorPayloadFor(err)}
		if writeErr := writeEnvelope(a.Stdout, envelope); writeErr != nil {
			_, _ = fmt.Fprintf(a.Stderr, "liquidharness: %v\n", err)
		}
		return
	}

	_, _ = fmt.Fprintf(a.Stderr, "liquidharness: %v\n", err)
	if core.Is(err, core.CodeUsage) {
		_, _ = fmt.Fprintln(a.Stderr, "run 'liquidharness --help' for usage")
	}
}

// ErrorPayloadFor maps any error to the stable error contract.
func ErrorPayloadFor(err error) *ErrorPayload {
	if err == nil {
		return nil
	}
	var coded *core.Error
	if errors.As(err, &coded) {
		message := coded.UserMessage()
		if coded.Err != nil {
			message = message + ": " + coded.Err.Error()
		}
		return &ErrorPayload{
			Code:      string(coded.Kind),
			Message:   message,
			ExitCode:  coded.ExitCode(),
			Retryable: coded.Retryable(),
		}
	}
	if errors.Is(err, context.Canceled) {
		return &ErrorPayload{
			Code:     string(core.CodeCanceled),
			Message:  "operation canceled",
			ExitCode: core.ExitCodeFor(core.CodeCanceled),
		}
	}
	return &ErrorPayload{
		Code:     string(core.CodeInternal),
		Message:  err.Error(),
		ExitCode: core.ExitCodeFor(core.CodeInternal),
	}
}

func writeEnvelope(w io.Writer, envelope Envelope) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope)
}

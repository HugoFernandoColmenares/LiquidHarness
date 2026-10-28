// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"
	"fmt"
)

// Code is a stable, machine-readable error category. Codes are part of the CLI contract and
// must not change between minor releases.
type Code string

const (
	// CodeInternal is an unexpected failure. Exit code 1.
	CodeInternal Code = "INTERNAL"
	// CodeUsage is an invalid invocation (unknown flag, bad arguments). Exit code 2.
	CodeUsage Code = "USAGE"
	// CodePolicyDenied means the policy engine refused an action. Exit code 3.
	CodePolicyDenied Code = "POLICY_DENIED"
	// CodeModelUnavailable means the model backend could not be reached. Exit code 4.
	CodeModelUnavailable Code = "MODEL_UNAVAILABLE"
	// CodeToolFailed means a tool failed while executing. Exit code 5.
	CodeToolFailed Code = "TOOL_FAILED"
	// CodeConfigInvalid means the configuration is missing or invalid. Exit code 6.
	CodeConfigInvalid Code = "CONFIG_INVALID"
	// CodeCanceled means the user or a signal canceled the operation. Exit code 7.
	CodeCanceled Code = "CANCELED"
	// CodeNotFound means a session, model or resource does not exist. Exit code 8.
	CodeNotFound Code = "NOT_FOUND"
	// CodeConflict means state changed underneath the operation. Exit code 9.
	CodeConflict Code = "CONFLICT"
	// CodeContextOverflow means the context window cannot be recovered. Exit code 10.
	CodeContextOverflow Code = "CONTEXT_OVERFLOW"
)

// exitCodes maps every code to the documented process exit code.
var exitCodes = map[Code]int{
	CodeInternal:         1,
	CodeUsage:            2,
	CodePolicyDenied:     3,
	CodeModelUnavailable: 4,
	CodeToolFailed:       5,
	CodeConfigInvalid:    6,
	CodeCanceled:         7,
	CodeNotFound:         8,
	CodeConflict:         9,
	CodeContextOverflow:  10,
}

// Error is the canonical error type of the harness. It carries a stable code, a message safe to
// show to the user, the wrapped cause (if any) and whether retrying may help.
type Error struct {
	Kind    Code
	Message string
	Err     error
	Retry   bool
	// Silent suppresses the default CLI rendering because the command already printed a report.
	Silent bool
}

// E builds a coded error.
func E(kind Code, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// Wrap builds a coded error around a cause.
func Wrap(kind Code, err error, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...), Err: err}
}

// Silent builds a coded error that the CLI must not print again.
func Silent(kind Code, format string, args ...any) *Error {
	e := E(kind, format, args...)
	e.Silent = true
	return e
}

// Error implements error.
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// Unwrap returns the wrapped cause.
func (e *Error) Unwrap() error { return e.Err }

// Code returns the stable error code.
func (e *Error) Code() string { return string(e.Kind) }

// ExitCode returns the process exit code for this error.
func (e *Error) ExitCode() int { return ExitCodeFor(e.Kind) }

// Retryable reports whether retrying may succeed.
func (e *Error) Retryable() bool { return e.Retry }

// UserMessage returns the message intended for humans.
func (e *Error) UserMessage() string { return e.Message }

// ExitCodeFor maps a code to its documented exit code, defaulting to 1.
func ExitCodeFor(kind Code) int {
	if code, ok := exitCodes[kind]; ok {
		return code
	}
	return 1
}

// ExitCode maps an error to the process exit code. Non-coded errors exit with 1.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var coded *Error
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 1
}

// Is reports whether err carries the given code.
func Is(err error, kind Code) bool {
	var coded *Error
	return errors.As(err, &coded) && coded.Kind == kind
}

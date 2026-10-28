// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

// ApprovalOptions are the non-interactive approval shortcuts of `run`. They never widen the
// policy: the engine still decides which calls may run at all.
type ApprovalOptions struct {
	// Yes approves R1 calls without asking. It never approves R2+.
	Yes bool
	// Deny refuses every call that needs approval.
	Deny bool
	// AllowUpTo approves every call up to and including this risk class.
	AllowUpTo *core.RiskClass
}

// prefix marks harness messages that go to stderr next to the model output.
const prefix = "liquidharness:"

// approver implements ports.Approver. Shortcuts are checked before the prompt; without a
// reader (or when stdin is not a terminal) the answer is a denial, never a silent approval.
type approver struct {
	options ApprovalOptions
	in      io.Reader
	out     io.Writer

	mu      sync.Mutex
	reader  *bufio.Reader
	session map[string]bool
}

func newApprover(in io.Reader, out io.Writer, options ApprovalOptions) *approver {
	return &approver{
		options: options,
		in:      in,
		out:     out,
		session: make(map[string]bool),
	}
}

// Approve implements ports.Approver.
func (a *approver) Approve(ctx context.Context, request ports.ApprovalRequest) (ports.ApprovalDecision, error) {
	if err := ctx.Err(); err != nil {
		return ports.ApprovalDecision{}, err
	}
	if a.options.Deny {
		a.explain(request, "denied by --deny")
		return ports.ApprovalDecision{ApprovedBy: "flag"}, nil
	}
	if a.options.AllowUpTo != nil && request.Risk <= *a.options.AllowUpTo {
		a.explain(request, "approved by --allow-risk "+a.options.AllowUpTo.String())
		return ports.ApprovalDecision{
			Approved:   true,
			Scope:      "flag",
			ApprovedBy: "flag",
		}, nil
	}
	if a.options.Yes && request.Risk <= core.RiskR1 {
		a.explain(request, "approved by --yes")
		return ports.ApprovalDecision{
			Approved:   true,
			Scope:      "flag",
			ApprovedBy: "flag",
		}, nil
	}
	if a.in == nil {
		a.explain(request, "denied: no interactive input; pass --yes or --allow-risk")
		return ports.ApprovalDecision{ApprovedBy: "flag"}, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.session[request.Tool] {
		_, _ = fmt.Fprintf(a.out, "%s %s (%s): approved earlier in this session\n",
			prefix, request.Tool, request.Risk)
		return ports.ApprovalDecision{Approved: true, Scope: "session", ApprovedBy: "user"}, nil
	}
	if a.reader == nil {
		a.reader = bufio.NewReader(a.in)
	}
	_, _ = fmt.Fprintf(a.out, "\n%s %s (%s) needs approval\n  %s\n  arguments: %s\n  approve? [y]es / [n]o / [a]lways: ",
		prefix, request.Tool, request.Risk, request.Reason, compactArguments(request.Arguments))

	line, err := a.reader.ReadString('\n')
	if err != nil && line == "" {
		_, _ = fmt.Fprintln(a.out, "\ndenied: input closed")
		return ports.ApprovalDecision{ApprovedBy: "user"}, nil
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		_, _ = fmt.Fprintln(a.out, "approved once")
		return ports.ApprovalDecision{Approved: true, Scope: "once", ApprovedBy: "user"}, nil
	case "a", "always":
		a.session[request.Tool] = true
		_, _ = fmt.Fprintln(a.out, "approved for this session")
		return ports.ApprovalDecision{Approved: true, Scope: "session", ApprovedBy: "user"}, nil
	default:
		_, _ = fmt.Fprintln(a.out, "denied")
		return ports.ApprovalDecision{ApprovedBy: "user"}, nil
	}
}

// explain reports a shortcut decision on stderr so the user always knows why a write ran.
func (a *approver) explain(request ports.ApprovalRequest, message string) {
	_, _ = fmt.Fprintf(a.out, "%s %s (%s): %s\n", prefix, request.Tool, request.Risk, message)
}

// compactArguments renders the request arguments in one line, capped so a large file body does
// not flood the terminal.
func compactArguments(raw []byte) string {
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		return truncate(string(raw), 200)
	}
	return truncate(buffer.String(), 200)
}

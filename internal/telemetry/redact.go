// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package telemetry

import "regexp"

var redactPatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	// Authorization headers.
	{regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`), "${1}***"},
	// OpenAI-style keys and similar prefixes.
	{regexp.MustCompile(`\b(sk|xai|hf|ghp|github_pat)[-_][A-Za-z0-9_-]{8,}\b`), "${1}-***"},
	// key=value pairs for common secret names.
	{regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password|passwd)(\s*[=:]\s*)[^\s,;&"']+`), "${1}${2}***"},
	// Credentials embedded in URLs.
	{regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s]+@`), "${1}***@"},
}

// Redact removes common secret shapes from a string. It is a safety net, not a substitute for
// never logging secrets in the first place.
func Redact(value string) string {
	if value == "" {
		return value
	}
	redacted := value
	for _, rule := range redactPatterns {
		redacted = rule.pattern.ReplaceAllString(redacted, rule.replacement)
	}
	return redacted
}

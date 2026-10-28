// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/liquidharness/liquidharness/internal/core"
)

// loadDotEnv parses a minimal .env file: KEY=VALUE lines, optional "export " prefix, comments
// and single or double quotes. It is not a shell and never expands variables.
func loadDotEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, core.Wrap(core.CodeConfigInvalid, err, "open env file %s", path)
	}
	defer func() { _ = file.Close() }()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, raw, found := strings.Cut(line, "=")
		if !found {
			return nil, core.E(core.CodeConfigInvalid, "%s:%d: expected KEY=VALUE", path, lineNumber)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, core.E(core.CodeConfigInvalid, "%s:%d: empty variable name", path, lineNumber)
		}
		value, err := unquote(strings.TrimSpace(raw))
		if err != nil {
			return nil, core.Wrap(core.CodeConfigInvalid, err, "%s:%d", path, lineNumber)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, core.Wrap(core.CodeConfigInvalid, err, "read env file %s", path)
	}
	return values, nil
}

func unquote(value string) (string, error) {
	if len(value) < 2 {
		return value, nil
	}
	switch value[0] {
	case '"':
		if value[len(value)-1] != '"' {
			return "", fmt.Errorf("unterminated double quote")
		}
		return strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\"`, `"`, `\\`, `\`).Replace(value[1 : len(value)-1]), nil
	case '\'':
		if value[len(value)-1] != '\'' {
			return "", fmt.Errorf("unterminated single quote")
		}
		return value[1 : len(value)-1], nil
	default:
		// An unquoted value ends at an inline comment introduced by " #".
		if index := strings.Index(value, " #"); index >= 0 {
			value = strings.TrimSpace(value[:index])
		}
		return value, nil
	}
}

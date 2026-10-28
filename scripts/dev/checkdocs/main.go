// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

// Command checkdocs validates the repository documentation: relative links resolve to real
// files, fenced code blocks are balanced and Markdown files are UTF-8 without a BOM.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var linkPattern = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkdocs: %v\n", err)
		os.Exit(1)
	}

	files, err := collect(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkdocs: %v\n", err)
		os.Exit(1)
	}

	problems := make([]string, 0, len(files))
	for _, file := range files {
		problems = append(problems, checkFile(root, file)...)
	}
	if len(problems) > 0 {
		for _, problem := range problems {
			fmt.Fprintln(os.Stderr, problem)
		}
		fmt.Fprintf(os.Stderr, "checkdocs: %d problem(s) in %d file(s)\n", len(problems), len(files))
		os.Exit(1)
	}
	fmt.Printf("checkdocs: %d Markdown files OK\n", len(files))
}

func collect(root string) ([]string, error) {
	var files []string
	for _, name := range []string{"README.md", "SECURITY.md", "CONTRIBUTING.md", "CHANGELOG.md", "CODE_OF_CONDUCT.md"} {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
		}
	}

	docs := filepath.Join(root, "docs")
	err := filepath.WalkDir(docs, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "wip" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	template := filepath.Join(root, ".github", "PULL_REQUEST_TEMPLATE.md")
	if _, err := os.Stat(template); err == nil {
		files = append(files, template)
	}
	return files, nil
}

func checkFile(root, path string) []string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		relative = path
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("%s: cannot read: %v", relative, err)}
	}
	var problems []string
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		problems = append(problems, fmt.Sprintf("%s:1: UTF-8 BOM is not allowed", relative))
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		fenceChar   byte
		fenceLength int
		fenceLine   int
		lineNumber  int
	)
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		if fenceChar != 0 {
			trimmed := strings.TrimSpace(line)
			if len(trimmed) >= fenceLength && strings.Trim(trimmed, string(fenceChar)) == "" &&
				trimmed[0] == fenceChar {
				fenceChar = 0
				fenceLength = 0
				fenceLine = 0
			}
			continue
		}

		if char, length := openingFence(line); char != 0 {
			fenceChar = char
			fenceLength = length
			fenceLine = lineNumber
			continue
		}

		for _, match := range linkPattern.FindAllStringSubmatch(line, -1) {
			if problem := checkLink(root, path, relative, lineNumber, match[1]); problem != "" {
				problems = append(problems, problem)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		problems = append(problems, fmt.Sprintf("%s:%d: read error: %v", relative, lineNumber, err))
	}
	if fenceChar != 0 {
		problems = append(problems, fmt.Sprintf("%s:%d: unclosed code fence", relative, fenceLine))
	}
	return problems
}

func openingFence(line string) (byte, int) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) < 3 {
		return 0, 0
	}
	char := trimmed[0]
	if char != '`' && char != '~' {
		return 0, 0
	}
	length := 0
	for length < len(trimmed) && trimmed[length] == char {
		length++
	}
	if length < 3 {
		return 0, 0
	}
	return char, length
}

func checkLink(root, file, relative string, line int, target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "<")
	target = strings.TrimSuffix(target, ">")
	if target == "" || strings.HasPrefix(target, "#") {
		return ""
	}
	lower := strings.ToLower(target)
	for _, prefix := range []string{"http://", "https://", "mailto:", "tel:", "data:"} {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}

	if index := strings.IndexAny(target, "#?"); index >= 0 {
		target = target[:index]
	}
	if target == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(target); err == nil {
		target = decoded
	}

	var resolved string
	if strings.HasPrefix(target, "/") {
		resolved = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(target, "/")))
	} else {
		resolved = filepath.Join(filepath.Dir(file), filepath.FromSlash(target))
	}
	//nolint:gosec // paths come from repository Markdown reviewed in the same commit, not from users.
	if _, err := os.Stat(resolved); err != nil {
		return fmt.Sprintf("%s:%d: broken link %q", relative, line, target)
	}
	return ""
}

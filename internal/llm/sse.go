// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"bufio"
	"io"
	"strings"
)

// maxSSELine bounds a single SSE line. Model deltas are small; a line larger than this is a
// backend bug or an attack, and failing loudly is safer than growing without limit.
const maxSSELine = 1 << 20

type sseEvent struct {
	Event string
	Data  string
	ID    string
}

// sseDecoder turns a text/event-stream body into events. It follows the subset used by
// OpenAI-compatible servers: `data:` lines, an optional `event:` name, comments and blank-line
// dispatch. Multi-line data is joined with newlines as the specification requires.
type sseDecoder struct {
	scanner *bufio.Scanner
}

func newSSEDecoder(reader io.Reader) *sseDecoder {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64<<10), maxSSELine)
	return &sseDecoder{scanner: scanner}
}

// Next returns the next event. It returns io.EOF when the body ends; a pending partial event is
// discarded because it was never dispatched by a blank line.
func (d *sseDecoder) Next() (sseEvent, error) {
	var event sseEvent
	var data []string

	for d.scanner.Scan() {
		line := strings.TrimSuffix(d.scanner.Text(), "\r")
		if line == "" {
			if len(data) == 0 && event.Event == "" && event.ID == "" {
				continue
			}
			event.Data = strings.Join(data, "\n")
			return event, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")

		switch field {
		case "event":
			event.Event = value
		case "data":
			data = append(data, value)
		case "id":
			event.ID = value
		case "retry":
			// Reconnection policy is owned by the client, not by the decoder.
		}
	}

	if err := d.scanner.Err(); err != nil {
		return sseEvent{}, err
	}
	return sseEvent{}, io.EOF
}

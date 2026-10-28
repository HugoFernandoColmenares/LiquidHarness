// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSSEDecoder(t *testing.T) {
	input := "event: message\ndata: first\ndata: second\n\n" +
		": comment\n" +
		"id: 7\ndata: [DONE]\n\n"

	decoder := newSSEDecoder(strings.NewReader(input))

	first, err := decoder.Next()
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.Event != "message" || first.Data != "first\nsecond" {
		t.Errorf("first = %+v", first)
	}

	second, err := decoder.Next()
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.ID != "7" || second.Data != "[DONE]" {
		t.Errorf("second = %+v", second)
	}

	if _, err := decoder.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("third error = %v, want EOF", err)
	}
}

func TestSSEDecoderDiscardsPartialEvent(t *testing.T) {
	decoder := newSSEDecoder(strings.NewReader("data: unfinished"))
	if _, err := decoder.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("error = %v, want EOF", err)
	}
}

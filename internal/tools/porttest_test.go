// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports/porttest"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/tools"
)

func TestToolsConformance(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := tools.NewReadOnly(root)
	if err != nil {
		t.Fatalf("NewReadOnly: %v", err)
	}

	cases := []struct {
		name      string
		arguments string
	}{
		{name: "fs_read", arguments: `{"path":"README.md"}`},
		{name: "fs_list", arguments: `{"path":"."}`},
		{name: "fs_search", arguments: `{"pattern":"hello"}`},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			porttest.Tool(t, func(t *testing.T) (ports.Tool, json.RawMessage) {
				tool, ok := registry.Get(test.name)
				if !ok {
					t.Fatalf("%s is not registered", test.name)
				}
				return tool, json.RawMessage(test.arguments)
			})
		})
	}
}

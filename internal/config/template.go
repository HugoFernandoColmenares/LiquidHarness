// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

// DefaultTOML is the commented template written by `liquidharness config init`.
const DefaultTOML = `# LiquidHarness configuration
# Precedence: flags > environment > .env > project config > this file > defaults.
# Secrets do not belong here: use the OS keyring or LIQUIDHARNESS_MODEL_API_KEY.
config_version = 1

[general]
profile = "balanced"
workspace = "."
language = "en"
log_level = "info"
log_format = "text"
telemetry = "off"          # off | local | anonymous

[model]
provider = "openai-compatible"
base_url = "http://127.0.0.1:8080/v1"
name = "lfm2.5-2.6b"
# Optional. path is the GGUF file used when autostart launches llama-server.
# server_path overrides the llama-server binary found in PATH.
# manifest and mirror are optional model-manager overrides.
path = ""
server_path = ""
manifest = ""
mirror = ""
context_window = 32768
max_output_tokens = 2048
temperature = 0.1
top_k = 50
repetition_penalty = 1.1
timeout = "120s"
autostart = true

[agent]
max_steps = 24
max_tool_calls = 32
max_turn_duration = "10m"
parallel_readonly_tools = 4

[policy]
mode = "ask"               # read-only | ask | allow
allow_shell = false
allow_network = false
max_write_bytes = 1048576

[tools]
allowlist = []

[rag]
enabled = true
chunk_tokens = 512
chunk_overlap = 64
top_k = 6
max_tokens = 3000
embedding_model = "bge-m3-Q5_K_M.gguf"

[memory]
enabled = true

[mcp]
enabled = false
config = ""

[storage]
# data_dir and db_path default to the OS data directory.
retention_days = 180

[ui]
theme = "liquid-dark"
animations = true
mouse = false

[server]
addr = "127.0.0.1:8787"
`

// WriteDefault writes the template to path. It refuses to overwrite unless force is true and
// returns whether a file was created.
func WriteDefault(path string, force bool) (bool, error) {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return false, core.E(core.CodeConflict, "config file already exists: %s (use --force)", path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, core.Wrap(core.CodeInternal, err, "inspect %s", path)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, core.Wrap(core.CodeInternal, err, "create config directory")
	}
	if err := os.WriteFile(path, []byte(DefaultTOML), 0o600); err != nil {
		return false, core.Wrap(core.CodeInternal, err, "write config file %s", path)
	}
	return true, nil
}

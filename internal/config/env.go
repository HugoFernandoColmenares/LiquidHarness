// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strconv"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/policy"
)

type envBinding func(*Config, string) error

var envBindings = map[string]envBinding{
	"LIQUIDHARNESS_PROFILE": func(c *Config, v string) error {
		c.General.Profile = v
		return nil
	},
	"LIQUIDHARNESS_LOG_LEVEL": func(c *Config, v string) error {
		c.General.LogLevel = v
		return nil
	},
	"LIQUIDHARNESS_LOG_FORMAT": func(c *Config, v string) error {
		c.General.LogFormat = v
		return nil
	},
	"LIQUIDHARNESS_TELEMETRY": func(c *Config, v string) error {
		c.General.Telemetry = v
		return nil
	},
	"LIQUIDHARNESS_MODEL_PROVIDER": func(c *Config, v string) error {
		c.Model.Provider = v
		return nil
	},
	"LIQUIDHARNESS_MODEL_BASE_URL": func(c *Config, v string) error {
		c.Model.BaseURL = v
		return nil
	},
	"LIQUIDHARNESS_MODEL_NAME": func(c *Config, v string) error {
		c.Model.Name = v
		return nil
	},
	"LIQUIDHARNESS_MODEL_PATH": func(c *Config, v string) error {
		c.Model.Path = v
		return nil
	},
	"LIQUIDHARNESS_MODEL_SERVER_PATH": func(c *Config, v string) error {
		c.Model.ServerPath = v
		return nil
	},
	"LIQUIDHARNESS_MODEL_MANIFEST": func(c *Config, v string) error {
		c.Model.Manifest = v
		return nil
	},
	"LIQUIDHARNESS_MODEL_MIRROR": func(c *Config, v string) error {
		c.Model.Mirror = v
		return nil
	},
	"LIQUIDHARNESS_MODEL_API_KEY": func(c *Config, v string) error {
		c.Model.APIKey = v
		return nil
	},
	"LIQUIDHARNESS_MODEL_CONTEXT_WINDOW": func(c *Config, v string) error {
		return setInt(&c.Model.ContextWindow, v)
	},
	"LIQUIDHARNESS_MODEL_MAX_TOKENS": func(c *Config, v string) error {
		return setInt(&c.Model.MaxOutputTokens, v)
	},
	"LIQUIDHARNESS_MODEL_TEMPERATURE": func(c *Config, v string) error {
		return setFloat(&c.Model.Temperature, v)
	},
	"LIQUIDHARNESS_MODEL_TOP_K": func(c *Config, v string) error {
		return setInt(&c.Model.TopK, v)
	},
	"LIQUIDHARNESS_MODEL_TIMEOUT": func(c *Config, v string) error {
		return setDuration(&c.Model.Timeout, v)
	},
	"LIQUIDHARNESS_MODEL_AUTOSTART": func(c *Config, v string) error {
		return setBool(&c.Model.Autostart, v)
	},
	"LIQUIDHARNESS_WORKSPACE": func(c *Config, v string) error {
		c.General.Workspace = v
		return nil
	},
	"LIQUIDHARNESS_POLICY_MODE": func(c *Config, v string) error {
		mode, err := policy.ParseMode(v)
		if err != nil {
			return err
		}
		c.Policy.Mode = mode
		return nil
	},
	"LIQUIDHARNESS_POLICY_ALLOW_SHELL": func(c *Config, v string) error {
		return setBool(&c.Policy.AllowShell, v)
	},
	"LIQUIDHARNESS_POLICY_ALLOW_NETWORK": func(c *Config, v string) error {
		return setBool(&c.Policy.AllowNetwork, v)
	},
	"LIQUIDHARNESS_TOOLS_ALLOWLIST": func(c *Config, v string) error {
		c.Tools.Allowlist = splitCSV(v)
		return nil
	},
	"LIQUIDHARNESS_DATA_DIR": func(c *Config, v string) error {
		c.Storage.DataDir = v
		return nil
	},
	"LIQUIDHARNESS_DB_PATH": func(c *Config, v string) error {
		c.Storage.DBPath = v
		return nil
	},
	"LIQUIDHARNESS_RETENTION_DAYS": func(c *Config, v string) error {
		return setInt(&c.Storage.RetentionDays, v)
	},
	"LIQUIDHARNESS_RAG_ENABLED": func(c *Config, v string) error {
		return setBool(&c.RAG.Enabled, v)
	},
	"LIQUIDHARNESS_RAG_TOP_K": func(c *Config, v string) error {
		return setInt(&c.RAG.TopK, v)
	},
	"LIQUIDHARNESS_RAG_MAX_TOKENS": func(c *Config, v string) error {
		return setInt(&c.RAG.MaxTokens, v)
	},
	"LIQUIDHARNESS_MEMORY_ENABLED": func(c *Config, v string) error {
		return setBool(&c.Memory.Enabled, v)
	},
	"LIQUIDHARNESS_MCP_ENABLED": func(c *Config, v string) error {
		return setBool(&c.MCP.Enabled, v)
	},
	"LIQUIDHARNESS_MCP_CONFIG": func(c *Config, v string) error {
		c.MCP.ConfigPath = v
		return nil
	},
	"LIQUIDHARNESS_SERVE_ADDR": func(c *Config, v string) error {
		c.Server.Addr = v
		return nil
	},
	"LIQUIDHARNESS_SERVE_TOKEN": func(c *Config, v string) error {
		c.Server.Token = v
		return nil
	},
}

func applyEnv(cfg *Config, lookup func(string) (string, bool)) error {
	// Legacy alias, applied first so the canonical variable wins when both are set.
	if value, ok := lookup("LIQUIDHARNESS_LLAMA_SERVER"); ok && value != "" {
		cfg.Model.ServerPath = value
	}
	for key, bind := range envBindings {
		value, ok := lookup(key)
		if !ok || value == "" {
			continue
		}
		if err := bind(cfg, value); err != nil {
			return envError(key, err)
		}
	}
	return nil
}

func applyEnvValue(cfg *Config, key, value string) error {
	bind, ok := envBindings[key]
	if !ok {
		// A .env file may carry unrelated variables; only reject unknown
		// LIQUIDHARNESS_* keys, where a typo is almost certainly the cause.
		if !strings.HasPrefix(key, "LIQUIDHARNESS_") {
			return nil
		}
		return core.E(core.CodeConfigInvalid, "unknown variable %s in .env file", key)
	}
	if err := bind(cfg, value); err != nil {
		return envError(key, err)
	}
	return nil
}

func envError(key string, err error) error {
	return core.Wrap(core.CodeConfigInvalid, err, "invalid value for %s", key)
}

func applyFlags(cfg *Config, flags Flags) error {
	if flags.Workspace != nil {
		cfg.General.Workspace = *flags.Workspace
	}
	if flags.Profile != nil {
		cfg.General.Profile = *flags.Profile
	}
	if flags.LogLevel != nil {
		cfg.General.LogLevel = *flags.LogLevel
	}
	if flags.LogFormat != nil {
		cfg.General.LogFormat = *flags.LogFormat
	}
	if flags.BaseURL != nil {
		cfg.Model.BaseURL = *flags.BaseURL
	}
	if flags.Model != nil {
		cfg.Model.Name = *flags.Model
	}
	if flags.DataDir != nil {
		cfg.Storage.DataDir = *flags.DataDir
	}
	if flags.PolicyMode != nil {
		mode, err := policy.ParseMode(*flags.PolicyMode)
		if err != nil {
			return core.Wrap(core.CodeUsage, err, "invalid value for --policy")
		}
		cfg.Policy.Mode = mode
	}
	return nil
}

func setBool(target *bool, value string) error {
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return err
	}
	*target = parsed
	return nil
}

func setInt(target *int, value string) error {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return err
	}
	*target = parsed
	return nil
}

func setFloat(target *float64, value string) error {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return err
	}
	*target = parsed
	return nil
}

func setDuration(target *Duration, value string) error {
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return err
	}
	*target = Duration(parsed)
	return nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

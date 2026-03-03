// Package config loads application configuration from DB or .env file.
package config

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"desktriage.davea.me/db/dbgen"
)

// Known config keys.
const (
	KeyFreshdeskURL    = "freshdesk_url"
	KeyFreshdeskKey    = "freshdesk_key"
	KeyFreshdeskCookie = "freshdesk_cookie"
)

// ConfigDef describes a config key with its description.
type ConfigDef struct {
	Key         string
	Description string
	Required    bool
	Sensitive   bool // mask in UI
}

// KnownKeys lists all recognised configuration keys in display order.
var KnownKeys = []ConfigDef{
	{Key: KeyFreshdeskURL, Description: "Freshdesk API base URL", Required: true},
	{Key: KeyFreshdeskKey, Description: "Freshdesk API key", Required: true, Sensitive: true},
	{Key: KeyFreshdeskCookie, Description: "Optional cookie header for Freshdesk requests", Sensitive: true},
}

// Config holds all application configuration.
type Config struct {
	FreshdeskURL string
	FreshdeskKey string
}

// LoadFromDB loads configuration from the config table.
func LoadFromDB(ctx context.Context, q *dbgen.Queries) (*Config, error) {
	rows, err := q.ListConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("list config: %w", err)
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}

	cfg := &Config{
		FreshdeskURL: m[KeyFreshdeskURL],
		FreshdeskKey: m[KeyFreshdeskKey],
	}

	if cfg.FreshdeskURL == "" {
		return nil, fmt.Errorf("%s is required", KeyFreshdeskURL)
	}
	if cfg.FreshdeskKey == "" {
		return nil, fmt.Errorf("%s is required", KeyFreshdeskKey)
	}
	return cfg, nil
}

// SeedFromEnv reads a .env file and writes any values into the config table
// that don't already exist. Returns the number of keys seeded.
func SeedFromEnv(ctx context.Context, q *dbgen.Queries, envPath string) (int, error) {
	env, err := parseEnvFile(envPath)
	if err != nil {
		return 0, err
	}

	// Map .env keys to config keys
	mapping := map[string]struct {
		ConfigKey   string
		Description string
	}{
		"FRESHDESK_URL":    {KeyFreshdeskURL, "Freshdesk API base URL"},
		"FRESHDESK_KEY":    {KeyFreshdeskKey, "Freshdesk API key"},
		"FRESHDESK_COOKIE": {KeyFreshdeskCookie, "Optional cookie header for Freshdesk requests"},
	}

	seeded := 0
	for envKey, def := range mapping {
		val, ok := env[envKey]
		if !ok || val == "" {
			continue
		}
		// Check if already exists
		_, err := q.GetConfig(ctx, def.ConfigKey)
		if err == nil {
			continue // already set
		}
		if err != sql.ErrNoRows {
			return seeded, fmt.Errorf("check config %s: %w", def.ConfigKey, err)
		}
		if err := q.UpsertConfig(ctx, dbgen.UpsertConfigParams{
			Key:         def.ConfigKey,
			Value:       val,
			Description: def.Description,
		}); err != nil {
			return seeded, fmt.Errorf("seed config %s: %w", def.ConfigKey, err)
		}
		seeded++
	}
	return seeded, nil
}

// Load reads configuration from a .env file (legacy).
func Load(path string) (*Config, error) {
	env, err := parseEnvFile(path)
	if err != nil {
		return nil, fmt.Errorf("loading config from %s: %w", path, err)
	}

	cfg := &Config{
		FreshdeskURL: env["FRESHDESK_URL"],
		FreshdeskKey: env["FRESHDESK_KEY"],
	}

	if cfg.FreshdeskURL == "" {
		return nil, fmt.Errorf("FRESHDESK_URL is required")
	}
	if cfg.FreshdeskKey == "" {
		return nil, fmt.Errorf("FRESHDESK_KEY is required")
	}

	return cfg, nil
}

func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	env := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Strip surrounding quotes
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		env[key] = value
	}
	return env, scanner.Err()
}

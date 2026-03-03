package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnv(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadValid(t *testing.T) {
	path := writeEnv(t, `FRESHDESK_URL=https://example.freshdesk.com
FRESHDESK_KEY=mykey123
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FreshdeskURL != "https://example.freshdesk.com" {
		t.Errorf("URL = %q, want %q", cfg.FreshdeskURL, "https://example.freshdesk.com")
	}
	if cfg.FreshdeskKey != "mykey123" {
		t.Errorf("Key = %q, want %q", cfg.FreshdeskKey, "mykey123")
	}
}

func TestLoadMissingURL(t *testing.T) {
	path := writeEnv(t, `FRESHDESK_KEY=mykey123`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing FRESHDESK_URL")
	}
}

func TestLoadMissingKey(t *testing.T) {
	path := writeEnv(t, `FRESHDESK_URL=https://example.freshdesk.com`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing FRESHDESK_KEY")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/.env")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadComments(t *testing.T) {
	path := writeEnv(t, `# This is a comment
FRESHDESK_URL=https://example.freshdesk.com
# Another comment
FRESHDESK_KEY=key123
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FreshdeskURL != "https://example.freshdesk.com" {
		t.Errorf("URL = %q", cfg.FreshdeskURL)
	}
}

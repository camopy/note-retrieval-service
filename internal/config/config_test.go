package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSuccess(t *testing.T) {
	t.Setenv("NRS_LISTEN_ADDR", "127.0.0.1:8787")
	t.Setenv("NRS_API_KEY", "test-key")
	t.Setenv("NRS_VERSION", "test")
	t.Setenv("NRS_LOG_LEVEL", "debug")
	t.Setenv("NRS_VAULT_NAME", "primary")
	t.Setenv("NRS_VAULT_PATH", "./fixtures/vault")
	t.Setenv("NRS_NOTESMD_CLI_BIN", "/usr/local/bin/notesmd-cli")
	t.Setenv("NRS_NOTESMD_CLI_HOME", "./tmp/notesmd-home")
	t.Setenv("NRS_NOTESMD_CLI_TIMEOUT_MS", "9000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ListenAddr != "127.0.0.1:8787" {
		t.Fatalf("cfg.ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.APIKey != "test-key" {
		t.Fatalf("cfg.APIKey = %q", cfg.APIKey)
	}
	if cfg.Version != "test" {
		t.Fatalf("cfg.Version = %q", cfg.Version)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("cfg.LogLevel = %q", cfg.LogLevel)
	}
	if cfg.VaultName != "primary" {
		t.Fatalf("cfg.VaultName = %q", cfg.VaultName)
	}
	if cfg.VaultPath != filepath.Clean("./fixtures/vault") {
		t.Fatalf("cfg.VaultPath = %q", cfg.VaultPath)
	}
	if cfg.CLIBinary != "/usr/local/bin/notesmd-cli" {
		t.Fatalf("cfg.CLIBinary = %q", cfg.CLIBinary)
	}
	if cfg.CLIHome != filepath.Clean("./tmp/notesmd-home") {
		t.Fatalf("cfg.CLIHome = %q", cfg.CLIHome)
	}
	if cfg.CLITimeout != 9*time.Second {
		t.Fatalf("cfg.CLITimeout = %v", cfg.CLITimeout)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("NRS_LISTEN_ADDR", "127.0.0.1:8787")
	t.Setenv("NRS_API_KEY", "test-key")
	t.Setenv("NRS_VAULT_PATH", "./fixtures/vault")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Version != defaultVersion {
		t.Fatalf("cfg.Version = %q, want %q", cfg.Version, defaultVersion)
	}
	if cfg.LogLevel != defaultLogLevel {
		t.Fatalf("cfg.LogLevel = %q, want %q", cfg.LogLevel, defaultLogLevel)
	}
	if cfg.VaultName != "main" {
		t.Fatalf("cfg.VaultName = %q, want main", cfg.VaultName)
	}
	if cfg.CLIBinary != defaultCLIBinary {
		t.Fatalf("cfg.CLIBinary = %q, want %q", cfg.CLIBinary, defaultCLIBinary)
	}
	if cfg.CLIHome != filepath.Clean(defaultCLIHome) {
		t.Fatalf("cfg.CLIHome = %q", cfg.CLIHome)
	}
	if cfg.CLITimeout != 5*time.Second {
		t.Fatalf("cfg.CLITimeout = %v, want 5s", cfg.CLITimeout)
	}
}

func TestLoadMissingRequiredEnv(t *testing.T) {
	t.Setenv("NRS_LISTEN_ADDR", "")
	t.Setenv("NRS_API_KEY", "")
	t.Setenv("NRS_VAULT_PATH", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
}

func TestLoadRejectsInvalidTimeout(t *testing.T) {
	t.Setenv("NRS_LISTEN_ADDR", "127.0.0.1:8787")
	t.Setenv("NRS_API_KEY", "test-key")
	t.Setenv("NRS_VAULT_PATH", "./fixtures/vault")
	t.Setenv("NRS_NOTESMD_CLI_TIMEOUT_MS", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
}

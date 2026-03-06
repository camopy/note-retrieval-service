package notesmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/camopy/note-retrieval-service/internal/apierror"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		VaultName: "test-vault",
		VaultPath: t.TempDir(),
		Binary:    "notesmd-cli",
		CLIHome:   filepath.Join(t.TempDir(), "notesmd-home"),
		Timeout:   time.Second,
	}
}

func TestBuildArgsIncludeVault(t *testing.T) {
	client := New(testConfig(t))

	if got := client.buildPrintArgs("folder/demo"); len(got) != 4 || got[0] != "print" || got[1] != "folder/demo.md" {
		t.Fatalf("buildPrintArgs() = %#v", got)
	}
	if got := client.buildFrontmatterArgs("folder/demo"); len(got) != 5 || got[0] != "frontmatter" || got[1] != "folder/demo.md" {
		t.Fatalf("buildFrontmatterArgs() = %#v", got)
	}
}

func TestEnsureCLIHomeWritesObsidianConfig(t *testing.T) {
	cfg := testConfig(t)
	client := NewWithRunner(cfg, func(_ context.Context, _ string, _ []string, _ []string) (RunResult, error) {
		return RunResult{Stdout: "notesmd-cli 1.0.0"}, nil
	})

	if _, err := client.Version(context.Background()); err != nil {
		t.Fatalf("Version() error = %v", err)
	}

	linuxConfig := filepath.Join(cfg.CLIHome, ".config", "obsidian", "obsidian.json")
	macConfig := filepath.Join(cfg.CLIHome, "Library", "Application Support", "obsidian", "obsidian.json")
	for _, configPath := range []string{linuxConfig, macConfig} {
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", configPath, err)
		}
		if string(content) == "" {
			t.Fatalf("config at %q is empty", configPath)
		}
	}
}

func TestPrintMapsMissingBinary(t *testing.T) {
	client := NewWithRunner(testConfig(t), func(_ context.Context, _ string, _ []string, _ []string) (RunResult, error) {
		return RunResult{}, exec.ErrNotFound
	})

	_, err := client.Print(context.Background(), "demo")
	if err == nil {
		t.Fatal("Print() error = nil, want non-nil")
	}
	apiErr, ok := err.(*apierror.Error)
	if !ok || apiErr.Code != "cli_not_available" {
		t.Fatalf("Print() error = %#v, want cli_not_available", err)
	}
}

func TestPrintMapsExitCodes(t *testing.T) {
	client := NewWithRunner(testConfig(t), func(_ context.Context, _ string, _ []string, _ []string) (RunResult, error) {
		return RunResult{ExitCode: 2, Stderr: "not found"}, nil
	})

	_, err := client.Print(context.Background(), "demo")
	if err == nil {
		t.Fatal("Print() error = nil, want non-nil")
	}
	apiErr, ok := err.(*apierror.Error)
	if !ok || apiErr.Code != "note_not_found" {
		t.Fatalf("Print() error = %#v, want note_not_found", err)
	}
}

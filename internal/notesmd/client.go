package notesmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/camopy/note-retrieval-service/internal/apierror"
	"github.com/camopy/note-retrieval-service/internal/vault"
)

type Config struct {
	VaultName string
	VaultPath string
	Binary    string
	CLIHome   string
	Timeout   time.Duration
}

type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Runner func(ctx context.Context, command string, args []string, env []string) (RunResult, error)

type Client struct {
	config Config
	runner Runner
}

func New(config Config) *Client {
	return &Client{
		config: config,
		runner: defaultRunner,
	}
}

func NewWithRunner(config Config, runner Runner) *Client {
	if runner == nil {
		runner = defaultRunner
	}
	return &Client{
		config: config,
		runner: runner,
	}
}

func (c *Client) Version(ctx context.Context) (string, error) {
	result, err := c.runChecked(ctx, []string{"--version"})
	if err != nil {
		return "", err
	}

	version := strings.TrimSpace(result.Stdout)
	if version == "" {
		version = strings.TrimSpace(result.Stderr)
	}
	if version == "" {
		return "available", nil
	}
	return version, nil
}

func (c *Client) Print(ctx context.Context, notePath string) (string, error) {
	if _, err := vault.NormalizeNotePath(notePath); err != nil {
		return "", err
	}
	result, err := c.runChecked(ctx, c.buildPrintArgs(notePath))
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

func (c *Client) PrintFrontmatter(ctx context.Context, notePath string) (string, error) {
	if _, err := vault.NormalizeNotePath(notePath); err != nil {
		return "", err
	}
	result, err := c.runChecked(ctx, c.buildFrontmatterArgs(notePath))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (c *Client) buildPrintArgs(notePath string) []string {
	normalizedPath, _ := vault.NormalizeNotePath(notePath)
	return c.withVaultArgs([]string{"print", normalizedPath})
}

func (c *Client) buildFrontmatterArgs(notePath string) []string {
	normalizedPath, _ := vault.NormalizeNotePath(notePath)
	return c.withVaultArgs([]string{"frontmatter", normalizedPath, "--print"})
}

func (c *Client) runChecked(ctx context.Context, args []string) (RunResult, error) {
	result, err := c.runRaw(ctx, args)
	if err != nil {
		return RunResult{}, err
	}

	if result.ExitCode == 0 {
		return result, nil
	}

	switch result.ExitCode {
	case 1:
		return RunResult{}, apierror.New(400, "cli_usage_error", "notesmd-cli rejected the command.")
	case 2:
		return RunResult{}, apierror.New(404, "note_not_found", "Note not found.")
	default:
		return RunResult{}, &apierror.Error{
			Status:  502,
			Code:    "cli_command_failed",
			Message: "notesmd-cli command failed.",
			Err:     fmt.Errorf("stderr=%s", strings.TrimSpace(result.Stderr)),
		}
	}
}

func (c *Client) runRaw(ctx context.Context, args []string) (RunResult, error) {
	if err := c.ensureCLIHome(); err != nil {
		return RunResult{}, apierror.Wrap(503, "cli_config_error", "Failed to prepare notesmd-cli runtime configuration.", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	command := c.config.Binary
	commandArgs := args
	if strings.HasSuffix(command, ".js") || strings.HasSuffix(command, ".mjs") {
		commandArgs = append([]string{command}, args...)
		command = "node"
	}

	result, err := c.runner(timeoutCtx, command, commandArgs, c.commandEnv())
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return RunResult{}, apierror.New(503, "cli_not_available", "notesmd-cli binary not found.")
		}
		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			return RunResult{}, apierror.New(504, "cli_timeout", "notesmd-cli command timed out.")
		}
		return RunResult{}, apierror.Wrap(502, "cli_execution_failed", "Failed to execute notesmd-cli.", err)
	}

	return result, nil
}

func (c *Client) withVaultArgs(args []string) []string {
	return append(args, "--vault", c.config.VaultName)
}

func (c *Client) commandEnv() []string {
	env := os.Environ()
	env = append(env, "HOME="+c.config.CLIHome)
	env = append(env, "XDG_CONFIG_HOME="+filepath.Join(c.config.CLIHome, ".config"))
	return env
}

func (c *Client) ensureCLIHome() error {
	payload := fmt.Sprintf("{\n  \"vaults\": {\n    %q: {\n      \"path\": %q\n    }\n  }\n}\n", c.config.VaultName, c.config.VaultPath)
	targets := []string{
		filepath.Join(c.config.CLIHome, ".config", "obsidian", "obsidian.json"),
		filepath.Join(c.config.CLIHome, "Library", "Application Support", "obsidian", "obsidian.json"),
	}

	for _, target := range targets {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(payload), 0o644); err != nil {
			return err
		}
	}

	return nil
}

func defaultRunner(ctx context.Context, command string, args []string, env []string) (RunResult, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = env

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return RunResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: 0,
		}, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return RunResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: exitErr.ExitCode(),
		}, nil
	}

	return RunResult{}, err
}

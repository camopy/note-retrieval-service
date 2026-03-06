package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultVersion   = "dev"
	defaultLogLevel  = "info"
	defaultCLIBinary = "notesmd-cli"
	defaultCLIHome   = ".runtime/notesmd-home"
	defaultTimeoutMS = 5000
)

type Config struct {
	ListenAddr string
	APIKey     string
	Version    string
	LogLevel   string
	VaultName  string
	VaultPath  string
	CLIBinary  string
	CLIHome    string
	CLITimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddr: strings.TrimSpace(os.Getenv("NRS_LISTEN_ADDR")),
		APIKey:     strings.TrimSpace(os.Getenv("NRS_API_KEY")),
		Version:    strings.TrimSpace(os.Getenv("NRS_VERSION")),
		LogLevel:   strings.TrimSpace(os.Getenv("NRS_LOG_LEVEL")),
		VaultName:  strings.TrimSpace(os.Getenv("NRS_VAULT_NAME")),
		VaultPath:  strings.TrimSpace(os.Getenv("NRS_VAULT_PATH")),
		CLIBinary:  strings.TrimSpace(os.Getenv("NRS_NOTESMD_CLI_BIN")),
		CLIHome:    strings.TrimSpace(os.Getenv("NRS_NOTESMD_CLI_HOME")),
	}

	if cfg.Version == "" {
		cfg.Version = defaultVersion
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = defaultLogLevel
	}
	if cfg.VaultName == "" {
		cfg.VaultName = "main"
	}
	if cfg.CLIBinary == "" {
		cfg.CLIBinary = defaultCLIBinary
	}
	if cfg.CLIHome == "" {
		cfg.CLIHome = defaultCLIHome
	}

	timeoutMS := defaultTimeoutMS
	if rawTimeout := strings.TrimSpace(os.Getenv("NRS_NOTESMD_CLI_TIMEOUT_MS")); rawTimeout != "" {
		parsedTimeout, err := strconv.Atoi(rawTimeout)
		if err != nil || parsedTimeout <= 0 {
			return Config{}, errors.New("NRS_NOTESMD_CLI_TIMEOUT_MS must be a positive integer when provided")
		}
		timeoutMS = parsedTimeout
	}
	cfg.CLITimeout = time.Duration(timeoutMS) * time.Millisecond

	var validationErrs []error
	if cfg.ListenAddr == "" {
		validationErrs = append(validationErrs, errors.New("NRS_LISTEN_ADDR is required"))
	}
	if cfg.APIKey == "" {
		validationErrs = append(validationErrs, errors.New("NRS_API_KEY is required"))
	}
	if cfg.VaultName == "" {
		validationErrs = append(validationErrs, errors.New("NRS_VAULT_NAME is required"))
	}
	if cfg.VaultPath == "" {
		validationErrs = append(validationErrs, errors.New("NRS_VAULT_PATH is required"))
	}
	if len(validationErrs) > 0 {
		return Config{}, errors.Join(validationErrs...)
	}

	cfg.VaultPath = filepath.Clean(cfg.VaultPath)
	cfg.CLIHome = filepath.Clean(cfg.CLIHome)

	return cfg, nil
}

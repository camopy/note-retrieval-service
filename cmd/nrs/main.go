package main

import (
	"log"
	"log/slog"

	"github.com/camopy/note-retrieval-service/internal/config"
	httpserver "github.com/camopy/note-retrieval-service/internal/http"
	"github.com/camopy/note-retrieval-service/internal/logging"
	"github.com/camopy/note-retrieval-service/internal/notesmd"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	logger := logging.New(cfg.LogLevel)
	logStartupDiagnostics(logger, cfg)

	client := notesmd.New(notesmd.Config{
		VaultName: cfg.VaultName,
		VaultPath: cfg.VaultPath,
		Binary:    cfg.CLIBinary,
		CLIHome:   cfg.CLIHome,
		Timeout:   cfg.CLITimeout,
	})

	server := httpserver.New(cfg, logger, client)
	logger.Info("starting nrs service", "listen_addr", cfg.ListenAddr, "vault_path", cfg.VaultPath, "version", cfg.Version)

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("nrs server exited: %v", err)
	}
}

func logStartupDiagnostics(logger *slog.Logger, cfg config.Config) {
	if logger == nil {
		return
	}

	logger.Info(
		"startup_diagnostics",
		"listen_addr", cfg.ListenAddr,
		"vault_name", cfg.VaultName,
		"vault_path", cfg.VaultPath,
		"cli_binary", cfg.CLIBinary,
		"cli_home", cfg.CLIHome,
		"cli_timeout_ms", cfg.CLITimeout.Milliseconds(),
		"version", cfg.Version,
	)
}

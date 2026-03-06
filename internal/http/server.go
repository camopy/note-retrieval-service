package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/camopy/note-retrieval-service/internal/apierror"
	"github.com/camopy/note-retrieval-service/internal/auth"
	"github.com/camopy/note-retrieval-service/internal/config"
	"github.com/camopy/note-retrieval-service/internal/vault"
	"github.com/camopy/note-retrieval-service/openapi"
)

const maxJSONBodyBytes int64 = 1 << 20

type notesClient interface {
	Version(ctx context.Context) (string, error)
	Print(ctx context.Context, notePath string) (string, error)
	PrintFrontmatter(ctx context.Context, notePath string) (string, error)
}

type Server struct {
	httpServer *http.Server
}

type folderReadRequest struct {
	Path string `json:"path"`
}

type notesListRequest struct {
	Folder    string `json:"folder"`
	Recursive bool   `json:"recursive"`
	Limit     int    `json:"limit"`
}

type notePathRequest struct {
	Path string `json:"path"`
}

type searchRequest struct {
	Query          string   `json:"query"`
	Limit          int      `json:"limit"`
	FolderPrefixes []string `json:"folder_prefixes"`
}

type apiErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type apiErrorResponse struct {
	Error apiErrorDetail `json:"error"`
}

func New(cfg config.Config, logger *slog.Logger, client notesClient) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(cfg, client))
	mux.HandleFunc("GET /openapi.yaml", openapiHandler)
	mux.Handle("POST /folders/read", auth.Bearer(cfg.APIKey, folderReadHandler(cfg, logger)))
	mux.Handle("POST /notes/list", auth.Bearer(cfg.APIKey, notesListHandler(cfg, logger)))
	mux.Handle("POST /notes/read", auth.Bearer(cfg.APIKey, noteReadHandler(cfg, client, logger)))
	mux.Handle("POST /notes/frontmatter", auth.Bearer(cfg.APIKey, noteFrontmatterHandler(cfg, client, logger)))
	mux.Handle("POST /search", auth.Bearer(cfg.APIKey, searchHandler(cfg, logger)))

	return &Server{
		httpServer: &http.Server{
			Addr:    cfg.ListenAddr,
			Handler: requestLoggingMiddleware(logger, mux),
		},
	}
}

func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

func healthHandler(cfg config.Config, client notesClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cliVersion, cliErr := client.Version(r.Context())
		noteCount, countErr := vault.CountNotes(cfg.VaultPath)

		response := map[string]any{
			"status":  "ok",
			"version": cfg.Version,
			"cli": map[string]any{
				"available":  cliErr == nil,
				"version":    nullableString(cliVersion),
				"binary":     cfg.CLIBinary,
				"timeout_ms": cfg.CLITimeout.Milliseconds(),
			},
			"vault": map[string]any{
				"name":       cfg.VaultName,
				"path":       cfg.VaultPath,
				"accessible": countErr == nil,
				"note_count": maxZero(noteCount),
			},
		}

		statusCode := http.StatusOK
		if cliErr != nil || countErr != nil {
			response["status"] = "degraded"
			statusCode = http.StatusServiceUnavailable
		}

		writeJSON(w, statusCode, response)
	}
}

func openapiHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapi.Bytes())
}

func folderReadHandler(cfg config.Config, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req folderReadRequest
		if err := decodeJSONBody(r, &req, true); err != nil {
			respondAPIError(w, logger, err)
			return
		}

		document, err := vault.ListFolder(cfg.VaultPath, req.Path)
		if err != nil {
			respondError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, document)
	})
}

func notesListHandler(cfg config.Config, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req notesListRequest
		if err := decodeJSONBody(r, &req, true); err != nil {
			respondAPIError(w, logger, err)
			return
		}

		limit := req.Limit
		if limit == 0 {
			limit = 100
		}
		if limit < 1 || limit > 500 {
			respondAPIError(w, logger, apierror.New(400, "invalid_request", "limit must be between 1 and 500"))
			return
		}

		items, err := vault.ListNotes(cfg.VaultPath, req.Folder, req.Recursive, limit)
		if err != nil {
			respondError(w, logger, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"folder":    req.Folder,
			"recursive": req.Recursive,
			"count":     len(items),
			"items":     items,
		})
	})
}

func noteReadHandler(cfg config.Config, client notesClient, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req notePathRequest
		if err := decodeJSONBody(r, &req, false); err != nil {
			respondAPIError(w, logger, err)
			return
		}

		normalizedPath, err := vault.NormalizeNotePath(req.Path)
		if err != nil {
			respondError(w, logger, err)
			return
		}

		rawMarkdown, err := client.Print(r.Context(), normalizedPath)
		if err != nil {
			respondError(w, logger, err)
			return
		}
		summary, err := vault.ReadNoteSummaryFromFile(cfg.VaultPath, normalizedPath)
		if err != nil {
			respondError(w, logger, err)
			return
		}

		parsed := vault.ParseMarkdown(rawMarkdown)
		writeJSON(w, http.StatusOK, vault.NoteDocument{
			NoteSummary: summary,
			Content:     parsed.Content,
			RawMarkdown: rawMarkdown,
		})
	})
}

func noteFrontmatterHandler(cfg config.Config, client notesClient, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req notePathRequest
		if err := decodeJSONBody(r, &req, false); err != nil {
			respondAPIError(w, logger, err)
			return
		}

		normalizedPath, err := vault.NormalizeNotePath(req.Path)
		if err != nil {
			respondError(w, logger, err)
			return
		}

		rawFrontmatter, err := client.PrintFrontmatter(r.Context(), normalizedPath)
		if err != nil {
			respondError(w, logger, err)
			return
		}
		summary, err := vault.ReadNoteSummaryFromFile(cfg.VaultPath, normalizedPath)
		if err != nil {
			respondError(w, logger, err)
			return
		}

		summary.Frontmatter = vault.ParseFrontmatterBody(rawFrontmatter)
		writeJSON(w, http.StatusOK, summary)
	})
}

func searchHandler(cfg config.Config, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req searchRequest
		if err := decodeJSONBody(r, &req, false); err != nil {
			respondAPIError(w, logger, err)
			return
		}

		limit := req.Limit
		if limit == 0 {
			limit = 20
		}
		if limit < 1 || limit > 50 {
			respondAPIError(w, logger, apierror.New(400, "invalid_request", "limit must be between 1 and 50"))
			return
		}

		items, err := vault.Search(cfg.VaultPath, req.Query, limit, req.FolderPrefixes)
		if err != nil {
			respondError(w, logger, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"query": req.Query,
			"count": len(items),
			"items": items,
		})
	})
}

func decodeJSONBody(r *http.Request, dest any, allowEmpty bool) *apierror.Error {
	if r.Body == nil {
		if allowEmpty {
			return nil
		}
		return apierror.New(400, "invalid_request", "request body is required")
	}

	decoder := json.NewDecoder(io.LimitReader(r.Body, maxJSONBodyBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dest); err != nil {
		if errors.Is(err, io.EOF) && allowEmpty {
			return nil
		}
		if errors.Is(err, io.EOF) {
			return apierror.New(400, "invalid_request", "request body is required")
		}
		return apierror.Wrap(400, "invalid_json", "invalid JSON body", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return apierror.New(400, "invalid_json", "invalid JSON body")
	}
	return nil
}

func respondError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var apiErr *apierror.Error
	if errors.As(err, &apiErr) {
		respondAPIError(w, logger, apiErr)
		return
	}
	respondAPIError(w, logger, apierror.Wrap(500, "internal_error", "internal server error", err))
}

func respondAPIError(w http.ResponseWriter, logger *slog.Logger, err *apierror.Error) {
	if err == nil {
		return
	}
	if logger != nil {
		logger.Error("request_failed", "status", err.Status, "code", err.Code, "message", err.Message, "error", err.Err)
	}
	writeJSON(w, err.Status, apiErrorResponse{
		Error: apiErrorDetail{
			Code:    err.Code,
			Message: err.Message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status        int
	responseBytes int
}

func (w *loggingResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *loggingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.responseBytes += n
	return n, err
}

func requestLoggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		logWriter := &loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(logWriter, r)

		status := logWriter.status
		if status == 0 {
			status = http.StatusOK
		}

		route := r.Pattern
		if route == "" {
			route = r.URL.Path
		}

		requestBytes := int(r.ContentLength)
		if requestBytes < 0 {
			requestBytes = 0
		}

		logger.Info(
			"http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"route", route,
			"status", status,
			"request_bytes", requestBytes,
			"response_bytes", logWriter.responseBytes,
			"latency_ms", time.Since(startedAt).Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func maxZero(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

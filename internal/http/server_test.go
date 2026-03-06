package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/camopy/note-retrieval-service/internal/apierror"
	"github.com/camopy/note-retrieval-service/internal/config"
	"github.com/camopy/note-retrieval-service/internal/logging"
	"github.com/camopy/note-retrieval-service/openapi"
)

type fakeNotesClient struct {
	version         string
	versionErr      error
	noteBodies      map[string]string
	noteFrontmatter map[string]string
	printErr        error
	printFrontErr   error
}

func (f *fakeNotesClient) Version(context.Context) (string, error) {
	return f.version, f.versionErr
}

func (f *fakeNotesClient) Print(_ context.Context, notePath string) (string, error) {
	if f.printErr != nil {
		return "", f.printErr
	}
	return f.noteBodies[notePath], nil
}

func (f *fakeNotesClient) PrintFrontmatter(_ context.Context, notePath string) (string, error) {
	if f.printFrontErr != nil {
		return "", f.printFrontErr
	}
	return f.noteFrontmatter[notePath], nil
}

func testConfig(root string) config.Config {
	return config.Config{
		ListenAddr: "127.0.0.1:8787",
		APIKey:     "secret",
		Version:    "test-version",
		LogLevel:   "debug",
		VaultName:  "test-vault",
		VaultPath:  root,
		CLIBinary:  "notesmd-cli",
		CLIHome:    filepath.Join(root, ".runtime"),
		CLITimeout: 5 * time.Second,
	}
}

func writeVaultFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	absolutePath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(absolutePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func authJSONRequest(t *testing.T, server *Server, method, url string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}

	req := httptest.NewRequest(method, url, &body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(rec, req)
	return rec
}

func TestHealthIsPublicAndDegradesWhenCLIIsUnavailable(t *testing.T) {
	root := t.TempDir()
	writeVaultFile(t, root, "notes/example.md", "# Example\n")
	server := New(testConfig(root), logging.New("debug"), &fakeNotesClient{
		versionErr: apierror.New(503, "cli_not_available", "missing"),
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestOpenAPISpecIsPublic(t *testing.T) {
	root := t.TempDir()
	server := New(testConfig(root), logging.New("debug"), &fakeNotesClient{})

	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "/notes/read") {
		t.Fatalf("openapi body missing notes/read route: %s", rec.Body.String())
	}
}

func TestNotesListRequiresAuth(t *testing.T) {
	root := t.TempDir()
	server := New(testConfig(root), logging.New("debug"), &fakeNotesClient{})

	req := httptest.NewRequest(http.MethodPost, "/notes/list", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestReadOnlyAPIFlow(t *testing.T) {
	root := t.TempDir()
	noteRaw := "---\ntitle: Welcome\ntags:\n  - inbox\n---\n# Welcome\nThis is the welcome note.\n"
	writeVaultFile(t, root, "inbox/welcome.md", noteRaw)
	writeVaultFile(t, root, "projects/roadmap.md", "# Roadmap\nSearch me please.\n")

	server := New(testConfig(root), logging.New("debug"), &fakeNotesClient{
		version: "notesmd-cli 1.0.0",
		noteBodies: map[string]string{
			"inbox/welcome":    noteRaw,
			"inbox/welcome.md": noteRaw,
		},
		noteFrontmatter: map[string]string{
			"inbox/welcome":    "title: Welcome\ntags:\n  - inbox",
			"inbox/welcome.md": "title: Welcome\ntags:\n  - inbox",
		},
	})

	listRec := authJSONRequest(t, server, http.MethodPost, "/notes/list", map[string]any{
		"recursive": true,
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("notes/list status = %d, body=%s", listRec.Code, listRec.Body.String())
	}

	readRec := authJSONRequest(t, server, http.MethodPost, "/notes/read", map[string]any{
		"path": "inbox/welcome",
	})
	if readRec.Code != http.StatusOK {
		t.Fatalf("notes/read status = %d, body=%s", readRec.Code, readRec.Body.String())
	}
	if !strings.Contains(readRec.Body.String(), "This is the welcome note.") {
		t.Fatalf("notes/read body missing note content: %s", readRec.Body.String())
	}

	frontmatterRec := authJSONRequest(t, server, http.MethodPost, "/notes/frontmatter", map[string]any{
		"path": "inbox/welcome",
	})
	if frontmatterRec.Code != http.StatusOK {
		t.Fatalf("notes/frontmatter status = %d, body=%s", frontmatterRec.Code, frontmatterRec.Body.String())
	}
	if !strings.Contains(frontmatterRec.Body.String(), "inbox") {
		t.Fatalf("notes/frontmatter body missing tag: %s", frontmatterRec.Body.String())
	}

	searchRec := authJSONRequest(t, server, http.MethodPost, "/search", map[string]any{
		"query": "welcome",
	})
	if searchRec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", searchRec.Code, searchRec.Body.String())
	}
	if !strings.Contains(searchRec.Body.String(), "\"match_type\":\"title\"") {
		t.Fatalf("search body missing title hit: %s", searchRec.Body.String())
	}

	folderRec := authJSONRequest(t, server, http.MethodPost, "/folders/read", map[string]any{
		"path": "inbox",
	})
	if folderRec.Code != http.StatusOK {
		t.Fatalf("folders/read status = %d, body=%s", folderRec.Code, folderRec.Body.String())
	}
}

func TestHandlersRejectTraversalAndNotFound(t *testing.T) {
	root := t.TempDir()
	server := New(testConfig(root), logging.New("debug"), &fakeNotesClient{
		printErr: apierror.New(404, "note_not_found", "missing"),
	})

	traversalRec := authJSONRequest(t, server, http.MethodPost, "/notes/read", map[string]any{
		"path": "../secret",
	})
	if traversalRec.Code != http.StatusBadRequest && traversalRec.Code != http.StatusNotFound {
		t.Fatalf("unexpected status = %d, body=%s", traversalRec.Code, traversalRec.Body.String())
	}

	notFoundRec := authJSONRequest(t, server, http.MethodPost, "/folders/read", map[string]any{
		"path": "missing",
	})
	if notFoundRec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", notFoundRec.Code, http.StatusNotFound)
	}
}

func TestOpenAPIContractIncludesBearerAndActionsRoutes(t *testing.T) {
	root := t.TempDir()
	server := New(testConfig(root), logging.New("debug"), &fakeNotesClient{})

	rec := authJSONRequest(t, server, http.MethodPost, "/notes/list", map[string]any{})
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("unexpected status = %d", rec.Code)
	}

	spec := openapi.String()
	for _, expected := range []string{"bearerAuth", "/folders/read", "/notes/list", "/notes/read", "/notes/frontmatter", "/search"} {
		if !strings.Contains(spec, expected) {
			t.Fatalf("openapi spec missing %q", expected)
		}
	}
}

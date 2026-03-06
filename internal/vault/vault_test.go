package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	absolutePath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(absolutePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestNormalizeNotePathRejectsTraversal(t *testing.T) {
	_, err := NormalizeNotePath("../secret")
	if err == nil {
		t.Fatal("NormalizeNotePath() error = nil, want non-nil")
	}
}

func TestReadNoteDocumentParsesFrontmatterAndTitle(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "inbox/welcome.md", "---\ntitle: Welcome\ntags:\n  - inbox\npublished: true\ncount: 3\n---\n# Ignored\nhello\n")

	document, err := ReadNoteDocumentFromFile(root, "inbox/welcome")
	if err != nil {
		t.Fatalf("ReadNoteDocumentFromFile() error = %v", err)
	}

	if document.Title != "Welcome" {
		t.Fatalf("document.Title = %q, want Welcome", document.Title)
	}
	if len(document.Frontmatter) == 0 {
		t.Fatal("document.Frontmatter is empty")
	}
	if document.Content == "" {
		t.Fatal("document.Content is empty")
	}
}

func TestListFolderAndSearch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "projects/roadmap.md", "# Roadmap\nSearch me please.\n")
	writeFile(t, root, "projects/todo.md", "# Todo\nRoadmap follow-up.\n")
	writeFile(t, root, "archive/done.md", "# Done\nNothing here.\n")

	folder, err := ListFolder(root, "projects")
	if err != nil {
		t.Fatalf("ListFolder() error = %v", err)
	}
	if len(folder.Notes) != 2 {
		t.Fatalf("len(folder.Notes) = %d, want 2", len(folder.Notes))
	}

	results, err := Search(root, "roadmap", 10, []string{"projects"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search() returned no results")
	}
}

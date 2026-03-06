package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/camopy/note-retrieval-service/internal/apierror"
)

const markdownExtension = ".md"

type NoteSummary struct {
	Path        string         `json:"path"`
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	FolderPath  string         `json:"folder_path"`
	UpdatedAt   string         `json:"updated_at"`
	Checksum    string         `json:"checksum"`
	Frontmatter map[string]any `json:"frontmatter"`
}

type NoteDocument struct {
	NoteSummary
	Content     string `json:"content"`
	RawMarkdown string `json:"raw_markdown"`
}

type FolderEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type FolderDocument struct {
	Path    string        `json:"path"`
	Folders []FolderEntry `json:"folders"`
	Notes   []NoteSummary `json:"notes"`
}

type SearchHit struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	FolderPath string `json:"folder_path"`
	UpdatedAt  string `json:"updated_at"`
	Checksum   string `json:"checksum"`
	LineNumber int    `json:"line_number"`
	Snippet    string `json:"snippet"`
	MatchType  string `json:"match_type"`
}

type ParsedMarkdown struct {
	Frontmatter map[string]any
	Content     string
}

func NormalizeNotePath(raw string) (string, error) {
	normalized, err := normalizeRelativePath(raw, false)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(normalized), markdownExtension) {
		normalized += markdownExtension
	}
	return normalized, nil
}

func NormalizeFolderPath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	normalized, err := normalizeRelativePath(raw, false)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(strings.ToLower(normalized), markdownExtension) {
		return "", apierror.New(400, "invalid_request", "folder path cannot reference a note")
	}
	return normalized, nil
}

func CountNotes(rootPath string) (int, error) {
	rootResolved, err := resolveRoot(rootPath)
	if err != nil {
		return 0, err
	}

	notePaths, apiErr := walkMarkdownFiles(rootResolved, "", true)
	if apiErr != nil {
		return 0, apiErr
	}
	return len(notePaths), nil
}

func ListNotes(rootPath, folderPath string, recursive bool, limit int) ([]NoteSummary, error) {
	rootResolved, err := resolveRoot(rootPath)
	if err != nil {
		return nil, err
	}
	normalizedFolder, err := NormalizeFolderPath(folderPath)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}

	basePath, _, apiErr := resolveExistingPath(rootResolved, normalizedFolder, true, "folder_not_found", "Folder not found.")
	if apiErr != nil {
		return nil, apiErr
	}

	relativeBase := normalizedFolder
	notePaths, apiErr := walkMarkdownFiles(basePath, relativeBase, recursive)
	if apiErr != nil {
		return nil, apiErr
	}
	if len(notePaths) > limit {
		notePaths = notePaths[:limit]
	}

	items := make([]NoteSummary, 0, len(notePaths))
	for _, notePath := range notePaths {
		summary, readErr := ReadNoteSummaryFromFile(rootResolved, notePath)
		if readErr != nil {
			return nil, readErr
		}
		items = append(items, summary)
	}
	return items, nil
}

func ListFolder(rootPath, folderPath string) (FolderDocument, error) {
	rootResolved, err := resolveRoot(rootPath)
	if err != nil {
		return FolderDocument{}, err
	}
	normalizedFolder, err := NormalizeFolderPath(folderPath)
	if err != nil {
		return FolderDocument{}, err
	}

	basePath, _, apiErr := resolveExistingPath(rootResolved, normalizedFolder, true, "folder_not_found", "Folder not found.")
	if apiErr != nil {
		return FolderDocument{}, apiErr
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return FolderDocument{}, apierror.Wrap(503, "vault_unavailable", "Failed to read folder.", err)
	}

	folders := make([]FolderEntry, 0)
	notePaths := make([]string, 0)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		childPath := joinRelative(normalizedFolder, entry.Name())
		if entry.IsDir() {
			folders = append(folders, FolderEntry{
				Path: childPath,
				Name: entry.Name(),
			})
			continue
		}
		if entry.Type().IsRegular() && isMarkdownFile(entry.Name()) {
			notePaths = append(notePaths, childPath)
		}
	}

	sort.Slice(folders, func(i, j int) bool {
		return folders[i].Path < folders[j].Path
	})
	sort.Strings(notePaths)

	notes := make([]NoteSummary, 0, len(notePaths))
	for _, notePath := range notePaths {
		summary, readErr := ReadNoteSummaryFromFile(rootResolved, notePath)
		if readErr != nil {
			return FolderDocument{}, readErr
		}
		notes = append(notes, summary)
	}

	return FolderDocument{
		Path:    normalizedFolder,
		Folders: folders,
		Notes:   notes,
	}, nil
}

func ReadNoteSummaryFromFile(rootPath, notePath string) (NoteSummary, error) {
	document, err := ReadNoteDocumentFromFile(rootPath, notePath)
	if err != nil {
		return NoteSummary{}, err
	}
	return document.NoteSummary, nil
}

func ReadNoteDocumentFromFile(rootPath, notePath string) (NoteDocument, error) {
	rootResolved, err := resolveRoot(rootPath)
	if err != nil {
		return NoteDocument{}, err
	}
	normalizedNotePath, err := NormalizeNotePath(notePath)
	if err != nil {
		return NoteDocument{}, err
	}

	absolutePath, relativePath, apiErr := resolveExistingPath(rootResolved, normalizedNotePath, false, "note_not_found", "Note not found.")
	if apiErr != nil {
		return NoteDocument{}, apiErr
	}

	rawMarkdownBytes, err := os.ReadFile(absolutePath)
	if err != nil {
		return NoteDocument{}, apierror.Wrap(503, "vault_unavailable", "Failed to read note.", err)
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		return NoteDocument{}, apierror.Wrap(503, "vault_unavailable", "Failed to stat note.", err)
	}

	rawMarkdown := string(rawMarkdownBytes)
	parsed := ParseMarkdown(rawMarkdown)
	summary := buildNoteSummary(relativePath, rawMarkdown, parsed.Frontmatter, info.ModTime())
	return NoteDocument{
		NoteSummary: summary,
		Content:     parsed.Content,
		RawMarkdown: rawMarkdown,
	}, nil
}

func ParseMarkdown(raw string) ParsedMarkdown {
	frontmatterBody, content, ok := splitFrontmatter(raw)
	if !ok {
		return ParsedMarkdown{
			Frontmatter: map[string]any{},
			Content:     strings.ReplaceAll(raw, "\r\n", "\n"),
		}
	}

	return ParsedMarkdown{
		Frontmatter: parseFrontmatterBody(frontmatterBody),
		Content:     content,
	}
}

func ParseFrontmatterBody(raw string) map[string]any {
	return parseFrontmatterBody(raw)
}

func Search(rootPath, query string, limit int, folderPrefixes []string) ([]SearchHit, error) {
	rootResolved, err := resolveRoot(rootPath)
	if err != nil {
		return nil, err
	}
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return nil, apierror.New(400, "invalid_request", "query is required")
	}
	if limit <= 0 {
		limit = 20
	}

	normalizedPrefixes := make([]string, 0, len(folderPrefixes))
	for _, prefix := range folderPrefixes {
		normalizedPrefix, prefixErr := normalizeRelativePath(prefix, false)
		if prefixErr != nil {
			return nil, prefixErr
		}
		normalizedPrefixes = append(normalizedPrefixes, strings.TrimSuffix(normalizedPrefix, "/"))
	}

	notePaths, apiErr := walkMarkdownFiles(rootResolved, "", true)
	if apiErr != nil {
		return nil, apiErr
	}

	results := make([]SearchHit, 0, limit)
	for _, notePath := range notePaths {
		if !matchesAnyPrefix(notePath, normalizedPrefixes) {
			continue
		}

		document, readErr := ReadNoteDocumentFromFile(rootResolved, notePath)
		if readErr != nil {
			return nil, readErr
		}

		score, matchIndex := lexicalScore(trimmedQuery, document.Title, document.Content)
		if score <= 0 {
			continue
		}

		lowerTitle := strings.ToLower(document.Title)
		lowerQuery := strings.ToLower(trimmedQuery)
		if strings.Contains(lowerTitle, lowerQuery) && len(results) < limit {
			results = append(results, SearchHit{
				Path:       document.Path,
				Name:       document.Name,
				Title:      document.Title,
				FolderPath: document.FolderPath,
				UpdatedAt:  document.UpdatedAt,
				Checksum:   document.Checksum,
				LineNumber: 1,
				Snippet:    document.Title,
				MatchType:  "title",
			})
		}

		contentLines := strings.Split(document.Content, "\n")
		lineNumber := firstMatchingLine(contentLines, lowerQuery)
		if lineNumber > 0 && len(results) < limit {
			results = append(results, SearchHit{
				Path:       document.Path,
				Name:       document.Name,
				Title:      document.Title,
				FolderPath: document.FolderPath,
				UpdatedAt:  document.UpdatedAt,
				Checksum:   document.Checksum,
				LineNumber: lineNumber,
				Snippet:    buildSnippet(document.Content, trimmedQuery, matchIndex, 180),
				MatchType:  "content",
			})
		}

		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

func buildNoteSummary(notePath, rawMarkdown string, frontmatter map[string]any, modTime time.Time) NoteSummary {
	if frontmatter == nil {
		frontmatter = map[string]any{}
	}

	return NoteSummary{
		Path:        notePath,
		Name:        path.Base(notePath),
		Title:       resolveTitle(rawMarkdown, frontmatter, notePath),
		FolderPath:  folderPath(notePath),
		UpdatedAt:   modTime.UTC().Format(time.RFC3339),
		Checksum:    checksum(rawMarkdown),
		Frontmatter: frontmatter,
	}
}

func resolveTitle(rawMarkdown string, frontmatter map[string]any, notePath string) string {
	if frontmatter != nil {
		if title, ok := frontmatter["title"].(string); ok && strings.TrimSpace(title) != "" {
			return strings.TrimSpace(title)
		}
	}

	for _, line := range strings.Split(strings.ReplaceAll(rawMarkdown, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}

	return strings.TrimSuffix(path.Base(notePath), markdownExtension)
}

func splitFrontmatter(raw string) (string, string, bool) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", normalized, false
	}

	for index := 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed != "---" && trimmed != "..." {
			continue
		}

		body := strings.Join(lines[1:index], "\n")
		content := strings.Join(lines[index+1:], "\n")
		content = strings.TrimPrefix(content, "\n")
		return body, content, true
	}

	return "", normalized, false
}

func parseFrontmatterBody(raw string) map[string]any {
	result := map[string]any{}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")

	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		colonIndex := strings.Index(line, ":")
		if colonIndex <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:colonIndex])
		if key == "" {
			continue
		}

		remainder := strings.TrimSpace(line[colonIndex+1:])
		if remainder != "" {
			result[key] = parseScalarValue(remainder)
			continue
		}

		arrayValues := make([]any, 0)
		nextIndex := index + 1
		for nextIndex < len(lines) {
			nextLine := lines[nextIndex]
			nextTrimmed := strings.TrimSpace(nextLine)
			if nextTrimmed == "" {
				nextIndex++
				continue
			}
			if !strings.HasPrefix(strings.TrimSpace(nextLine), "- ") {
				break
			}
			item := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(nextLine), "- "))
			arrayValues = append(arrayValues, parseScalarValue(item))
			nextIndex++
		}
		if len(arrayValues) > 0 {
			result[key] = arrayValues
			index = nextIndex - 1
			continue
		}

		result[key] = nil
	}

	return result
}

func parseScalarValue(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) >= 2 {
		if (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') || (trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') {
			return trimmed[1 : len(trimmed)-1]
		}
	}

	switch strings.ToLower(trimmed) {
	case "true":
		return true
	case "false":
		return false
	case "null", "~":
		return nil
	}

	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		body := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
		if body == "" {
			return []any{}
		}
		parts := strings.Split(body, ",")
		values := make([]any, 0, len(parts))
		for _, part := range parts {
			values = append(values, parseScalarValue(part))
		}
		return values
	}

	if number, err := strconv.Atoi(trimmed); err == nil {
		return number
	}
	if number, err := strconv.ParseFloat(trimmed, 64); err == nil && strings.Contains(trimmed, ".") {
		return number
	}
	return trimmed
}

func normalizeRelativePath(raw string, allowEmpty bool) (string, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if trimmed == "" {
		if allowEmpty {
			return "", nil
		}
		return "", apierror.New(400, "invalid_request", "path is required")
	}
	if strings.ContainsRune(trimmed, '\x00') || strings.HasPrefix(trimmed, "/") {
		return "", apierror.New(400, "unsafe_path", "absolute paths are not allowed")
	}

	normalized := path.Clean(trimmed)
	if normalized == "." {
		if allowEmpty {
			return "", nil
		}
		return "", apierror.New(400, "invalid_request", "path is required")
	}
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", apierror.New(400, "unsafe_path", "path traversal is not allowed")
	}

	return strings.TrimPrefix(normalized, "./"), nil
}

func resolveRoot(rootPath string) (string, error) {
	if strings.TrimSpace(rootPath) == "" {
		return "", apierror.New(503, "vault_unavailable", "Vault path is not configured.")
	}

	absoluteRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return "", apierror.Wrap(503, "vault_unavailable", "Failed to resolve vault path.", err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", apierror.New(503, "vault_unavailable", "Vault path does not exist.")
		}
		return "", apierror.Wrap(503, "vault_unavailable", "Failed to resolve vault path.", err)
	}

	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return "", apierror.Wrap(503, "vault_unavailable", "Failed to access vault path.", err)
	}
	if !info.IsDir() {
		return "", apierror.New(503, "vault_unavailable", "Vault path must be a directory.")
	}

	return filepath.Clean(resolvedRoot), nil
}

func resolveExistingPath(rootResolved, relativePath string, expectDir bool, notFoundCode, notFoundMessage string) (string, string, *apierror.Error) {
	targetPath := rootResolved
	if relativePath != "" {
		targetPath = filepath.Join(rootResolved, filepath.FromSlash(relativePath))
	}

	resolvedPath, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", apierror.New(404, notFoundCode, notFoundMessage)
		}
		return "", "", apierror.Wrap(503, "vault_unavailable", "Failed to resolve vault path.", err)
	}
	if !isWithinRoot(rootResolved, resolvedPath) {
		return "", "", apierror.New(400, "unsafe_path", "path escapes the configured vault")
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", apierror.New(404, notFoundCode, notFoundMessage)
		}
		return "", "", apierror.Wrap(503, "vault_unavailable", "Failed to access vault path.", err)
	}
	if info.IsDir() != expectDir {
		return "", "", apierror.New(404, notFoundCode, notFoundMessage)
	}

	relativeResolved, err := filepath.Rel(rootResolved, resolvedPath)
	if err != nil {
		return "", "", apierror.Wrap(503, "vault_unavailable", "Failed to resolve vault path.", err)
	}
	if relativeResolved == "." {
		relativeResolved = ""
	}
	return resolvedPath, filepath.ToSlash(relativeResolved), nil
}

func walkMarkdownFiles(basePath, relativeBase string, recursive bool) ([]string, *apierror.Error) {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, apierror.New(404, "folder_not_found", "Folder not found.")
		}
		return nil, apierror.Wrap(503, "vault_unavailable", "Failed to read vault contents.", err)
	}

	results := make([]string, 0)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		relativePath := joinRelative(relativeBase, entry.Name())
		if entry.IsDir() {
			if recursive {
				childPath := filepath.Join(basePath, entry.Name())
				childResults, childErr := walkMarkdownFiles(childPath, relativePath, true)
				if childErr != nil {
					return nil, childErr
				}
				results = append(results, childResults...)
			}
			continue
		}
		if entry.Type().IsRegular() && isMarkdownFile(entry.Name()) {
			results = append(results, relativePath)
		}
	}

	sort.Strings(results)
	return results, nil
}

func checksum(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func folderPath(notePath string) string {
	dir := path.Dir(notePath)
	if dir == "." {
		return ""
	}
	return dir
}

func joinRelative(base, name string) string {
	if base == "" {
		return name
	}
	return path.Join(base, name)
}

func isMarkdownFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), markdownExtension)
}

func isWithinRoot(rootResolved, targetResolved string) bool {
	relativePath, err := filepath.Rel(rootResolved, targetResolved)
	if err != nil {
		return false
	}
	return relativePath == "." || (!strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) && relativePath != "..")
}

func lexicalScore(query, title, content string) (float64, int) {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return 0, -1
	}

	lowerTitle := strings.ToLower(title)
	lowerContent := strings.ToLower(content)
	score := 0.0
	firstMatch := strings.Index(lowerContent, strings.ToLower(strings.TrimSpace(query)))

	for _, term := range terms {
		score += float64(strings.Count(lowerTitle, term)*2 + strings.Count(lowerContent, term))
		if firstMatch < 0 {
			firstMatch = strings.Index(lowerContent, term)
		}
	}
	return score, firstMatch
}

func queryTerms(query string) []string {
	parts := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	seen := make(map[string]struct{}, len(parts))
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		terms = append(terms, part)
	}
	return terms
}

func buildSnippet(content, query string, matchIndex, maxChars int) string {
	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		return ""
	}

	runes := []rune(trimmedContent)
	if len(runes) <= maxChars {
		return trimmedContent
	}

	runeMatch := 0
	if matchIndex > 0 && matchIndex <= len(trimmedContent) {
		runeMatch = len([]rune(trimmedContent[:matchIndex]))
	}
	start := runeMatch - maxChars/4
	if start < 0 {
		start = 0
	}
	end := start + maxChars
	if end > len(runes) {
		end = len(runes)
	}
	snippet := strings.TrimSpace(string(runes[start:end]))
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runes) {
		snippet += "..."
	}
	return snippet
}

func firstMatchingLine(lines []string, query string) int {
	for index, line := range lines {
		if strings.Contains(strings.ToLower(line), query) {
			return index + 1
		}
	}
	return 0
}

func matchesAnyPrefix(notePath string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, prefix := range prefixes {
		if notePath == prefix || strings.HasPrefix(notePath, prefix+"/") {
			return true
		}
	}
	return false
}

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ClaudeMdType indicates the source type of a CLAUDE.md file
type ClaudeMdType string

const (
	ClaudeMdTypeProject ClaudeMdType = "project"
	ClaudeMdTypeLocal   ClaudeMdType = "local"
	ClaudeMdTypeUser    ClaudeMdType = "user"
	ClaudeMdTypeManaged ClaudeMdType = "managed"
	ClaudeMdTypeAutoMem ClaudeMdType = "automem"
)

// ClaudeMdFile represents a loaded CLAUDE.md file
type ClaudeMdFile struct {
	Path    string
	Content string
	Type    ClaudeMdType
}

const maxIncludeDepth = 5

var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

// LoadClaudeMds loads all CLAUDE.md files and returns them in priority order (lowest first)
// The caller should format them into the system-reminder user message.
func LoadClaudeMds(cwd string) ([]ClaudeMdFile, error) {
	var files []ClaudeMdFile
	processed := make(map[string]bool)

	// 1. Managed: /etc/claude-code/CLAUDE.md (lowest priority)
	for _, p := range managedClaudeMdPaths() {
		if f, err := loadSingleClaudeMd(p, ClaudeMdTypeManaged, processed, 0); err == nil {
			files = append(files, f...)
		}
	}

	// 2. User: ~/.claude/CLAUDE.md
	userDir := userConfigDir()
	if f, err := loadSingleClaudeMd(filepath.Join(userDir, "CLAUDE.md"), ClaudeMdTypeUser, processed, 0); err == nil {
		files = append(files, f...)
	}
	// User rules: ~/.claude/rules/*.md
	files = append(files, loadRulesDir(filepath.Join(userDir, "rules"), ClaudeMdTypeUser, processed)...)

	// 3. Project: walk from git root (or filesystem root) to cwd
	projectFiles, err := loadProjectClaudeMds(cwd, processed)
	if err == nil {
		files = append(files, projectFiles...)
	}

	// 4. AutoMem: ~/.claude/projects/{slug}/memory/MEMORY.md
	if autoMem, err := loadAutoMem(cwd); err == nil && autoMem != nil {
		files = append(files, *autoMem)
	}

	return files, nil
}

// FormatClaudeMdMessage formats CLAUDE.md files as the system-reminder user message content
func FormatClaudeMdMessage(files []ClaudeMdFile, date string) string {
	var sb strings.Builder
	sb.WriteString("As you answer the user's questions, you can use the following context:\n")
	sb.WriteString("# claudeMd\n")
	sb.WriteString("Codebase and user instructions are shown below. Be sure to adhere to these instructions. IMPORTANT: These instructions OVERRIDE any default behavior and you MUST follow them exactly as written.\n\n")

	for _, f := range files {
		sb.WriteString(fmt.Sprintf("Contents of %s (%s):\n\n", f.Path, claudeMdDescription(f.Type)))
		sb.WriteString(f.Content)
		sb.WriteString("\n\n")
	}

	sb.WriteString(fmt.Sprintf("# currentDate\nToday's date is %s.\n\n", date))
	sb.WriteString("      IMPORTANT: this context may or may not be relevant to your tasks. You should not respond to this context unless it is highly relevant to your task.\n")

	return sb.String()
}

func claudeMdDescription(t ClaudeMdType) string {
	switch t {
	case ClaudeMdTypeProject:
		return "project instructions, checked into the codebase"
	case ClaudeMdTypeLocal:
		return "user's private project instructions, not checked in"
	case ClaudeMdTypeAutoMem:
		return "user's auto-memory, persists across conversations"
	default:
		return "user's private global instructions for all projects"
	}
}

func loadSingleClaudeMd(path string, t ClaudeMdType, processed map[string]bool, depth int) ([]ClaudeMdFile, error) {
	if depth > maxIncludeDepth {
		return nil, fmt.Errorf("max include depth exceeded")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if processed[abs] {
		return nil, nil
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	processed[abs] = true

	content := stripHTMLComments(string(data))
	var result []ClaudeMdFile

	// Process @include directives
	lines := strings.Split(content, "\n")
	var kept []string
	dir := filepath.Dir(abs)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@include ") {
			includePath := strings.TrimPrefix(trimmed, "@include ")
			includePath = strings.TrimSpace(includePath)
			if !filepath.IsAbs(includePath) {
				includePath = filepath.Join(dir, includePath)
			}
			if included, err := loadSingleClaudeMd(includePath, t, processed, depth+1); err == nil {
				result = append(result, included...)
			}
		} else {
			kept = append(kept, line)
		}
	}

	finalContent := strings.Join(kept, "\n")
	finalContent = strings.TrimSpace(finalContent)
	if finalContent != "" {
		result = append(result, ClaudeMdFile{Path: abs, Content: finalContent, Type: t})
	}
	return result, nil
}

func loadRulesDir(dir string, t ClaudeMdType, processed map[string]bool) []ClaudeMdFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)

	var files []ClaudeMdFile
	for _, p := range paths {
		if f, err := loadSingleClaudeMd(p, t, processed, 0); err == nil {
			files = append(files, f...)
		}
	}
	return files
}

func loadProjectClaudeMds(cwd string, processed map[string]bool) ([]ClaudeMdFile, error) {
	// Find git root or use cwd as base
	root := findGitRoot(cwd)
	if root == "" {
		root = cwd
	}

	// Collect all directories from root to cwd
	dirs := []string{}
	current := cwd
	for {
		dirs = append([]string{current}, dirs...)
		if current == root {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	var files []ClaudeMdFile
	for _, dir := range dirs {
		// {dir}/CLAUDE.md - project type
		if f, err := loadSingleClaudeMd(filepath.Join(dir, "CLAUDE.md"), ClaudeMdTypeProject, processed, 0); err == nil {
			files = append(files, f...)
		}
		// {dir}/.claude/CLAUDE.md - project type
		if f, err := loadSingleClaudeMd(filepath.Join(dir, ".claude", "CLAUDE.md"), ClaudeMdTypeProject, processed, 0); err == nil {
			files = append(files, f...)
		}
		// {dir}/.claude/rules/*.md - project type
		files = append(files, loadRulesDir(filepath.Join(dir, ".claude", "rules"), ClaudeMdTypeProject, processed)...)
		// {dir}/CLAUDE.local.md - local type
		if f, err := loadSingleClaudeMd(filepath.Join(dir, "CLAUDE.local.md"), ClaudeMdTypeLocal, processed, 0); err == nil {
			files = append(files, f...)
		}
	}
	return files, nil
}

func loadAutoMem(cwd string) (*ClaudeMdFile, error) {
	root := findGitRoot(cwd)
	if root == "" {
		root = cwd
	}
	slug := sanitizePath(root)
	userDir := userConfigDir()
	path := filepath.Join(userDir, "projects", slug, "memory", "MEMORY.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(stripHTMLComments(string(data)))
	if content == "" {
		return nil, nil
	}
	return &ClaudeMdFile{Path: path, Content: content, Type: ClaudeMdTypeAutoMem}, nil
}

func findGitRoot(dir string) string {
	current := dir
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func sanitizePath(path string) string {
	// Replace path separators with "-"
	result := strings.ReplaceAll(path, string(filepath.Separator), "-")
	// Remove leading dash
	result = strings.TrimPrefix(result, "-")
	return result
}

func stripHTMLComments(s string) string {
	return htmlCommentRe.ReplaceAllString(s, "")
}

func managedClaudeMdPaths() []string {
	return []string{"/etc/claude-code/CLAUDE.md"}
}

// CurrentDate returns today's date in YYYY-MM-DD format
func CurrentDate() string {
	return time.Now().Format("2006-01-02")
}

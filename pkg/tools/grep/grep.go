// Package grep implements the Grep tool
package grep

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hankwenyx/claude-code-go/pkg/config"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

func openFile(path string) (*os.File, error) {
	return os.Open(path)
}

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {
      "type": "string",
      "description": "The regular expression pattern to search for"
    },
    "path": {
      "type": "string",
      "description": "File or directory to search in (default: current directory)"
    },
    "glob": {
      "type": "string",
      "description": "Glob pattern to filter files (e.g. \"*.go\")"
    },
    "-i": {
      "type": "boolean",
      "description": "Case insensitive search"
    },
    "output_mode": {
      "type": "string",
      "enum": ["content", "files_with_matches", "count"],
      "description": "Output mode (default: files_with_matches)"
    }
  },
  "required": ["pattern"]
}`)

// Tool implements the Grep tool
type Tool struct {
	CWD string
}

// New creates a new Grep tool
func New(cwd string) *Tool {
	return &Tool{CWD: cwd}
}

func (t *Tool) Name() string                 { return "Grep" }
func (t *Tool) IsReadOnly() bool             { return true }
func (t *Tool) InputSchema() json.RawMessage { return inputSchema }
func (t *Tool) Description() string {
	return `Search for patterns in files using ripgrep (rg) if available, fallback to Go implementation. Supports regex, file globs, and output modes.`
}

type input struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	Glob       string `json:"glob,omitempty"`
	CaseInsens bool   // "-i" key — parsed manually below because "-" is not a valid JSON tag char
	OutputMode string `json:"output_mode,omitempty"`
}

func (t *Tool) Call(ctx context.Context, rawInput json.RawMessage) (tools.ToolResult, error) {
	// Parse manually to handle "-i" key
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rawInput, &raw); err != nil {
		return tools.ToolResult{IsError: true, Content: "invalid input: " + err.Error()}, nil
	}

	in := input{}
	if v, ok := raw["pattern"]; ok {
		json.Unmarshal(v, &in.Pattern)
	}
	if v, ok := raw["path"]; ok {
		json.Unmarshal(v, &in.Path)
	}
	if v, ok := raw["glob"]; ok {
		json.Unmarshal(v, &in.Glob)
	}
	if v, ok := raw["-i"]; ok {
		json.Unmarshal(v, &in.CaseInsens)
	}
	if v, ok := raw["output_mode"]; ok {
		json.Unmarshal(v, &in.OutputMode)
	}

	if in.Pattern == "" {
		return tools.ToolResult{IsError: true, Content: "pattern is required"}, nil
	}

	searchPath := in.Path
	if searchPath == "" {
		searchPath = t.CWD
	}
	if searchPath == "" {
		searchPath = "."
	}

	// Try ripgrep first
	if result, err := t.runRipgrep(ctx, in, searchPath); err == nil {
		return result, nil
	}

	// Fallback to Go implementation
	return t.runGoGrep(ctx, in, searchPath)
}

func (t *Tool) runRipgrep(ctx context.Context, in input, searchPath string) (tools.ToolResult, error) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("rg not found")
	}

	args := []string{in.Pattern, searchPath, "--no-heading"}

	switch in.OutputMode {
	case "files_with_matches":
		args = append(args, "-l")
	case "count":
		args = append(args, "-c")
	default:
		// content mode with line numbers
		args = append(args, "-n")
	}

	if in.CaseInsens {
		args = append(args, "-i")
	}
	if in.Glob != "" {
		args = append(args, "--glob", in.Glob)
	}

	// Exclude .git
	args = append(args, "--glob", "!.git/**")

	cmd := exec.CommandContext(ctx, rgPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	output := stdout.String()

	// Exit code 1 means no matches (not an error)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return tools.ToolResult{Content: "no matches found"}, nil
			}
		}
		return tools.ToolResult{}, fmt.Errorf("rg failed: %s", stderr.String())
	}

	if output == "" {
		return tools.ToolResult{Content: "no matches found"}, nil
	}
	return tools.ToolResult{Content: output}, nil
}

func (t *Tool) runGoGrep(ctx context.Context, in input, searchPath string) (tools.ToolResult, error) {
	// Compile the regex (honour case-insensitive flag via inline flag)
	pattern := in.Pattern
	if in.CaseInsens {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return tools.ToolResult{IsError: true, Content: "invalid regex: " + err.Error()}, nil
	}

	var matches strings.Builder
	fileCount := 0 // files_with_matches
	lineCount := 0 // count mode (total matching lines across all files)

	err = filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply glob filter
		if in.Glob != "" {
			matched, _ := filepath.Match(in.Glob, filepath.Base(path))
			if !matched {
				return nil
			}
		}

		f, err := openFile(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		fileMatched := false
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				lineCount++
				switch in.OutputMode {
				case "files_with_matches":
					if !fileMatched {
						fileMatched = true
						fileCount++
						matches.WriteString(path + "\n")
					}
					// continue scanning to count remaining lines (not needed here)
					// but we can stop early for this file
					return nil
				case "count":
					// keep scanning, accumulate lineCount
				default: // content
					matches.WriteString(fmt.Sprintf("%s:%d:%s\n", path, lineNum, line))
				}
			}
		}
		return nil
	})

	if err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("search error: %v", err)}, nil
	}

	switch in.OutputMode {
	case "count":
		return tools.ToolResult{Content: fmt.Sprintf("%d", lineCount)}, nil
	case "files_with_matches":
		if fileCount == 0 {
			return tools.ToolResult{Content: "no matches found"}, nil
		}
		return tools.ToolResult{Content: matches.String()}, nil
	default:
		result := matches.String()
		if result == "" {
			return tools.ToolResult{Content: "no matches found"}, nil
		}
		return tools.ToolResult{Content: result}, nil
	}
}

func (t *Tool) CheckPermissions(rawInput json.RawMessage, mode string, rules tools.PermissionRules) tools.PermissionDecision {
	cfgRules := config.PermissionRules{Allow: rules.Allow, Deny: rules.Deny, Ask: rules.Ask}
	var raw map[string]json.RawMessage
	json.Unmarshal(rawInput, &raw)
	var path string
	if v, ok := raw["path"]; ok {
		json.Unmarshal(v, &path)
	}
	for _, rule := range cfgRules.Deny {
		if config.MatchRule(rule, t.Name(), path) {
			return tools.PermissionDecision{Behavior: "deny", Reason: "matched deny rule: " + rule}
		}
	}
	return tools.PermissionDecision{Behavior: "allow", Reason: "read-only default allow"}
}

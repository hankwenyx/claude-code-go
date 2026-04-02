package api

import "strings"

// BuildOptions configures system prompt construction
type BuildOptions struct {
	ClaudeMdContent string   // formatted CLAUDE.md system-reminder content
	CWD             string
	EnabledTools    []string // tool names to list in prompt
}

// BuildSystemPrompt constructs the system prompt as SystemBlocks.
// Block 1 (static, cached): role + behavior rules + tool usage guidance
// Block 2 (dynamic, not cached): CLAUDE.md content + CWD + date
func BuildSystemPrompt(opts BuildOptions) []SystemBlock {
	var blocks []SystemBlock

	// Block 1: Static content with cache_control
	static := buildStaticBlock(opts.EnabledTools)
	blocks = append(blocks, SystemBlock{
		Type: "text",
		Text: static,
		CacheControl: &CacheControl{Type: "ephemeral"},
	})

	// Block 2: Dynamic content (no cache)
	if dynamic := buildDynamicBlock(opts); dynamic != "" {
		blocks = append(blocks, SystemBlock{
			Type: "text",
			Text: dynamic,
		})
	}

	return blocks
}

func buildStaticBlock(enabledTools []string) string {
	var sb strings.Builder

	sb.WriteString(`You are Claude Code Go, an interactive CLI agent for software engineering tasks — a Go implementation of Claude Code.

# Core Principles
- Assist with authorized security testing, CTF challenges, and educational security contexts
- Write safe, secure, and correct code; avoid OWASP top 10 vulnerabilities
- Only make changes that are directly requested or clearly necessary
- Verify existing code before suggesting modifications
- Trust internal code and framework guarantees; only validate at system boundaries

# Behavior
- Keep responses short and concise
- Use Github-flavored markdown for formatting
- Reference file paths with line numbers: file_path:line_number
- Do not use emojis unless explicitly requested

# Tool Usage
`)

	if len(enabledTools) > 0 {
		sb.WriteString("Available tools: ")
		sb.WriteString(strings.Join(enabledTools, ", "))
		sb.WriteString("\n\n")
	}

	sb.WriteString(`- Use Read before Edit to understand existing code
- Use Glob and Grep for file discovery
- Prefer editing existing files over creating new ones
- Use Bash for system commands and terminal operations
`)

	return sb.String()
}

func buildDynamicBlock(opts BuildOptions) string {
	var parts []string

	if opts.CWD != "" {
		parts = append(parts, "Current working directory: "+opts.CWD)
	}

	combined := strings.Join(parts, "\n")
	return combined
}

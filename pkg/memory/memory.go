// Package memory implements the persistent memory system.
//
// It provides:
//   - Session compaction: summarise long conversations to reduce token usage
//   - AutoMem: extract cross-session memories to MEMORY.md
//   - MEMORY.md management: 200-line / 25KB truncation, atomic two-phase write
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/api"
)

const (
	// MaxMemoryLines is the maximum number of lines kept in MEMORY.md.
	MaxMemoryLines = 200
	// MaxMemoryBytes is the maximum byte size of MEMORY.md before truncation.
	MaxMemoryBytes = 25 * 1024 // 25 KB

	compactSystemPrompt = `You are a conversation summariser for an AI coding assistant.
Produce a concise summary of the conversation so far, preserving:
- The original task / goal
- Key decisions and code changes made
- Pending tasks and open questions
- Any important context needed to continue

Keep the summary under 2000 tokens. Output plain text, no JSON.`

	autoMemSystemPrompt = `You are a memory extractor for an AI coding assistant.
Read the conversation summary below and extract facts that should be remembered across sessions.
Focus on:
- User preferences (tools, languages, frameworks, style)
- Project conventions (naming, patterns, architecture)
- Recurring problems and their solutions
- Explicit "remember this" instructions

Output a markdown bullet list starting each item with "- ".
Output NOTHING else — no headers, no preamble.`
)

// streamText calls client.StreamMessage and collects all text deltas.
// Returns the concatenated text or an error.
func streamText(ctx context.Context, client *api.Client, msgs []api.APIMessage, system []api.SystemBlock) (string, error) {
	req := api.CreateMessageRequest{
		Messages:  msgs,
		System:    system,
		MaxTokens: 4096,
		Stream:    true,
	}
	ch, err := client.StreamMessage(ctx, req)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			return "", chunk.Error
		}
		if chunk.Type == "content_block_delta" {
			if ev, ok := chunk.Data.(api.ContentBlockDeltaEvent); ok && ev.Delta.Type == "text_delta" {
				buf.WriteString(ev.Delta.Text)
			}
		}
	}
	return strings.TrimSpace(buf.String()), nil
}

// CompactMessages summarises a conversation using the provided client.
// Returns the compacted message list and the summary text.
func CompactMessages(ctx context.Context, client *api.Client, messages []api.APIMessage) ([]api.APIMessage, string, error) {
	if len(messages) == 0 {
		return messages, "", nil
	}

	// Build plain-text transcript
	var transcript strings.Builder
	for _, m := range messages {
		var blocks []api.ContentBlock
		if err := json.Unmarshal(m.Content, &blocks); err == nil {
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" {
					fmt.Fprintf(&transcript, "[%s]: %s\n\n", m.Role, b.Text)
				}
			}
		}
	}

	summaryMsgs := []api.APIMessage{
		api.NewUserTextMessage("Summarise this conversation:\n\n" + transcript.String()),
	}
	sysp := []api.SystemBlock{{Type: "text", Text: compactSystemPrompt}}

	summaryText, err := streamText(ctx, client, summaryMsgs, sysp)
	if err != nil {
		return messages, "", fmt.Errorf("compact: %w", err)
	}
	if summaryText == "" {
		return messages, "", fmt.Errorf("compact: empty summary returned")
	}

	// Replace history with a compact-summary user message
	compacted := "<compact-summary>\n" + summaryText + "\n</compact-summary>"
	return []api.APIMessage{api.NewUserTextMessage(compacted)}, summaryText, nil
}

// ExtractMemories uses the LLM to extract persistent facts from a summary.
// Returns a markdown bullet list.
func ExtractMemories(ctx context.Context, client *api.Client, summary string) (string, error) {
	msgs := []api.APIMessage{
		api.NewUserTextMessage("Conversation summary:\n\n" + summary),
	}
	sysp := []api.SystemBlock{{Type: "text", Text: autoMemSystemPrompt}}
	return streamText(ctx, client, msgs, sysp)
}

// MemoryPath returns the path to the project's MEMORY.md.
// cwd is used to derive the project slug.
func MemoryPath(cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	slug := CWDSlug(cwd)
	return filepath.Join(home, ".claude", "projects", slug, "memory", "MEMORY.md"), nil
}

// AppendMemory adds new bullet points to the project's MEMORY.md,
// then truncates to MaxMemoryLines / MaxMemoryBytes using a two-phase atomic write.
func AppendMemory(cwd, newBullets string) error {
	path, err := MemoryPath(cwd)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	var existing string
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}

	var sb strings.Builder
	if existing != "" {
		sb.WriteString(strings.TrimRight(existing, "\n"))
		sb.WriteString("\n\n")
	}
	sb.WriteString("<!-- updated: ")
	sb.WriteString(time.Now().UTC().Format(time.RFC3339))
	sb.WriteString(" -->\n")
	sb.WriteString(strings.TrimSpace(newBullets))
	sb.WriteString("\n")

	truncated := TruncateMemory(sb.String())
	return atomicWrite(path, []byte(truncated))
}

// LoadMemory reads the project's MEMORY.md and returns its (truncated) content.
// Returns "" if the file does not exist.
func LoadMemory(cwd string) (string, error) {
	path, err := MemoryPath(cwd)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return TruncateMemory(string(data)), nil
}

// TruncateMemory enforces the 200-line / 25KB limits, dropping the oldest lines.
func TruncateMemory(content string) string {
	if len(content) <= MaxMemoryBytes && lineCount(content) <= MaxMemoryLines {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) > MaxMemoryLines {
		lines = lines[len(lines)-MaxMemoryLines:]
	}
	for len(lines) > 1 {
		joined := strings.Join(lines, "\n")
		if len(joined) <= MaxMemoryBytes {
			return joined
		}
		lines = lines[1:]
	}
	// Hard truncate single remaining line
	joined := strings.Join(lines, "\n")
	if len(joined) > MaxMemoryBytes {
		return joined[:MaxMemoryBytes]
	}
	return joined
}

// atomicWrite writes data to path via a temp-file + rename (two-phase write).
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// CWDSlug converts a filesystem path to a directory-name-safe slug by
// replacing path separators with '-'.
func CWDSlug(cwd string) string {
	slug := strings.NewReplacer("/", "-", "\\", "-", ":", "").Replace(cwd)
	return strings.TrimPrefix(slug, "-")
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// Package webfetch implements the WebFetch tool
package webfetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/hankwenyx/claude-code-go/pkg/config"
	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

const (
	maxContentChars = 100_000
	cacheTTL        = 15 * time.Minute
)

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {
      "type": "string",
      "description": "The URL to fetch content from"
    },
    "prompt": {
      "type": "string",
      "description": "What information to extract from the page"
    }
  },
  "required": ["url", "prompt"]
}`)

type cacheEntry struct {
	content   string
	expiresAt time.Time
}

// Tool implements the WebFetch tool
type Tool struct {
	HTTPClient *http.Client
	cache      sync.Map // url → cacheEntry
}

// New creates a new WebFetch tool
func New() *Tool {
	return &Tool{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *Tool) Name() string                { return "WebFetch" }
func (t *Tool) IsReadOnly() bool            { return true }
func (t *Tool) InputSchema() json.RawMessage { return inputSchema }
func (t *Tool) Description() string {
	return `Fetch content from a URL. Converts HTML to markdown. Caches responses for 15 minutes. Content > 100,000 characters is truncated.`
}

type input struct {
	URL    string `json:"url"`
	Prompt string `json:"prompt"`
}

func (t *Tool) Call(ctx context.Context, rawInput json.RawMessage) (tools.ToolResult, error) {
	var in input
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return tools.ToolResult{IsError: true, Content: "invalid input: " + err.Error()}, nil
	}

	if in.URL == "" {
		return tools.ToolResult{IsError: true, Content: "url is required"}, nil
	}

	// Upgrade HTTP to HTTPS
	url := in.URL
	if strings.HasPrefix(url, "http://") {
		url = "https://" + url[7:]
	}

	// Check cache
	if entry, ok := t.cache.Load(url); ok {
		e := entry.(cacheEntry)
		if time.Now().Before(e.expiresAt) {
			return tools.ToolResult{Content: e.content}, nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("invalid URL: %v", err)}, nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 Claude-Code/1.0")

	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("fetch error: %v", err)}, nil
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("read error: %v", err)}, nil
	}

	content := convertHTML(string(bodyBytes))
	if len(content) > maxContentChars {
		content = content[:maxContentChars] + "\n... (content truncated)"
	}

	// Cache result
	t.cache.Store(url, cacheEntry{content: content, expiresAt: time.Now().Add(cacheTTL)})

	return tools.ToolResult{Content: content}, nil
}

func (t *Tool) CheckPermissions(rawInput json.RawMessage, mode string, rules tools.PermissionRules) tools.PermissionDecision {
	cfgRules := config.PermissionRules{Allow: rules.Allow, Deny: rules.Deny, Ask: rules.Ask}
	var in input
	json.Unmarshal(rawInput, &in)

	for _, rule := range cfgRules.Deny {
		if config.MatchRule(rule, t.Name(), in.URL) {
			return tools.PermissionDecision{Behavior: "deny", Reason: "matched deny rule: " + rule}
		}
	}
	for _, rule := range cfgRules.Allow {
		if config.MatchRule(rule, t.Name(), in.URL) {
			return tools.PermissionDecision{Behavior: "allow", Reason: "matched allow rule: " + rule}
		}
	}
	return tools.PermissionDecision{Behavior: "allow", Reason: "read-only default allow"}
}

// convertHTML converts HTML content to Markdown using html-to-markdown/v2.
func convertHTML(html string) string {
	md, err := htmltomarkdown.ConvertString(html)
	if err != nil {
		// Fallback: strip tags
		return stripTags(html)
	}
	return md
}

// stripTags is a minimal fallback HTML stripper.
func stripTags(html string) string {
	var sb strings.Builder
	inTag := false
	for _, r := range html {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				sb.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

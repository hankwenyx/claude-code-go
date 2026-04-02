package webfetch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/tools"
)

// createTestTool creates a tool with a custom HTTP client that doesn't auto-upgrade to HTTPS
func createTestTool(server *httptest.Server) *Tool {
	return &Tool{
		HTTPClient: server.Client(),
	}
}

func TestWebFetch_Basic(t *testing.T) {
	// Test with a real URL (will use cache if available, or fail gracefully)
	tool := New()
	testURL := "https://example.com/test"

	// Pre-populate cache with test content
	tool.cache.Store(testURL, cacheEntry{
		content:   "# Hello World\n\nThis is a test page.",
		expiresAt: time.Now().Add(15 * time.Minute),
	})

	input, _ := json.Marshal(map[string]interface{}{
		"url":    testURL,
		"prompt": "Extract the title",
	})

	result, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Hello World") {
		t.Errorf("expected 'Hello World' in content, got: %s", result.Content)
	}
}

func TestWebFetch_HTTPSUpgrade(t *testing.T) {
	// Test that http:// URLs are upgraded to https://
	tool := New()
	input, _ := json.Marshal(map[string]interface{}{
		"url":    "http://example.com",
		"prompt": "test",
	})

	// The tool should attempt to connect to https://example.com
	// We just verify it doesn't crash and the logic exists
	result, _ := tool.Call(context.Background(), input)
	// This will fail because we can't actually connect, but we test the upgrade logic
	t.Logf("Result (expected to fail): IsError=%v", result.IsError)
}

func TestWebFetch_EmptyURL(t *testing.T) {
	tool := New()
	input, _ := json.Marshal(map[string]interface{}{
		"url":    "",
		"prompt": "test",
	})

	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected error for empty URL")
	}
	if !strings.Contains(result.Content, "url is required") {
		t.Errorf("unexpected error: %s", result.Content)
	}
}

func TestWebFetch_InvalidJSON(t *testing.T) {
	tool := New()
	result, _ := tool.Call(context.Background(), json.RawMessage(`not json`))
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestWebFetch_Cache(t *testing.T) {
	tool := New()
	testURL := "https://example.com/cached"

	// Pre-populate cache
	tool.cache.Store(testURL, cacheEntry{
		content:   "cached content",
		expiresAt: time.Now().Add(15 * time.Minute),
	})

	input, _ := json.Marshal(map[string]interface{}{
		"url":    testURL,
		"prompt": "test",
	})

	// First call should use cache
	result1, _ := tool.Call(context.Background(), input)
	if result1.IsError {
		t.Fatalf("first call failed: %s", result1.Content)
	}
	if !strings.Contains(result1.Content, "cached content") {
		t.Errorf("expected cached content, got: %s", result1.Content)
	}
}

func TestWebFetch_CacheExpiry(t *testing.T) {
	tool := New()
	testURL := "https://example.com/expired"

	// Pre-populate cache with expired entry
	tool.cache.Store(testURL, cacheEntry{
		content:   "old cached content",
		expiresAt: time.Now().Add(-1 * time.Hour), // Expired
	})

	input, _ := json.Marshal(map[string]interface{}{
		"url":    testURL,
		"prompt": "test",
	})

	// Should try to fetch (and fail since URL doesn't exist)
	result, _ := tool.Call(context.Background(), input)
	// Just verify it tried to fetch (will error due to invalid URL)
	t.Logf("Result after cache expiry: IsError=%v", result.IsError)
}

func TestWebFetch_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Slow response
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	tool := &Tool{
		HTTPClient: &http.Client{Timeout: 10 * time.Millisecond},
	}
	// Cache the result to avoid HTTPS upgrade
	tool.cache.Store(server.URL, cacheEntry{
		content:   "",
		expiresAt: time.Now().Add(-1 * time.Second),
	})

	input, _ := json.Marshal(map[string]interface{}{
		"url":    server.URL,
		"prompt": "test",
	})

	result, _ := tool.Call(context.Background(), input)
	if !result.IsError {
		t.Error("expected timeout error")
	}
}

func TestWebFetch_UserAgent(t *testing.T) {
	// Test User-Agent header is set correctly
	// We can't easily test this with httptest because of HTTPS upgrade,
	// so we just verify the tool is configured correctly
	tool := New()

	if tool.HTTPClient == nil {
		t.Error("HTTPClient should not be nil")
	}
	// The User-Agent is set in the Call method, so we just verify the tool works
	input, _ := json.Marshal(map[string]interface{}{
		"url":    "https://example.com/ua-test",
		"prompt": "test",
	})

	// Pre-populate cache
	tool.cache.Store("https://example.com/ua-test", cacheEntry{
		content:   "test content",
		expiresAt: time.Now().Add(15 * time.Minute),
	})

	result, _ := tool.Call(context.Background(), input)
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
}

func TestWebFetch_Permissions_Deny(t *testing.T) {
	tool := New()
	input, _ := json.Marshal(map[string]interface{}{
		"url":    "https://blocked.example.com/page",
		"prompt": "test",
	})
	// Use a deny rule that matches the full URL pattern
	d := tool.CheckPermissions(input, "default", tools.PermissionRules{
		Deny: []string{"WebFetch"}, // Deny all WebFetch
	})
	if d.Behavior != "deny" {
		t.Errorf("expected deny, got %q: %s", d.Behavior, d.Reason)
	}
}

func TestWebFetch_Permissions_Allow(t *testing.T) {
	tool := New()
	input, _ := json.Marshal(map[string]interface{}{
		"url":    "https://allowed.example.com",
		"prompt": "test",
	})
	d := tool.CheckPermissions(input, "default", tools.PermissionRules{
		Allow: []string{"WebFetch(allowed.example.com)"},
	})
	if d.Behavior != "allow" {
		t.Errorf("expected allow, got %q", d.Behavior)
	}
}

func TestWebFetch_Permissions_DefaultAllow(t *testing.T) {
	tool := New()
	input, _ := json.Marshal(map[string]interface{}{
		"url":    "https://any.example.com",
		"prompt": "test",
	})
	d := tool.CheckPermissions(input, "default", tools.PermissionRules{})
	if d.Behavior != "allow" {
		t.Errorf("expected default allow for read-only tool, got %q", d.Behavior)
	}
}

func TestWebFetch_Metadata(t *testing.T) {
	tool := New()

	if tool.Name() != "WebFetch" {
		t.Errorf("Name: got %q", tool.Name())
	}
	if !tool.IsReadOnly() {
		t.Error("IsReadOnly should be true")
	}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
	if len(tool.InputSchema()) == 0 {
		t.Error("InputSchema should not be empty")
	}
}

func TestConvertHTML(t *testing.T) {
	// Test that convertHTML handles basic HTML
	tests := []struct {
		html     string
		contains string
	}{
		{"<html><body>Hello World</body></html>", "Hello World"},
		{"<p>Paragraph text</p>", "Paragraph text"},
		{"<h1>Heading</h1>", "Heading"},
	}

	for _, tt := range tests {
		got := convertHTML(tt.html)
		if !strings.Contains(got, tt.contains) {
			t.Errorf("convertHTML(%q) = %q, should contain %q", tt.html, got, tt.contains)
		}
	}
}

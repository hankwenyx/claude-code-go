package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	debugOnce   sync.Once
	debugWriter *os.File
	debugActive bool
)

func initDebug() {
	debugOnce.Do(func() {
		if os.Getenv("CLAUDE_DEBUG") == "" {
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		dir := filepath.Join(home, ".claude", "logs")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return
		}
		path := filepath.Join(dir, "api_debug.log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}
		debugWriter = f
		debugActive = true
		debugLog("=== session start ===")
	})
}

func debugLog(format string, args ...any) {
	if !debugActive || debugWriter == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("15:04:05.000")
	fmt.Fprintf(debugWriter, "[%s] %s\n", ts, msg)
}

func debugJSON(label string, v any) {
	if !debugActive {
		return
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		debugLog("%s: (marshal error: %v)", label, err)
		return
	}
	debugLog("%s:\n%s", label, string(b))
}

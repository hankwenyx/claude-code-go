// Package session handles conversation persistence.
// Sessions are stored as JSON files under:
//
//	~/.claude/projects/{cwd-slug}/sessions/{sessionID}.json
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/api"
)

// Record is the on-disk representation of a saved session.
type Record struct {
	ID        string           `json:"id"`
	CWD       string           `json:"cwd"`
	Model     string           `json:"model"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	Messages  []api.APIMessage `json:"messages"`
}

// Save writes the session record to disk, creating directories as needed.
func Save(rec Record) error {
	dir, err := sessionDir(rec.CWD)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	rec.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, rec.ID+".json"), data, 0644)
}

// Load reads a session by ID from the directory associated with cwd.
func Load(cwd, id string) (Record, error) {
	dir, err := sessionDir(cwd)
	if err != nil {
		return Record{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return Record{}, fmt.Errorf("session %q not found: %w", id, err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, fmt.Errorf("session %q corrupt: %w", id, err)
	}
	return rec, nil
}

// List returns all saved sessions for cwd, sorted newest first.
func List(cwd string) ([]Record, error) {
	dir, err := sessionDir(cwd)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var recs []Record
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		rec, err := Load(cwd, id)
		if err != nil {
			continue // skip corrupt files
		}
		recs = append(recs, rec)
	}
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].UpdatedAt.After(recs[j].UpdatedAt)
	})
	return recs, nil
}

// sessionDir returns the directory used for sessions of the given cwd.
func sessionDir(cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	slug := cwdSlug(cwd)
	return filepath.Join(home, ".claude", "projects", slug, "sessions"), nil
}

// cwdSlug converts an absolute path to a filesystem-safe slug.
func cwdSlug(cwd string) string {
	slug := strings.NewReplacer("/", "-", "\\", "-", ":", "").Replace(cwd)
	return strings.TrimPrefix(slug, "-")
}

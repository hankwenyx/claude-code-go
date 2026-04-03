package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hankwenyx/claude-code-go/pkg/api"
	"github.com/hankwenyx/claude-code-go/pkg/session"
)

// redirectHome overrides HOME so session files land in a temp dir.
func redirectHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // Windows
	return tmp
}

func makeMsg(role, text string) api.APIMessage {
	content, _ := json.Marshal(text)
	return api.APIMessage{Role: role, Content: json.RawMessage(content)}
}

func TestSaveAndLoad(t *testing.T) {
	redirectHome(t)
	cwd := t.TempDir()

	rec := session.Record{
		ID:        "abc123",
		CWD:       cwd,
		Model:     "claude-sonnet-4-6",
		CreatedAt: time.Now(),
		Messages:  []api.APIMessage{makeMsg("user", "hello"), makeMsg("assistant", "hi")},
	}
	if err := session.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := session.Load(cwd, "abc123")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != "abc123" {
		t.Errorf("ID: got %q", got.ID)
	}
	if got.Model != "claude-sonnet-4-6" {
		t.Errorf("Model: got %q", got.Model)
	}
	if len(got.Messages) != 2 {
		t.Errorf("Messages: got %d", len(got.Messages))
	}
	// UpdatedAt should be set by Save
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt not set")
	}
}

func TestLoadNotFound(t *testing.T) {
	redirectHome(t)
	cwd := t.TempDir()
	_, err := session.Load(cwd, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestLoadCorrupt(t *testing.T) {
	home := redirectHome(t)
	cwd := t.TempDir()

	// Build the expected session dir path (mirrors cwdSlug logic)
	slug := filepath.Base(cwd) // rough approximation; just put file in correct dir
	_ = slug

	// Save a valid record first to learn the dir, then corrupt it.
	rec := session.Record{ID: "corruptme", CWD: cwd, Model: "m"}
	if err := session.Save(rec); err != nil {
		t.Fatal(err)
	}

	// Find and corrupt the file
	var found string
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Base(path) == "corruptme.json" {
			found = path
		}
		return nil
	})
	if found == "" {
		t.Fatal("session file not found on disk")
	}
	if err := os.WriteFile(found, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := session.Load(cwd, "corruptme")
	if err == nil {
		t.Fatal("expected error for corrupt session")
	}
}

func TestList(t *testing.T) {
	redirectHome(t)
	cwd := t.TempDir()

	// Empty — no error, nil slice
	recs, err := session.List(cwd)
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("expected 0 records, got %d", len(recs))
	}

	// Save two sessions with distinct UpdatedAt
	r1 := session.Record{ID: "s1", CWD: cwd, Model: "m"}
	r2 := session.Record{ID: "s2", CWD: cwd, Model: "m"}
	if err := session.Save(r1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := session.Save(r2); err != nil {
		t.Fatal(err)
	}

	recs, err = session.List(cwd)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2, got %d", len(recs))
	}
	// Newest first
	if recs[0].ID != "s2" {
		t.Errorf("expected s2 first (newest), got %q", recs[0].ID)
	}
}

func TestListSkipsCorrupt(t *testing.T) {
	home := redirectHome(t)
	cwd := t.TempDir()

	// Save valid record
	if err := session.Save(session.Record{ID: "good", CWD: cwd, Model: "m"}); err != nil {
		t.Fatal(err)
	}

	// Inject a corrupt .json file directly
	var sessionDir string
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && filepath.Base(path) == "sessions" {
			sessionDir = path
		}
		return nil
	})
	if sessionDir == "" {
		t.Fatal("sessions dir not found")
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "bad.json"), []byte("{bad}"), 0644); err != nil {
		t.Fatal(err)
	}

	recs, err := session.List(cwd)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 1 || recs[0].ID != "good" {
		t.Errorf("expected only 'good', got %+v", recs)
	}
}

func TestSaveUpdatesExisting(t *testing.T) {
	redirectHome(t)
	cwd := t.TempDir()

	rec := session.Record{ID: "upd", CWD: cwd, Model: "old"}
	if err := session.Save(rec); err != nil {
		t.Fatal(err)
	}
	rec.Model = "new"
	if err := session.Save(rec); err != nil {
		t.Fatal(err)
	}

	got, err := session.Load(cwd, "upd")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "new" {
		t.Errorf("expected model 'new', got %q", got.Model)
	}
}

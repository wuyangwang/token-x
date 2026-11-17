package gemini

import (
	"os"
	"path/filepath"
	"testing"

	"token-x/core"
)

func TestGeminiParse(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "tmp", "project-hash", "chats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"sessionId":"7507e2d9-5772-4e4b-ae7e-cd39c5ffc268","projectHash":"project-hash","startTime":"2026-11-17T10:00:00Z","lastUpdated":"2026-11-17T10:05:00Z","kind":"main","messages":[` +
		`{"id":"u1","timestamp":"2026-11-17T10:00:00Z","type":"user","content":[{"text":"What was the number"}]},` +
		`{"id":"g1","timestamp":"2026-11-17T10:01:00Z","type":"gemini","content":"2156","thoughts":[],"tokens":{"input":21554,"output":9,"cached":100,"thoughts":117,"tool":0,"total":21780},"model":"gemini-3.1-pro-preview"}` +
		`]}`
	path := filepath.Join(dir, "session-20261117-g1.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(home, core.DefaultPrices())
	if err := s.Available(); err != nil {
		t.Fatal(err)
	}
	sessions, err := s.Sessions(core.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	got := sessions[0]
	if got.ID != "7507e2d9-5772-4e4b-ae7e-cd39c5ffc268" || got.Title != "What was the number" {
		t.Errorf("id %q title %q", got.ID, got.Title)
	}
	if got.Tokens.Input != 21554 || got.Tokens.CachedIn != 100 || got.Tokens.Output != 126 {
		t.Errorf("tokens %+v", got.Tokens)
	}
	if got.Model != "gemini-3.1-pro-preview" {
		t.Errorf("model %q", got.Model)
	}
	if !got.Cost.Estimated || got.Cost.Amount <= 0 {
		t.Errorf("cost %+v, want an estimate", got.Cost)
	}

	// Delete removes the file.
	if err := s.DeleteSession(got.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("session file still exists after delete")
	}
}

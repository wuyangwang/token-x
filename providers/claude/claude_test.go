package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"token-x/core"
)

func TestClaudeParse(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "projects", "E--work-demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "3c90a1b2-1234-5678-9abc-def012345678"
	ts := func(min int) string {
		return time.Date(2026, 11, 17, 10, min, 0, 0, time.UTC).Format(time.RFC3339Nano)
	}
	// Two assistant lines share message id msg1: without dedupe the
	// 100 input tokens would be counted twice.
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"` + ts(0) + `","sessionId":"` + id + `","cwd":"E:/work/demo","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"` + ts(1) + `","sessionId":"` + id + `","message":{"id":"msg1","model":"claude-opus-4-6","usage":{"input_tokens":100,"output_tokens":10,"cache_read_input_tokens":50,"cache_creation_input_tokens":5}},"costUSD":0.0123}`,
		`{"type":"assistant","uuid":"a2","timestamp":"` + ts(2) + `","sessionId":"` + id + `","message":{"id":"msg1","model":"claude-opus-4-6","usage":{"input_tokens":100,"output_tokens":10,"cache_read_input_tokens":50,"cache_creation_input_tokens":5}},"costUSD":0.0123}`,
		`{"type":"summary","summary":"测试会话","leafUuid":"a2"}`,
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(body), 0o644); err != nil {
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
	if got.ID != id || got.Title != "测试会话" {
		t.Errorf("id %q title %q", got.ID, got.Title)
	}
	if got.Tokens.Input != 100 || got.Tokens.Output != 10 || got.Tokens.CachedIn != 50 || got.Tokens.CacheWrite != 5 {
		t.Errorf("tokens %+v, want deduped usage", got.Tokens)
	}
	if got.Cost.Amount != 0.0246 || got.Cost.Estimated {
		t.Errorf("cost %+v, want recorded 0.0246", got.Cost)
	}
}

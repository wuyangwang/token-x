package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"token-x/core"
)

func TestRolloutUUID(t *testing.T) {
	cases := map[string]string{
		"rollout-2026-11-17T01-25-03-01a1e71a-b6e4-7493-b7c1-af454b18825d.jsonl":     "01a1e71a-b6e4-7493-b7c1-af454b18825d",
		"rollout-2026-11-17T01-25-03-01a1e71a-b6e4-7493-b7c1-af454b18825d.jsonl.zst": "01a1e71a-b6e4-7493-b7c1-af454b18825d",
		"index.jsonl": "",
		"rollout-short.jsonl": "",
	}
	for name, want := range cases {
		if got := rolloutUUID(name); got != want {
			t.Errorf("rolloutUUID(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestAccumulatorDelta checks the token_count cumulative-delta path and
// that last_token_usage takes precedence when present.
func TestAccumulatorDelta(t *testing.T) {
	total := func(in, out int64) json.RawMessage {
		return json.RawMessage(`{"type":"token_count","info":{"total_token_usage":{` +
			`"input_tokens":` + itoa(in) + `,"output_tokens":` + itoa(out) + `}}}`)
	}
	acc := &rolloutAccumulator{}

	ev1 := rolloutEvent{Type: "event_msg", Payload: total(100, 10)}
	inc, ok := acc.apply(ev1, "m")
	if !ok || inc.Input != 100 || inc.Output != 10 {
		t.Fatalf("first delta: ok=%v %+v", ok, inc)
	}

	ev2 := rolloutEvent{Type: "event_msg", Payload: total(150, 15)}
	inc, ok = acc.apply(ev2, "m")
	if !ok || inc.Input != 50 || inc.Output != 5 {
		t.Fatalf("second delta: ok=%v %+v", ok, inc)
	}

	last := json.RawMessage(`{"type":"token_count","info":{` +
		`"total_token_usage":{"input_tokens":200,"output_tokens":20},` +
		`"last_token_usage":{"input_tokens":30,"output_tokens":3}}}`)
	inc, ok = acc.apply(rolloutEvent{Type: "event_msg", Payload: last}, "m")
	if !ok || inc.Input != 30 || inc.Output != 3 {
		t.Fatalf("last_token_usage: ok=%v %+v", ok, inc)
	}

	// Non-token events apply nothing.
	inc, ok = acc.apply(rolloutEvent{Type: "event_msg", Payload: json.RawMessage(`{"type":"task_started"}`)}, "m")
	if ok {
		t.Fatalf("task_started produced usage %+v", inc)
	}
}

func itoa(n int64) string {
	return json.Number(int64String(n)).String()
}

func int64String(n int64) string {
	return string(jsonInt(n))
}

func jsonInt(n int64) []byte {
	b, _ := json.Marshal(n)
	return b
}

// TestParseRollout runs the full parse over a synthetic rollout file.
func TestParseRollout(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "11", "17")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ts := func(min int) string {
		return time.Date(2026, 11, 17, 10, min, 0, 0, time.UTC).Format(time.RFC3339Nano)
	}
	lines := []string{
		`{"timestamp":"` + ts(0) + `","ordinal":0,"type":"session_meta","payload":{"session_id":"01a1e71a-b6e4-7493-b7c1-af454b18825d","cwd":"E:/work/demo","originator":"codex-tui"}}`,
		`{"timestamp":"` + ts(1) + `","ordinal":1,"type":"turn_context","payload":{"turn_id":"t1","model":"gpt-6.1-sol"}}`,
		`{"timestamp":"` + ts(2) + `","ordinal":2,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20}}}}`,
		`{"timestamp":"` + ts(3) + `","ordinal":3,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":200,"cached_input_tokens":20,"output_tokens":40}}}}`,
	}
	path := filepath.Join(dir, "rollout-2026-11-17T10-00-00-01a1e71a-b6e4-7493-b7c1-af454b18825d.jsonl")
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// The index names the session.
	idx := filepath.Join(home, "session_index.jsonl")
	if err := os.WriteFile(idx, []byte(`{"id":"01a1e71a-b6e4-7493-b7c1-af454b18825d","thread_name":"测试会话"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(home, core.DefaultPrices())
	sessions, err := s.Sessions(core.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	got := sessions[0]
	if got.ID != "01a1e71a-b6e4-7493-b7c1-af454b18825d" {
		t.Errorf("id %q", got.ID)
	}
	if got.Title != "测试会话" {
		t.Errorf("title %q", got.Title)
	}
	if got.Model != "gpt-6.1-sol" {
		t.Errorf("model %q", got.Model)
	}
	if got.Project != "E:/work/demo" {
		t.Errorf("project %q", got.Project)
	}
	// Two cumulative totals 100→200: the deltas sum to the final total.
	if got.Tokens.Input != 200 || got.Tokens.CachedIn != 20 || got.Tokens.Output != 40 {
		t.Errorf("tokens %+v, want the sum of deltas (the final cumulative total)", got.Tokens)
	}
	if !got.Cost.Estimated || got.Cost.Amount <= 0 {
		t.Errorf("cost %+v, want a positive estimate", got.Cost)
	}
}

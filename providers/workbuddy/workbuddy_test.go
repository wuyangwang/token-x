package workbuddy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"token-x/core"
)

const (
	idOne = "2ec7150d-e176-45e3-8533-3396277bb57a"
	idTwo = "5fb5aeeb-a1eb-4ad9-9054-a7cbdf8bf41e"
)

func writeTranscript(t *testing.T, home, project, id, body string) string {
	t.Helper()
	dir := filepath.Join(home, "projects", project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func millis(minute int) int64 {
	return time.Date(2026, 11, 17, 10, minute, 0, 0, time.UTC).UnixMilli()
}

func prices() *core.PriceTable {
	p := core.DefaultPrices()
	p.Models["hy3"] = core.ModelPrice{Input: 1, CachedInput: 0.1, Output: 5}
	p.Models["glm-5.3"] = core.ModelPrice{Input: 2, CachedInput: 0.2, Output: 10}
	return p
}

func TestWorkbuddyParse(t *testing.T) {
	home := t.TempDir()
	usageA := `{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":40},` +
		`"completion_tokens_details":{"reasoning_tokens":4},"prompt_cache_hit_tokens":40,"completion_thinking_tokens":4}`
	usageB := `{"prompt_tokens":200,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":50},` +
		`"prompt_cache_hit_tokens":50}`
	lines := []string{
		fmt.Sprintf(`{"type":"session-meta","id":"m0","sessionId":%q,"timestamp":%d,"meta":{"codebuddy.ai/hostKind":"unopted"}}`, idOne, millis(0)),
		fmt.Sprintf(`{"type":"ai-title","aiTitle":"签到脚本","sessionId":%q,"timestamp":%d}`, idOne, millis(1)),
		fmt.Sprintf(`{"type":"message","role":"user","timestamp":%d,"cwd":"E:/work/demo","content":[{"type":"input_text","text":"<system-reminder>context</system-reminder><user_query>跑一下签到</user_query>"}]}`, millis(2)),
		// Two events of the same step carry the same usage; only one counts.
		fmt.Sprintf(`{"type":"reasoning","id":"r1","timestamp":%d,"providerData":{"conversationRequestId":"req1","messageId":"msg1","stepSeq":1,"model":"hy3"}}`, millis(3)),
		fmt.Sprintf(`{"type":"function_call","id":"f1","timestamp":%d,"providerData":{"conversationRequestId":"req1","messageId":"msg1","stepSeq":1,"model":"hy3","rawUsage":%s}}`, millis(4), usageA),
		fmt.Sprintf(`{"type":"function_call","id":"f1dup","timestamp":%d,"providerData":{"conversationRequestId":"req1","messageId":"msg1","stepSeq":1,"model":"hy3","rawUsage":%s}}`, millis(5), usageA),
		fmt.Sprintf(`{"type":"message","role":"assistant","timestamp":%d,"providerData":{"conversationRequestId":"req1","messageId":"msg2","stepSeq":2,"model":"glm-5.3","rawUsage":%s}}`, millis(6), usageB),
		`{"type":"file-history-snapshot","timestamp":` + fmt.Sprint(millis(7)) + `,"snapshot":{}}`,
	}
	writeTranscript(t, home, "E-work-demo", idOne, strings.Join(lines, "\n")+"\n")

	// A second session has no ai-title, so the user query becomes the title.
	userOnly := fmt.Sprintf(`{"type":"message","role":"user","timestamp":%d,"sessionId":%q,`+
		`"content":[{"type":"input_text","text":"<system-reminder>ctx</system-reminder><user_query>检查签到状态</user_query>"}]}`, millis(9), idTwo)
	writeTranscript(t, home, "E-work-demo", idTwo, userOnly+"\n")

	s := New(home, prices())
	if err := s.Available(); err != nil {
		t.Fatal(err)
	}
	sessions, err := s.Sessions(core.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	if sessions[0].ID != idTwo {
		t.Errorf("newest session = %s, want %s", sessions[0].ID, idTwo)
	}
	var got core.Session
	for _, sess := range sessions {
		if sess.ID == idOne {
			got = sess
		}
	}
	if got.Title != "签到脚本" {
		t.Errorf("title = %q, want ai-title", got.Title)
	}
	if got.Project != "E:/work/demo" {
		t.Errorf("project = %q", got.Project)
	}
	want := core.TokenUsage{Input: 210, CachedIn: 90, Output: 30, Reasoning: 4}
	if got.Tokens != want {
		t.Errorf("tokens = %+v, want %+v", got.Tokens, want)
	}
	if got.Model != "hy3, glm-5.3" {
		t.Errorf("model = %q", got.Model)
	}
	wantCost := 0.000114 + 0.00051
	if diff := got.Cost.Amount - wantCost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost = %v, want %v", got.Cost.Amount, wantCost)
	}
	if !got.Cost.Estimated {
		t.Error("cost should be estimated from prices.json")
	}
	if title := sessions[0].Title; title != "检查签到状态" {
		t.Errorf("fallback title = %q", title)
	}
	if want := time.UnixMilli(millis(7)); !got.Updated.Equal(want) {
		t.Errorf("updated = %v, want %v", got.Updated, want)
	}
}

func TestWorkbuddyDelete(t *testing.T) {
	home := t.TempDir()
	path := writeTranscript(t, home, "E-work-demo", idOne,
		fmt.Sprintf(`{"type":"session-meta","sessionId":%q,"timestamp":%d}`, idOne, millis(0))+"\n")
	other := writeTranscript(t, home, "E-work-demo", idTwo,
		fmt.Sprintf(`{"type":"session-meta","sessionId":%q,"timestamp":%d}`, idTwo, millis(0))+"\n")
	dir := filepath.Dir(path)
	journal := filepath.Join(dir, idOne+".file-rollback.ndjson")
	results := filepath.Join(dir, idOne, "tool-results")
	if err := os.MkdirAll(results, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{journal, filepath.Join(results, "call-1.txt")} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := New(home, core.DefaultPrices())
	if err := s.DeleteSession(idOne); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, journal, results} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists (err=%v)", p, err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("other session removed: %v", err)
	}
	if err := s.DeleteSession(idOne); err == nil {
		t.Error("deleting the same session twice should fail")
	}
	if err := s.DeleteSession("../etc/passwd"); err == nil {
		t.Error("malformed session id should be rejected")
	}
}

func TestWorkbuddyUnavailable(t *testing.T) {
	s := New(t.TempDir(), core.DefaultPrices())
	if err := s.Available(); err == nil {
		t.Error("missing projects dir should be unavailable")
	}
}

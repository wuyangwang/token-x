package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	myui "github.com/wuyangwang/my-ui"

	"token-x/core"
)

// codexStub stands in for the real source in tests.
type codexStub struct{}

func (codexStub) Name() string { return "codex" }

func (codexStub) Available() error { return nil }

func (codexStub) Sessions(filter core.Filter) ([]core.Session, error) { return nil, nil }

func (codexStub) DeleteSession(id string) error { return nil }

// opencodeStub stands in for the real source in tests.
type opencodeStub struct{}

func (opencodeStub) Name() string { return "opencode" }

func (opencodeStub) Available() error { return nil }

func (opencodeStub) Sessions(filter core.Filter) ([]core.Session, error) { return nil, nil }

func (opencodeStub) DeleteSession(id string) error { return nil }

// testApp builds an app with fake sessions and no window, as the tester
// exercises the view without one.
func testApp(t *testing.T, sessions []core.Session) *app {
	t.Helper()
	a := &app{}
	a.prices = core.DefaultPrices()
	a.reg = core.NewRegistry(codexStub{}, opencodeStub{})
	a.session = sessions
	return a
}

// fakeSessions is a small, deterministic set.
func fakeSessions() []core.Session {
	now := time.Now()
	day := 24 * time.Hour
	return []core.Session{
		{ID: "s1", Title: "今天的会话", Source: "codex", Project: "E:/work/alpha",
			Model: "gpt-6.1-sol", Created: now, Updated: now,
			Tokens: core.TokenUsage{Input: 1000, Output: 500}},
		{ID: "s2", Title: "昨天的会话", Source: "opencode", Project: "E:/work/beta",
			Model: "glm-5.3-flash-free", Created: now.Add(-day), Updated: now.Add(-day),
			Tokens: core.TokenUsage{Input: 2000, Output: 100}},
		{ID: "s3", Title: "上周的会话", Source: "opencode", Project: "E:/work/alpha",
			Model: "glm-5.3-flash-free", Created: now.Add(-8 * day), Updated: now.Add(-8 * day),
			Tokens: core.TokenUsage{Input: 3000, Output: 200}},
	}
}

func TestMainViewRenders(t *testing.T) {
	a := testApp(t, fakeSessions())
	tt := ui.NewTester(a.view, 640, 620)
	for _, want := range []string{"今日金额", "3 个 session", "今天的会话", "昨天的会话"} {
		if !tt.HasText(want) {
			t.Errorf("main view missing text %q, texts %q", want, tt.Texts())
		}
	}
}

func TestPeriodSwitch(t *testing.T) {
	a := testApp(t, fakeSessions())
	tt := ui.NewTester(a.view, 640, 620)
	if err := tt.Click("近 7 天"); err != nil {
		t.Fatalf("period click: %v", err)
	}
	if a.period != 1 {
		t.Errorf("period %d after clicking 近 7 天", a.period)
	}
	if !tt.HasText("近 7 天金额") {
		t.Errorf("hero label did not follow, texts %q", tt.Texts())
	}
}

func TestFilterByQuery(t *testing.T) {
	a := testApp(t, fakeSessions())
	tt := ui.NewTester(a.view, 640, 620)
	tt.Click("搜索…")
	tt.Type("昨天")
	if !tt.HasText("1 个 session") {
		t.Errorf("filter did not narrow the table, texts %q", tt.Texts())
	}
}

func TestSortByCostAndTime(t *testing.T) {
	a := testApp(t, fakeSessions())
	a.sort = ui.SortOrder{Column: "cost"}
	a.rowsNow = a.filtered()
	a.sortRows(a.rowsNow)
	if a.rowsNow[0].ID != "s1" {
		t.Fatalf("cost ascending: first %q, want the highest-cost session s1", a.rowsNow[0].ID)
	}

	a.sort = ui.SortOrder{Column: "time", Descending: true}
	a.rowsNow = a.filtered()
	a.sortRows(a.rowsNow)
	if a.rowsNow[0].ID != "s1" || a.rowsNow[len(a.rowsNow)-1].ID != "s3" {
		t.Errorf("time descending: %s", idsOf(a.rowsNow))
	}
}

func idsOf(rows []core.Session) []string {
	out := make([]string, len(rows))
	for i, s := range rows {
		out[i] = s.ID
	}
	return out
}

func TestThemeToggle(t *testing.T) {
	a := testApp(t, fakeSessions())
	tt := ui.NewTester(a.view, 640, 620)
	if err := tt.Click("切换到深色"); err != nil {
		t.Fatalf("theme click: %v", err)
	}
	if a.appearance != myui.AppearanceDark {
		t.Errorf("appearance %v after clicking the moon", a.appearance)
	}
	if !tt.HasText("切换到浅色") {
		t.Errorf("the switch did not flip its hint, texts %q", tt.Texts())
	}
}

func TestDeleteFlowDisabledWithoutSelection(t *testing.T) {
	a := testApp(t, fakeSessions())
	tt := ui.NewTester(a.view, 640, 620)
	if !tt.HasText("删除") {
		t.Errorf("delete action missing, texts %q", tt.Texts())
	}
	// No selection: the confirm modal must not open.
	if a.confirm {
		t.Errorf("confirm opened without a selection")
	}
}

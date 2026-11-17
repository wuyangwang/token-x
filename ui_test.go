package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

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
	a := &app{trendDays: 30}
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

func TestDashboardRenders(t *testing.T) {
	a := testApp(t, fakeSessions())
	tt := ui.NewTester(a.view, 1100, 720)
	for _, want := range []string{"Token X", "今日 Tokens", "近 7 天金额", "每日金额趋势", "按模型细分"} {
		if !tt.HasText(want) {
			t.Errorf("dashboard missing text %q, texts %q", want, tt.Texts())
		}
	}
}

func TestSessionsPage(t *testing.T) {
	a := testApp(t, fakeSessions())
	a.page = 1
	tt := ui.NewTester(a.view, 1100, 720)
	if !tt.HasText("今天的会话") {
		t.Errorf("sessions table missing row, texts %q", tt.Texts())
	}
	if !tt.HasText("3 个 session") {
		t.Errorf("row count missing, texts %q", tt.Texts())
	}
}

func TestSettingsPage(t *testing.T) {
	a := testApp(t, fakeSessions())
	a.page = 2
	tt := ui.NewTester(a.view, 1100, 720)
	for _, want := range []string{"价格表", "恢复默认", "数据源"} {
		if !tt.HasText(want) {
			t.Errorf("settings missing text %q, texts %q", want, tt.Texts())
		}
	}
}

func TestNavigation(t *testing.T) {
	a := testApp(t, fakeSessions())
	tt := ui.NewTester(a.view, 1100, 720)
	if err := tt.Click("Sessions"); err != nil {
		t.Fatalf("nav click: %v", err)
	}
	if a.page != 1 {
		t.Errorf("page %d after clicking Sessions", a.page)
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatalf("nav click: %v", err)
	}
	if a.page != 2 {
		t.Errorf("page %d after clicking 设置", a.page)
	}
}

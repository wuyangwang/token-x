// token-x manages the token usage of local AI coding agents: it reads the
// session stores of Codex and OpenCode, shows token and cost totals, and
// deletes sessions for good.
//
// The window is one column, in the manner of the usage tools it resembles
// (ccusage, ccseva): the period's cost is the hero number, a compact bar
// chart of the same period sits under it, and the sessions table — the
// thing the user acts on — fills the rest. No sidebar, no settings page;
// prices live in ~/.token-x/prices.json.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	myui "github.com/wuyangwang/my-ui"
	"github.com/wuyangwang/my-ui/icons"

	"token-x/core"
	"token-x/providers/codex"
	"token-x/providers/opencode"
)

// periods is the segmented control at the top: what the hero, the chart
// and the model line show.
var periods = []string{"今日", "近 7 天", "近 30 天"}
var periodDays = []int{1, 7, 30}

// app is the state the window shows.
type app struct {
	win    *mygo.Window
	reg    *core.Registry
	prices *core.PriceTable

	loading bool
	loadErr error
	session []core.Session // all sessions, newest first

	period int // index into periods

	// Filters. Empty strings mean "all".
	fSource  string
	fProject string
	fQuery   string

	// The sessions table. The selection holds session ids, so it follows
	// rows across reloads and filter changes.
	rows   ui.ListState
	chosen ui.Selection[string]
	// The sessions of the current frame, for cell building and actions.
	rowsNow []core.Session

	confirm  bool // the delete-confirmation modal
	deleting bool // a delete job is running

	// toast is shown once by the view, then cleared.
	toast pendingToast
}

// pendingToast is a notification the view shows once.
type pendingToast struct {
	Title string
	Desc  string
	Error bool
	shown bool
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--list" {
		if err := cliList(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	windowMain()
}

func windowMain() {
	a := &app{}
	home, _ := os.UserHomeDir()
	a.prices = core.LoadPrices(filepath.Join(home, ".token-x"))
	a.reg = core.NewRegistry(codex.New("", a.prices), opencode.New(""))

	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "Token X",
			Width:     640,
			Height:    620,
			MinWidth:  560,
			MinHeight: 480,
			StateKey:  "main",
			Content:   ui.View(a.view),
		})
		a.reload()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// reload scans every source off the UI thread and publishes through
// Window.Update.
func (a *app) reload() {
	if a.loading {
		return
	}
	a.loading = true
	win := a.win
	reg := a.reg
	go func() {
		sessions, err := reg.AllSessions(core.Filter{IncludeArchived: true})
		sort.Slice(sessions, func(i, j int) bool { return sessions[i].Updated.After(sessions[j].Updated) })
		win.Update(func() {
			a.session = sessions
			a.loadErr = err
			a.loading = false
			a.chosen.Clear()
		})
	}()
}

// view builds one frame.
func (a *app) view(c *ui.Context) {
	t := myui.Apply(c, myui.Options{})

	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.header(c, t)
		a.usage(c, t)
		a.divider(c, t)
		a.sessionsSection(c, t)
		a.footer(c, t)
	})
	myui.NewToastViewport(c)

	// One toast per pending notification.
	if a.toast.Title != "" && !a.toast.shown {
		a.toast.shown = true
		toast := myui.NewToast(c.Key("app toast"), a.toast.Title).
			Description(a.toast.Desc).Timeout(6 * time.Second)
		if a.toast.Error {
			toast.Destructive()
		} else {
			toast.Success()
		}
		toast.Show()
	}
	if a.toast.shown && a.toast.Title != "" {
		a.toast.Title, a.toast.Desc = "", "" // shown; clear on this frame
	}
}

func (a *app) divider(c *ui.Context, t *ui.Theme) {
	ui.Box(c).FillWidth().Height(1).Background(t.Border)
}

// header is the hero line: the period's cost, the period switch, and the
// tools. The cost is the one accent of the window.
func (a *app) header(c *ui.Context, t *ui.Theme) {
	tokens, cost, count := a.periodTotals()
	est := ""
	if cost.Estimated {
		est = "*"
	}

	ui.Column(c).FillWidth().PaddingY(t.Space(5)).PaddingX(t.Space(6)).Gap(t.Space(2)).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.End).Children(func() {
			ui.Column(c).Gap(0).Children(func() {
				ui.Text(c, periodLabel(a.period)).FontSize(myui.TextSize(t, myui.TextSm)).
					TextColor(t.TextMuted)
				ui.Text(c, fmtCost(cost.Amount)+est).
					FontSize(myui.TextSize(t, myui.TextXl)).Bold()
			})
			ui.Box(c).Grow(1)
			a.periodSwitch(c, t)
			refresh := myui.NewButton(c.Key("reload"), "").Icon(icons.RefreshCw()).Ghost().Sm()
			refresh.AlignSelf(ui.End)
			if a.loading {
				refresh.Disabled(true)
			}
			if refresh.Clicked() {
				a.reload()
			}
		})
		ui.Text(c, fmt.Sprintf("%s tokens · %d 个 session", fmtTokens(tokens.Total()), count)).
			FontSize(myui.TextSize(t, myui.TextSm)).TextColor(t.TextMuted)
		if a.loadErr != nil {
			ui.Text(c, "部分数据读取失败: "+a.loadErr.Error()).
				FontSize(myui.TextSize(t, myui.TextXs)).TextColor(t.Warning)
		}
	})
}

func periodLabel(period int) string {
	if period < len(periods) {
		return periods[period] + "金额"
	}
	return "金额"
}

// periodSwitch is the segmented 今日 / 近 7 天 / 近 30 天 control.
func (a *app) periodSwitch(c *ui.Context, t *ui.Theme) {
	myui.NewTabs(c, &a.period, myui.Tab{Label: periods[0]}, myui.Tab{Label: periods[1]}, myui.Tab{Label: periods[2]})
}

// periodSessions returns the sessions whose last activity falls in the
// chosen period, and the day count the period covers.
func (a *app) periodSessions() ([]core.Session, int) {
	days := periodDays[a.period]
	start := time.Now().Local().Truncate(24*time.Hour).AddDate(0, 0, -(days - 1))
	var out []core.Session
	for _, s := range a.session {
		if !s.Updated.Local().Truncate(24*time.Hour).Before(start) {
			out = append(out, s)
		}
	}
	return out, days
}

func (a *app) periodTotals() (core.TokenUsage, core.CostValue, int) {
	sessions, _ := a.periodSessions()
	tokens, cost := core.Totals(sessions)
	return tokens, cost, len(sessions)
}

// usage is the contribution-style heat grid of the period (one column per
// week, one cell per day, depth = that day's cost) plus a one-line model
// summary.
func (a *app) usage(c *ui.Context, t *ui.Theme) {
	sessions, days := a.periodSessions()
	daily := core.DailyTotals(sessions, days, time.Now())

	ui.Column(c).FillWidth().PaddingX(t.Space(6)).PaddingY(t.Space(2.5)).Children(func() {
		a.heatmap(c, t, daily)
		a.modelLine(c, t, sessions)
	})
}

// heatmap draws the daily cells GitHub's contribution graph does: weeks as
// columns, Monday at the top, cell depth by cost against the period's
// busiest day.
func (a *app) heatmap(c *ui.Context, t *ui.Theme, daily []core.DailyUsage) {
	const cell, gap = 12.0, 3.0

	byDay := map[string]core.DailyUsage{}
	var peak float64
	for _, d := range daily {
		byDay[d.Day.Format("2006-01-02")] = d
		if d.Cost.Amount > peak {
			peak = d.Cost.Amount
		}
	}
	today := time.Now().Local().Truncate(24 * time.Hour)
	if len(daily) == 0 {
		return
	}

	// Pad the first week so the first day lands on its weekday row.
	offset := (int(daily[0].Day.Weekday()) + 6) % 7 // Monday = 0
	weeks := (offset + len(daily) + 6) / 7
	levelAlpha := []float32{0.18, 0.38, 0.6, 1.0}

	ui.Row(c).FillWidth().Gap(gap).AlignItems(ui.Start).Children(func() {
		// Weekday labels: 一, 三, 五 as GitHub marks Mon, Wed, Fri.
		ui.Column(c).Gap(gap).Children(func() {
			for i := 0; i < 7; i++ {
				label := ""
				switch i {
				case 0:
					label = "一"
				case 2:
					label = "三"
				case 4:
					label = "五"
				}
				ui.Box(c).Size(cell, cell).Children(func() {
					if label != "" {
						ui.Text(c, label).FontSize(myui.TextSize(t, myui.TextXs)).
							TextColor(t.TextMuted)
					}
				})
			}
		})

		for w := 0; w < weeks; w++ {
			week := w
			ui.Column(c).Gap(gap).Children(func() {
				for d := 0; d < 7; d++ {
					idx := week*7 + d - offset
					if idx < 0 || idx >= len(daily) {
						ui.Box(c).Size(cell, cell).Radius(2).Background(t.Surface.Alpha(0.5))
						continue
					}
					day := daily[idx]
					date := day.Day.Format("2006-01-02")
					if day.Day.After(today) {
						ui.Box(c).Size(cell, cell).Radius(2).Background(t.Surface.Alpha(0.5))
						continue
					}
					level := 0
					if peak > 0 && day.Cost.Amount > 0 {
						level = int(day.Cost.Amount/peak*4) + 1
						if level > 4 {
							level = 4
						}
					}
					lv := level
					box := ui.Box(c).Size(cell, cell).Radius(2)
					if lv == 0 {
						box.Background(t.Surface)
						box.Tooltip(date + " · 无用量")
					} else {
						box.Background(t.Accent.Alpha(levelAlpha[lv-1]))
						box.Tooltip(fmt.Sprintf("%s · %s · %s", date,
							fmtCost(day.Cost.Amount), fmtTokens(day.Tokens.Total())+" tokens"))
					}
				}
			})
		}

		// The depth legend: 少 → 多, real swatches of the same ramp.
		ui.Box(c).Grow(1)
		ui.Row(c).Gap(gap).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "少").FontSize(myui.TextSize(t, myui.TextXs)).TextColor(t.TextMuted)
			for _, al := range levelAlpha {
				ui.Box(c).Size(cell, cell).Radius(2).Background(t.Accent.Alpha(al))
			}
			ui.Text(c, "多").FontSize(myui.TextSize(t, myui.TextXs)).TextColor(t.TextMuted)
		})
	})
}

// modelLine is the per-model summary of the period, one muted line: the
// model breakdown without a table of its own.
func (a *app) modelLine(c *ui.Context, t *ui.Theme, sessions []core.Session) {
	rows := core.ModelTotals(sessions)
	if len(rows) == 0 {
		ui.Text(c, "没有用量记录").FontSize(myui.TextSize(t, myui.TextXs)).TextColor(t.TextMuted)
		return
	}
	max := 3
	if len(rows) < max {
		max = len(rows)
	}
	parts := make([]string, 0, max+1)
	for _, r := range rows[:max] {
		s := fmt.Sprintf("%s %s", r.Model, fmtCost(r.Cost.Amount))
		if r.Cost.Estimated {
			s += "*"
		}
		parts = append(parts, s)
	}
	if len(rows) > max {
		parts = append(parts, fmt.Sprintf("…等 %d 个模型", len(rows)))
	}
	ui.Text(c, strings.Join(parts, "  ·  ")).FontSize(myui.TextSize(t, myui.TextXs)).
		TextColor(t.TextMuted).SingleLine()
}

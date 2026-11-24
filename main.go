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
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	myui "github.com/wuyangwang/my-ui"
	"github.com/wuyangwang/my-ui/controls"
	"github.com/wuyangwang/my-ui/display"
	"github.com/wuyangwang/my-ui/icons"
	"github.com/wuyangwang/my-ui/overlays"
	mywindow "github.com/wuyangwang/my-ui/window"

	"token-x/core"
	_ "token-x/locales" // register zh-Hans so the UI and installer are Chinese
)

// heatLevels is the 少→多 ramp, from faintest to full accent.
var heatLevels = []float32{0.18, 0.38, 0.6, 1.0}

// periods is the segmented control at the top: what the hero, the chart
// and the model line show.
var periods = []string{"今日", "近 7 天", "近 30 天"}
var periodDays = []int{1, 7, 30}

// app is the state the window shows.
type app struct {
	win      *mygo.Window
	reg      *core.Registry
	prices   *core.PriceTable
	themeDir string // ~/.token-x: prices.json and the theme preference

	loading bool
	loadErr error
	session []core.Session // all sessions, newest first

	// cache is the on-disk parse snapshot. It lets the window show data
	// immediately and keeps each session's original cost across price updates.
	cache *core.SessionCache
	// reloadPending requests another scan when one is already running, so a
	// price refresh that lands mid-scan is not lost.
	reloadPending bool
	// priceNote is the small source label under the hero number.
	priceNote string

	period int // index into periods

	// The appearance choice. followSystem is true until the user switches;
	// dark then mirrors the effective theme for the toggle's icon, while
	// my-ui's window shell drives the content, the title bar and the frame.
	followSystem bool
	dark         bool

	// Filters. Empty strings mean "all".
	fSource  string
	fProject string
	fQuery   string

	// The sessions table. The selection holds session ids, so it follows
	// rows across reloads and filter changes.
	rows   ui.ListState
	sort   ui.SortOrder
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
	a.themeDir = filepath.Join(home, ".token-x")
	a.prices = core.LoadPrices(a.themeDir)
	a.reg = newRegistry(a.prices)

	// Show the last snapshot before any network or disk scan: with many
	// archived sessions the first full scan of a new install is still the
	// slow case, but every later launch starts from cached data.
	a.cache = core.LoadSessionCache(a.themeDir)
	a.session = a.cache.Sessions()
	sort.Slice(a.session, func(i, j int) bool { return a.session[i].Updated.After(a.session[j].Updated) })
	a.priceNote = priceStatus(a.themeDir)

	// No saved choice means "follow the system"; the first run starts in
	// the desktop's appearance and later runs in the one the user chose.
	src := loadTheme(a.themeDir)
	a.followSystem = src == mygo.ThemeSystem
	a.dark = src == mygo.ThemeDark

	mygo.App.WhenReady(func() {
		// my-ui's window shell owns the frameless window, the themed title
		// bar (Windows buttons or Mac traffic lights, by platform), the
		// window background and the native frame's appearance.
		a.win = mywindow.New(mywindow.Options{
			Title:     "Token X",
			Name:      "Token X",
			Width:     640,
			Height:    620,
			MinWidth:  560,
			MinHeight: 480,
			StateKey:  "main",
			Logo: func(c *ui.Context) {
				ui.Image(c, appIcon).Size(18, 18).Radius(4)
			},
			Theme: a.appearance,
			View:  a.view,
		})
		a.syncAppearance()
		mygo.Theme.OnUpdated(a.syncAppearance)
		a.reload()
		// One price fetch per launch, off the UI thread. A failure is silent:
		// the cached or built-in table stays in effect.
		go a.fetchPrices()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// appearance supplies my-ui's window shell with the theme options for every
// frame: the bar, the content and the native frame all take their colors
// from here, so a switch changes them together.
func (a *app) appearance() myui.Options {
	appearance := myui.AppearanceSystem
	if !a.followSystem {
		appearance = myui.AppearanceLight
		if a.dark {
			appearance = myui.AppearanceDark
		}
	}
	return myui.Options{Appearance: appearance, Palette: myui.PaletteOrange}
}

// syncAppearance mirrors the effective theme for the toggle's icon. Called
// at startup and whenever the appearance changes, on MyGo's main thread.
func (a *app) syncAppearance() {
	a.dark = mygo.Theme.IsDark()
}

// toggleAppearance switches light/dark and remembers the choice, so the
// next run starts in it. The window shell applies it on the next frame.
func (a *app) toggleAppearance() {
	a.followSystem = false
	a.dark = !a.dark
	src := mygo.ThemeDark
	if !a.dark {
		src = mygo.ThemeLight
	}
	if a.themeDir != "" {
		_ = saveTheme(a.themeDir, src)
	}
}

// reload requests a scan. A scan already in flight is not cancelled; the
// request is remembered and run again once it publishes, so a price refresh
// arriving mid-scan still takes effect on new sessions.
func (a *app) reload() {
	if a.loading {
		a.reloadPending = true
		return
	}
	a.startReload()
}

// startReload refreshes every source off the UI thread, persists the new
// snapshot and publishes through Window.Update. Unchanged backing files are
// reused from the cache, so their historical cost never moves.
func (a *app) startReload() {
	a.loading = true
	a.reloadPending = false
	win := a.win
	reg := a.reg
	var prev map[string]map[string]core.FileCache
	if a.cache != nil {
		prev = a.cache.Sources
	}
	dir := a.themeDir
	go func() {
		sessions, caches, err := reg.Refresh(prev)
		sort.Slice(sessions, func(i, j int) bool { return sessions[i].Updated.After(sessions[j].Updated) })
		if dir != "" {
			_ = core.SaveSessionCache(dir, core.NewSessionCache(caches))
		}
		win.Update(func() {
			a.session = sessions
			if a.cache == nil {
				a.cache = core.NewSessionCache(nil)
			}
			a.cache.Sources = caches
			a.loadErr = err
			a.loading = false
			a.chosen.Clear()
			// If the filtered project/source no longer has any session
			// (e.g. the last one was just deleted), fall back to "all".
			if a.fProject != "" && !hasProject(sessions, a.fProject) {
				a.fProject = ""
			}
			if a.fSource != "" && !hasSource(sessions, a.fSource) {
				a.fSource = ""
			}
			if a.reloadPending {
				a.startReload()
			}
		})
	}()
}

// fetchPrices pulls the public LiteLLM cost map once at launch, stores it
// under ~/.token-x and swaps it into the live table. Only models the user
// has not overridden in prices.json change; already-cached sessions keep
// the cost they were first computed with.
func (a *app) fetchPrices() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 9 * time.Second}
	models, source, err := core.FetchOnlinePrices(ctx, client)
	if err != nil || len(models) == 0 {
		return
	}
	if err := core.SaveOnline(a.themeDir, &core.OnlinePriceFile{
		FetchedAt: time.Now(), Source: source, Models: models,
	}); err != nil {
		return
	}
	a.prices.Replace(core.LoadPrices(a.themeDir))
	win := a.win
	win.Update(func() {
		a.priceNote = priceStatus(a.themeDir)
		a.reload()
	})
}

// priceStatus is the short label shown next to the period totals.
func priceStatus(dir string) string {
	f := core.LoadOnline(dir)
	if f == nil || len(f.Models) == 0 {
		return "价格：内置表"
	}
	return "价格：线上 · " + f.FetchedAt.Local().Format("01-02 15:04")
}

func hasProject(sessions []core.Session, project string) bool {
	for _, s := range sessions {
		if s.Project == project {
			return true
		}
	}
	return false
}

func hasSource(sessions []core.Session, source string) bool {
	for _, s := range sessions {
		if s.Source == source {
			return true
		}
	}
	return false
}

// view builds one frame. my-ui's window shell has already applied the
// theme, so the frame's own theme and colors are ready to use here.
func (a *app) view(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.header(c, t)
		a.usage(c, t)
		a.divider(c, t)
		a.sessionsSection(c, t)
		a.footer(c, t)
	})
	overlays.NewToastViewport(c)

	// One toast per pending notification.
	if a.toast.Title != "" && !a.toast.shown {
		a.toast.shown = true
		toast := overlays.NewToast(c.Key("app toast"), a.toast.Title).
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

	ui.Column(c).FillWidth().PaddingY(t.Space(5)).PaddingX(t.Space(6)).Gap(t.Space(2)).Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(0).Children(func() {
				ui.Text(c, periodLabel(a.period)).FontSize(myui.TextSize(t, myui.TextSm)).
					TextColor(t.TextMuted)
				ui.Text(c, fmtCost(cost.Amount)).
					FontSize(myui.TextSize(t, myui.Text2Xl)).Bold()
			})
			ui.Box(c).Grow(1)
			a.periodSwitch(c, t)
			refresh := controls.NewButton(c.Key("reload"), "").Icon(icons.RefreshCw()).Ghost().Sm().Label("刷新")
			refresh.AlignSelf(ui.Center)
			if a.loading {
				refresh.Disabled(true)
			}
			if refresh.Clicked() {
				a.reload()
			}
			icon, hint := icons.Moon(), "切换到深色"
			if a.dark {
				icon, hint = icons.Sun(), "切换到浅色"
			}
			appearance := controls.NewButton(c.Key("appearance"), "").Icon(icon).Ghost().Sm().Label(hint)
			appearance.AlignSelf(ui.Center)
			if appearance.Clicked() {
				a.toggleAppearance()
			}
		})
		detail := fmt.Sprintf("%s tokens · %d 个 session", fmtTokens(tokens.Total()), count)
		if a.priceNote != "" {
			detail += " · " + a.priceNote
		}
		ui.Text(c, detail).FontSize(myui.TextSize(t, myui.TextSm)).TextColor(t.TextMuted)
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
	display.NewTabs(c, &a.period, display.Tab{Label: periods[0]}, display.Tab{Label: periods[1]}, display.Tab{Label: periods[2]})
}

// periodSessions returns the sessions whose last activity falls in the
// chosen period, and the day count the period covers.
func (a *app) periodSessions() ([]core.Session, int) {
	days := periodDays[a.period]
	start := time.Now().Local().Truncate(24*time.Hour).AddDate(0, 0, -(days - 1))
	var out []core.Session
	for _, s := range a.session {
		if !s.Updated.Local().Truncate(24 * time.Hour).Before(start) {
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

// usage is the contribution-style heat grid (one column per week, one
// cell per day, depth = that day's cost) plus a one-line model summary.
func (a *app) usage(c *ui.Context, t *ui.Theme) {
	sessions, _ := a.periodSessions()

	ui.Column(c).FillWidth().PaddingX(t.Space(6)).PaddingY(t.Space(2.5)).Gap(t.Space(2)).Children(func() {
		a.heatmap(c, t)
		a.legend(c, t)
		a.modelLine(c, t, sessions)
	})
}

// heatmap draws the daily cells GitHub's contribution graph does: weeks as
// columns, square cells, depth by cost against the busiest day. The
// window's width picks the week count, so the cells span it edge to edge
// and the range follows the window instead of being a fixed slice.
func (a *app) heatmap(c *ui.Context, t *ui.Theme) {
	const cell, gap = 12.0, 3.0

	// The legend sits below the grid, so the cells can use the whole row.
	w, _ := c.Size()
	weeks := int((w - 2*t.Space(6)) / (cell + gap))
	if weeks < 8 {
		weeks = 8
	}
	if weeks > 60 {
		weeks = 60
	}

	// Anchor on today, which is always the bottom-right cell. Walking
	// backwards by column and row gives each cell its date directly.
	today := time.Now().Local().Truncate(24 * time.Hour)
	todayRow := (int(today.Weekday()) + 6) % 7 // Monday = 0
	daily := core.DailyTotals(a.session, weeks*7, time.Now())
	byDay := make(map[string]core.DailyUsage, len(daily))
	var peak float64
	for _, d := range daily {
		byDay[d.Day.Format("2006-01-02")] = d
		if d.Cost.Amount > peak {
			peak = d.Cost.Amount
		}
	}
	if len(byDay) == 0 {
		return
	}
	ui.Row(c).FillWidth().Gap(gap).AlignItems(ui.End).Children(func() {
		for w := 0; w < weeks; w++ {
			col := w
			ui.Column(c).Gap(gap).Children(func() {
				for d := 0; d < 7; d++ {
					daysAgo := (weeks-1-col)*7 + todayRow - d
					date := today.AddDate(0, 0, -daysAgo)
					key := date.Format("2006-01-02")
					day, ok := byDay[key]
					box := ui.Box(c).Size(cell, cell).Radius(2)
					if daysAgo < 0 || !ok {
						box.Background(t.Surface.Alpha(0.5))
					} else if peak > 0 && day.Cost.Amount > 0 {
						lv := int(day.Cost.Amount/peak*4) + 1
						if lv > 4 {
							lv = 4
						}
						box.Background(t.Accent.Alpha(heatLevels[lv-1]))
						box.Tooltip(fmt.Sprintf("%s · %s · %s", key,
							fmtCost(day.Cost.Amount), fmtTokens(day.Tokens.Total())+" tokens"))
					} else {
						box.Background(t.Surface)
						box.Tooltip(key + " · 无用量")
					}
				}
			})
		}
	})
}

// legend is the 少 → 多 depth scale, placed under the grid and right
// aligned.
func (a *app) legend(c *ui.Context, t *ui.Theme) {
	const cell, gap = 12.0, 3.0
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Box(c).Grow(1)
		ui.Row(c).Gap(gap).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "少").FontSize(myui.TextSize(t, myui.TextXs)).TextColor(t.TextMuted)
			for _, al := range heatLevels {
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
		parts = append(parts, fmt.Sprintf("%s %s", r.Model, fmtCost(r.Cost.Amount)))
	}
	if len(rows) > max {
		parts = append(parts, fmt.Sprintf("…等 %d 个模型", len(rows)))
	}
	ui.Text(c, strings.Join(parts, "  ·  ")).FontSize(myui.TextSize(t, myui.TextXs)).
		TextColor(t.TextMuted).SingleLine()
}

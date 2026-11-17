// token-x manages the token usage of local AI coding agents: it reads the
// session stores of Codex and OpenCode, shows daily token and cost
// totals, and deletes sessions for good.
package main

import (
	"fmt"
	"log"
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

// app is the state the window shows.
type app struct {
	win    *mygo.Window
	reg    *core.Registry
	prices *core.PriceTable

	page int // 0 总览, 1 Sessions, 2 设置

	loading bool
	loadErr error
	session []core.Session // all sessions, newest first
	lastLoad time.Time

	// Filters (Sessions page). Empty strings mean "all".
	fSource  string
	fProject string
	fModel   string
	fQuery   string
	// The filters the current table state was built with; when they
	// change, the row selection starts over.
	tableSig string

	rows      ui.ListState // the Sessions table
	chosen    ui.Selection[string]
	confirm   bool         // the delete-confirmation modal
	deleting  bool         // a delete job is running
	dayTab    int          // breakdown tab: 0 今日, 1 累计
	trendDays int          // 7 or 30

	// toast is shown once by the view, then cleared.
	toast pendingToast

	// Settings page: the price inputs are strings until saved.
	priceDraft  map[string]*priceRow
	priceOrder  []string
	priceDirty  bool
}

// pendingToast is a notification the view shows once.
type pendingToast struct {
	Title string
	Desc  string
	Error bool
	shown bool
}

// priceRow is one editable row of the settings price table.
type priceRow struct {
	Model                string
	Input, Cached, Output string // as edited text
	Known                bool
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--list" {
		if err := cliList(); err != nil {
			log.Fatal(err)
		}
		return
	}
	windowMain()
}

func windowMain() {
	a := &app{trendDays: 30}
	home, _ := os.UserHomeDir()
	a.prices = core.LoadPrices(filepath.Join(home, ".token-x"))
	a.reg = core.NewRegistry(codex.New("", a.prices), opencode.New(""))

	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "Token X",
			Width:     1100,
			Height:    720,
			MinWidth:  820,
			MinHeight: 520,
			StateKey:  "main",
			Content:   ui.View(a.view),
		})
		a.reload()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
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
			a.lastLoad = time.Now()
			a.loading = false
			a.chosen.Clear()
		})
	}()
}

// view builds one frame.
func (a *app) view(c *ui.Context) {
	t := myui.Apply(c, myui.Options{})

	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.appBar(c, t)
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			a.main(c, t)
		})
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

// appBar is the top strip: brand, the page tabs, and the refresh control.
func (a *app) appBar(c *ui.Context, t *ui.Theme) {
	ui.Row(c).FillWidth().Height(t.Space(13)).PaddingX(t.Space(6)).
		Gap(t.Space(4)).AlignItems(ui.Center).Children(func() {
		mark := ui.Box(c).Size(t.Space(6), t.Space(6)).Radius(t.Space(2)).
			Background(t.Accent)
		mark.Children(func() {
			ui.Icon(c, icons.Coins()).Size(t.Space(4), t.Space(4)).
				TextColor(t.AccentText)
		})
		ui.Text(c, "Token X").FontSize(myui.TextSize(t, myui.TextLg)).Bold()

		ui.Box(c).Width(t.Space(4))
		for _, s := range a.reg.Sources {
			live := s.Available() == nil
			ui.Box(c).Padding(t.Space(0.5), t.Space(2)).
				Radius(t.Space(2)).Background(t.Surface).Children(func() {
				text := ui.Text(c, s.Name()).FontSize(myui.TextSize(t, myui.TextXs))
				if live {
					text.TextColor(t.Text)
				} else {
					text.TextColor(t.TextMuted)
				}
			})
		}

		ui.Box(c).Grow(1)

		refresh := myui.NewButton(c.Key("reload"), "刷新").
			Icon(icons.RefreshCw()).Ghost().Sm()
		refresh.AlignSelf(ui.Center)
		if a.loading {
			refresh.Disabled(true)
		}
		if refresh.Clicked() {
			a.reload()
		}
		status := fmt.Sprintf("%d 个 session", len(a.session))
		if a.loading {
			status = "读取中…"
		}
		ui.Text(c, status).FontSize(myui.TextSize(t, myui.TextXs)).
			TextColor(t.TextMuted)
	})
}

// main is the sidebar and the page it opens.
func (a *app) main(c *ui.Context, t *ui.Theme) {
	// Sidebar.
	ui.Column(c).Width(t.Space(44)).Fill().Background(t.Surface).
		Padding(t.Space(4), t.Space(3)).Gap(t.Space(1)).Children(func() {
		a.navItem(c, t, 0, "总览", icons.LayoutDashboard())
		a.navItem(c, t, 1, "Sessions", icons.ListChecks())
		a.navItem(c, t, 2, "设置", icons.Settings())
	})

	// Page.
	switch a.page {
	case 1:
		a.sessionsPage(c, t)
	case 2:
		a.settingsPage(c, t)
	default:
		ui.Scroll(c).Grow(1).Children(func() {
			a.dashboardPage(c, t)
		})
	}
}

func (a *app) navItem(c *ui.Context, t *ui.Theme, page int, label string, svg *ui.SVG) {
	open := a.page == page
	item := ui.Row(c).FillWidth().Height(t.Space(9)).Radius(t.Space(2)).
		PaddingX(t.Space(3)).Gap(t.Space(2.5)).AlignItems(ui.Center)
	if open {
		item.Background(t.Background)
		item.Children(func() {
			ui.Box(c).Width(2).FillHeight().Radius(1).Background(t.Accent)
			ui.Icon(c, svg).Size(t.Space(4), t.Space(4)).TextColor(t.Text)
			ui.Text(c, label).FontSize(myui.TextSize(t, myui.TextSm)).Bold()
		})
	} else {
		item.Children(func() {
			ui.Box(c).Width(2).FillHeight()
			ui.Icon(c, svg).Size(t.Space(4), t.Space(4)).TextColor(t.TextMuted)
			ui.Text(c, label).FontSize(myui.TextSize(t, myui.TextSm)).
				TextColor(t.TextMuted)
		})
		if item.Hovered() {
			item.Background(t.Background.Alpha(0.6))
		}
		if item.Clicked() {
			a.page = page
		}
	}
}

// filtered applies the Sessions filters.
func (a *app) filtered() []core.Session {
	out := a.session[:0:0]
	for _, s := range a.session {
		if a.fSource != "" && s.Source != a.fSource {
			continue
		}
		if a.fProject != "" && s.Project != a.fProject {
			continue
		}
		if a.fModel != "" && s.Model != a.fModel {
			continue
		}
		if a.fQuery != "" && !strings.Contains(strings.ToLower(s.Title), strings.ToLower(a.fQuery)) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// tableSignature identifies the filter set; when it changes the selection
// of the table starts over.
func (a *app) tableSignature() string {
	return a.fSource + "|" + a.fProject + "|" + a.fModel + "|" + a.fQuery
}

// fmtTokens renders a token count compactly.
func fmtTokens(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

// fmtCost renders a USD amount.
func fmtCost(v float64) string {
	if v == 0 {
		return "$0.00"
	}
	return fmt.Sprintf("$%.2f", v)
}

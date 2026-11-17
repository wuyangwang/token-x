package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"token-x/core"
	"token-x/providers/codex"
	"token-x/providers/opencode"
)

// registry builds the data-source registry used by both the CLI mode and
// the window.
func registry() *core.Registry {
	home, _ := os.UserHomeDir()
	prices := core.LoadPrices(filepath.Join(home, ".token-x"))
	return core.NewRegistry(
		codex.New("", prices),
		opencode.New(""),
	)
}

// cliList implements the M1 acceptance: list every session with usage and
// print daily totals, without opening a window.
func cliList() error {
	reg := registry()
	sessions, err := reg.AllSessions(core.Filter{IncludeArchived: true})
	if err != nil {
		return err
	}
	for _, s := range sessions {
		fmt.Printf("%-9s %s  %s  %-12s %s\n",
			s.Source, s.Updated.Local().Format("01-02 15:04"), s.ID[:12], s.Model, s.Title)
		fmt.Printf("          tokens in=%d cached=%d write=%d out=%d (reasoning %d)  cost=$%.4f%s\n",
			s.Tokens.Input, s.Tokens.CachedIn, s.Tokens.CacheWrite, s.Tokens.Output,
			s.Tokens.Reasoning, s.Cost.Amount, estMark(s.Cost.Estimated))
	}

	today := core.TokenUsage{}
	var todayCost float64
	day := time.Now().Truncate(24 * time.Hour)
	total := core.TokenUsage{}
	var totalCost float64
	for _, s := range sessions {
		total = total.Add(s.Tokens)
		totalCost += s.Cost.Amount
		if s.Updated.Local().Truncate(24*time.Hour).Equal(day) {
			today = today.Add(s.Tokens)
			todayCost += s.Cost.Amount
		}
	}
	fmt.Printf("\nsessions: %d\n", len(sessions))
	fmt.Printf("today:    %s  $%.4f\n", fmtTokens(today), todayCost)
	fmt.Printf("total:    %s  $%.4f\n", fmtTokens(total), totalCost)
	return nil
}

func estMark(estimated bool) string {
	if estimated {
		return " (estimated)"
	}
	return ""
}

func fmtTokens(t core.TokenUsage) string {
	return fmt.Sprintf("in=%d cached=%d out=%d total=%d",
		t.Input, t.CachedIn, t.Output, t.Total())
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

// app is the state the window shows. Its view builds the interface from
// it, on the main thread, whenever the window needs a frame.
type app struct {
	placeholder bool
}

func (a *app) view(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(16).Padding(24).Children(func() {
		ui.Text(c, "token-x").FontSize(26).Bold()
		ui.Text(c, "数据面板在 M2 接入").TextColor(c.Theme().TextMuted)
	})
}

func windowMain() {
	a := &app{}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:     "token-x",
			Width:     480,
			Height:    360,
			MinWidth:  320,
			MinHeight: 280,
			// Opens where the user left it last time.
			StateKey: "main",
			// The window shows the interface MyGo draws, not a web page.
			Content: ui.View(a.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

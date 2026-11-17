package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"token-x/core"
	"token-x/providers/claude"
	"token-x/providers/codex"
	"token-x/providers/gemini"
	"token-x/providers/opencode"
)

// cliList implements the M1 acceptance: list every session with usage and
// print daily totals, without opening a window.
func cliList() error {
	home, _ := os.UserHomeDir()
	prices := core.LoadPrices(filepath.Join(home, ".token-x"))
	reg := core.NewRegistry(
		codex.New("", prices), opencode.New(""),
		claude.New("", prices), gemini.New("", prices),
	)

	sessions, err := reg.AllSessions(core.Filter{IncludeArchived: true})
	if err != nil {
		return err
	}
	for _, s := range sessions {
		fmt.Printf("%-9s %s  %s  %-12s %s\n",
			s.Source, s.Updated.Local().Format("01-02 15:04"), shortID(s.ID), s.Model, s.Title)
		fmt.Printf("          tokens in=%d cached=%d write=%d out=%d (reasoning %d)  cost=$%.4f%s\n",
			s.Tokens.Input, s.Tokens.CachedIn, s.Tokens.CacheWrite, s.Tokens.Output,
			s.Tokens.Reasoning, s.Cost.Amount, estMark(s.Cost.Estimated))
	}

	today := core.TokenUsage{}
	var todayCost float64
	day := time.Now().Local().Truncate(24 * time.Hour)
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
	fmt.Printf("today:    in=%d cached=%d out=%d  $%.4f\n",
		today.Input, today.CachedIn, today.Output, todayCost)
	fmt.Printf("total:    in=%d cached=%d out=%d  $%.4f\n",
		total.Input, total.CachedIn, total.Output, totalCost)
	return nil
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func estMark(estimated bool) string {
	if estimated {
		return " (estimated)"
	}
	return ""
}

var _ = sort.Ints

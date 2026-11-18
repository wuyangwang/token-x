package main

import (
	"token-x/core"
	"token-x/providers/claude"
	"token-x/providers/codex"
	"token-x/providers/gemini"
	"token-x/providers/opencode"
	"token-x/providers/workbuddy"
)

// newRegistry builds the registry from the agents installed on this machine.
// The source set is detected at startup instead of being fixed: an agent is
// included only when its own local data store is present, so the source filter
// never offers one that has nothing to read.
func newRegistry(prices *core.PriceTable) *core.Registry {
	return core.NewRegistry(installedSources(
		codex.New("", prices), opencode.New(""),
		claude.New("", prices), gemini.New("", prices),
		workbuddy.New("", prices),
	)...)
}

// installedSources keeps the sources whose Available check passes. Each
// provider reports its own local store; a failing check means that agent is
// not installed (or has never been used) here.
func installedSources(sources ...core.DataSource) []core.DataSource {
	out := make([]core.DataSource, 0, len(sources))
	for _, s := range sources {
		if s.Available() == nil {
			out = append(out, s)
		}
	}
	return out
}

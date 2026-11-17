// Package core holds the domain types and the DataSource plugin interface
// that providers (codex, opencode) implement.
package core

import "time"

// TokenUsage carries the per-category token counts of one session.
type TokenUsage struct {
	Input      int64
	CachedIn   int64 // cache read, billed at a discount
	CacheWrite int64
	Output     int64 // includes reasoning where the source does not split it
	Reasoning  int64 // subset of Output when the source reports it separately
}

// Total sums every category.
func (t TokenUsage) Total() int64 {
	return t.Input + t.CachedIn + t.CacheWrite + t.Output
}

// Add accumulates u into t and returns the result.
func (t TokenUsage) Add(u TokenUsage) TokenUsage {
	return TokenUsage{
		Input:      t.Input + u.Input,
		CachedIn:   t.CachedIn + u.CachedIn,
		CacheWrite: t.CacheWrite + u.CacheWrite,
		Output:     t.Output + u.Output,
		Reasoning:  t.Reasoning + u.Reasoning,
	}
}

// CostValue is a USD amount with a flag telling whether it came from the
// agent's own records or was derived from the local price table.
type CostValue struct {
	Amount    float64
	Estimated bool
}

// Session is one agent conversation as a DataSource reports it.
type Session struct {
	ID     string
	Title  string
	Source string // "codex" | "opencode"
	// Project is the working directory the session ran in.
	Project string
	Model   string
	Agent   string // agent/originator label, may be empty

	Created time.Time
	Updated time.Time

	Tokens TokenUsage
	Cost   CostValue

	// Parent is the session id of the parent for subagent sessions, empty
	// for top-level ones.
	Parent string
}

// IsSubagent reports whether the session ran as a child of another.
func (s Session) IsSubagent() bool { return s.Parent != "" }

// Filter narrows a Sessions query. Zero fields mean no restriction.
type Filter struct {
	// Project matches the working directory prefix, empty for all.
	Project string
	// IncludeArchived keeps archived (.zst) codex sessions in the scan.
	IncludeArchived bool
}

// DailyUsage aggregates one local calendar day across sessions.
type DailyUsage struct {
	Day    time.Time // midnight local
	Tokens TokenUsage
	Cost   CostValue
	Models map[string]TokenUsage
}

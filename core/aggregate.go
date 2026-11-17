package core

import (
	"sort"
	"time"
)

// DailyTotals buckets sessions into the last days local calendar days,
// ordered oldest first. Days with no usage still get a zero entry.
func DailyTotals(sessions []Session, days int, now time.Time) []DailyUsage {
	today := now.Local().Truncate(24 * time.Hour)
	out := make([]DailyUsage, days)
	for i := range out {
		out[i] = DailyUsage{
			Day:    today.AddDate(0, 0, i-(days-1)),
			Models: map[string]TokenUsage{},
		}
	}
	byDay := map[string]*DailyUsage{}
	for i := range out {
		byDay[out[i].Day.Format("2006-01-02")] = &out[i]
	}
	for _, s := range sessions {
		u, ok := byDay[s.Updated.Local().Truncate(24*time.Hour).Format("2006-01-02")]
		if !ok {
			continue
		}
		u.Tokens = u.Tokens.Add(s.Tokens)
		u.Cost.Amount += s.Cost.Amount
		u.Cost.Estimated = u.Cost.Estimated || s.Cost.Estimated
		if s.Model != "" {
			u.Models[s.Model] = u.Models[s.Model].Add(s.Tokens)
		}
	}
	return out
}

// ModelTotal is one row of a per-model breakdown.
type ModelTotal struct {
	Model  string
	Tokens TokenUsage
	Cost   CostValue
	Count  int
}

// ModelTotals sums usage per model across sessions, largest cost first.
func ModelTotals(sessions []Session) []ModelTotal {
	type acc struct {
		tokens TokenUsage
		cost   CostValue
		count  int
	}
	byModel := map[string]*acc{}
	for _, s := range sessions {
		a := byModel[s.Model]
		if a == nil {
			a = &acc{}
			byModel[s.Model] = a
		}
		a.tokens = a.tokens.Add(s.Tokens)
		a.cost.Amount += s.Cost.Amount
		a.cost.Estimated = a.cost.Estimated || s.Cost.Estimated
		a.count++
	}
	out := make([]ModelTotal, 0, len(byModel))
	for m, a := range byModel {
		out = append(out, ModelTotal{Model: m, Tokens: a.tokens, Cost: a.cost, Count: a.count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cost.Amount > out[j].Cost.Amount })
	return out
}

// Totals sums usage and cost over a session slice.
func Totals(sessions []Session) (TokenUsage, CostValue) {
	var t TokenUsage
	var c CostValue
	for _, s := range sessions {
		t = t.Add(s.Tokens)
		c.Amount += s.Cost.Amount
		c.Estimated = c.Estimated || s.Cost.Estimated
	}
	return t, c
}

// Distinct returns the distinct values of a field in session order, for
// filter dropdowns.
func Distinct(sessions []Session, key func(Session) string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range sessions {
		v := key(s)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// ModelPrice is the USD price per million tokens of one model.
type ModelPrice struct {
	Input       float64 `json:"input"`
	CachedInput float64 `json:"cachedInput"`
	CacheWrite  float64 `json:"cacheWrite"`
	Output      float64 `json:"output"`
}

// PriceTable maps model names to their prices. It merges a user-edited
// prices.json over the built-in defaults; unknown models use Fallback.
type PriceTable struct {
	mu       sync.RWMutex
	Models   map[string]ModelPrice `json:"models"`
	Fallback ModelPrice            `json:"fallback"`
	path     string
}

// DefaultPrices returns the built-in table. These are OpenAI list prices as
// a starting point: codex may run through a relay whose real billing
// differs, so the UI marks every price-table amount as estimated and the
// settings page lets the user correct each model.
func DefaultPrices() *PriceTable {
	return &PriceTable{
		Models: map[string]ModelPrice{
			"gpt-5.5":    {Input: 1.25, CachedInput: 0.125, Output: 10.0},
			"gpt-6.1":    {Input: 1.25, CachedInput: 0.125, Output: 10.0},
			"gpt-6.1-sol": {Input: 1.25, CachedInput: 0.125, Output: 10.0},
		},
		Fallback: ModelPrice{Input: 1.0, CachedInput: 0.1, Output: 5.0},
	}
}

// LoadPrices reads prices.json from dir, falling back to defaults when it
// does not exist or does not parse.
func LoadPrices(dir string) *PriceTable {
	t := DefaultPrices()
	t.path = filepath.Join(dir, "prices.json")
	data, err := os.ReadFile(t.path)
	if err != nil {
		return t
	}
	var user PriceTable
	if json.Unmarshal(data, &user) != nil {
		return t
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for name, p := range user.Models {
		t.Models[name] = p
	}
	if user.Fallback != (ModelPrice{}) {
		t.Fallback = user.Fallback
	}
	return t
}

// PriceOf returns the price of a model, and whether it was known exactly.
func (t *PriceTable) PriceOf(model string) (ModelPrice, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if p, ok := t.Models[model]; ok {
		return p, true
	}
	return t.Fallback, false
}

// Calc converts token usage of a model into USD. The second return value
// is false when the model had no exact price and the fallback was used.
func (t *PriceTable) Calc(model string, u TokenUsage) (float64, bool) {
	p, known := t.PriceOf(model)
	amount := (float64(u.Input)*p.Input +
		float64(u.CachedIn)*p.CachedInput +
		float64(u.CacheWrite)*p.CacheWrite +
		float64(u.Output)*p.Output) / 1e6
	return amount, known
}

// Save writes the current table to prices.json.
func (t *PriceTable) Save() error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.path == "" {
		return os.ErrNotExist
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return err
	}
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, t.path)
}

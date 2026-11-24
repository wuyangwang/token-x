package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// pricesFile is the user-editable override table in ~/.token-x.
const pricesFile = "prices.json"

// ModelPrice is the USD price per million tokens of one model.
type ModelPrice struct {
	Input       float64 `json:"input"`
	CachedInput float64 `json:"cachedInput"`
	CacheWrite  float64 `json:"cacheWrite"`
	Output      float64 `json:"output"`
}

// PriceTable maps model names to their prices. The effective price of a
// model is decided by the first layer that provides it:
//
//	user prices.json  >  fetched online prices  >  built-in defaults  >  Fallback
//
// userModels remembers which entries came from the user's own file, so a
// later online refresh can replace the fetched layer without touching them.
type PriceTable struct {
	mu         sync.RWMutex
	Models     map[string]ModelPrice `json:"models"`
	Fallback   ModelPrice            `json:"fallback"`
	path       string
	userModels map[string]bool
}

// DefaultPrices returns the built-in table. These are starting-point list
// prices; codex may run through a relay whose real billing differs, so the
// UI marks every price-table amount as estimated and the user can correct
// each model in prices.json.
func DefaultPrices() *PriceTable {
	return &PriceTable{
		Models: map[string]ModelPrice{
			"gpt-5.5":     {Input: 1.25, CachedInput: 0.125, Output: 10.0},
			"gpt-6.1":     {Input: 1.25, CachedInput: 0.125, Output: 10.0},
			"gpt-6.1-sol": {Input: 1.25, CachedInput: 0.125, Output: 10.0},
		},
		Fallback: ModelPrice{Input: 1.0, CachedInput: 0.1, Output: 5.0},
	}
}

// LoadPrices builds the effective table from the built-in defaults, the
// fetched online cache and the user's prices.json, in that order of
// precedence. Missing or damaged files are ignored; the app always ends up
// with a usable table.
func LoadPrices(dir string) *PriceTable {
	t := DefaultPrices()
	t.path = filepath.Join(dir, pricesFile)

	// Fetched layer first: defaults stay unless the online feed prices the
	// same model, and the user still has the final say below.
	if online := LoadOnline(dir); online != nil {
		for name, p := range online.Models {
			t.Models[name] = p
		}
	}

	data, err := os.ReadFile(t.path)
	if err != nil {
		return t
	}
	var user PriceTable
	if json.Unmarshal(data, &user) != nil {
		return t
	}
	t.mu.Lock()
	t.userModels = make(map[string]bool, len(user.Models))
	for name, p := range user.Models {
		t.Models[name] = p
		t.userModels[name] = true
	}
	if user.Fallback != (ModelPrice{}) {
		t.Fallback = user.Fallback
	}
	t.mu.Unlock()
	return t
}

// Replace swaps in a freshly loaded table in place. Sources hold a pointer
// to the table, so they see new prices on their next scan without being
// rebuilt. User overrides are carried in the fresh table by LoadPrices.
func (t *PriceTable) Replace(other *PriceTable) {
	if other == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Models = other.Models
	t.Fallback = other.Fallback
	t.userModels = other.userModels
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

// Save writes the current table to prices.json. Only the JSON-tagged fields
// are persisted; userModels is an in-memory layering detail.
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

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LiteLLMURL is the public model-cost map maintained from each provider's
// published prices. It is the only kind of URL this app ever contacts;
// session data is never uploaded.
const LiteLLMURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// priceFeedURLs are tried in order. The CDN mirror keeps the fetch working
// where GitHub's raw host is hard to reach; a failure is never fatal.
var priceFeedURLs = []string{
	LiteLLMURL,
	"https://cdn.jsdelivr.net/gh/BerriAI/litellm@main/model_prices_and_context_window.json",
}

// FetchOnlinePrices tries each known mirror until one answers. The returned
// Source is the URL that worked, stored with the cached prices.
func FetchOnlinePrices(ctx context.Context, client *http.Client) (map[string]ModelPrice, string, error) {
	var lastErr error
	for _, url := range priceFeedURLs {
		models, err := FetchLiteLLM(ctx, client, url)
		if err == nil && len(models) > 0 {
			return models, url, nil
		}
		if err != nil {
			lastErr = err
		}
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no price feed returned any prices")
	}
	return nil, "", lastErr
}

// onlinePricesFile caches the last successful fetch so a launch with no
// connection still gets real prices from a previous run.
const onlinePricesFile = "prices-online.json"

// OnlinePriceFile is the on-disk shape of the fetched layer.
type OnlinePriceFile struct {
	FetchedAt time.Time             `json:"fetchedAt"`
	Source    string                `json:"source"`
	Models    map[string]ModelPrice `json:"models"`
}

// LoadOnline reads the cached online prices. A missing or damaged file is
// not an error: the app simply runs on defaults until the next fetch.
func LoadOnline(dir string) *OnlinePriceFile {
	data, err := os.ReadFile(filepath.Join(dir, onlinePricesFile))
	if err != nil {
		return nil
	}
	var f OnlinePriceFile
	if json.Unmarshal(data, &f) != nil || len(f.Models) == 0 {
		return nil
	}
	return &f
}

// SaveOnline writes a fetched price layer atomically, next to prices.json.
func SaveOnline(dir string, f *OnlinePriceFile) error {
	if f == nil || len(f.Models) == 0 {
		return fmt.Errorf("no online prices to save")
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, onlinePricesFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// litellmEntry is the slice of a LiteLLM record this app cares about. All
// costs in that file are USD per token; our ModelPrice is USD per million.
type litellmEntry struct {
	Input       float64 `json:"input_cost_per_token"`
	Output      float64 `json:"output_cost_per_token"`
	CachedInput float64 `json:"cache_read_input_token_cost"`
	CacheWrite  float64 `json:"cache_creation_input_token_cost"`
}

// FetchLiteLLM downloads and normalizes the public price map. Keys are
// reduced to the bare model name (the form local agents record), with
// OpenAI's own entry winning over other providers for the same name.
func FetchLiteLLM(ctx context.Context, client *http.Client, url string) (map[string]ModelPrice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("price feed: %s", resp.Status)
	}
	// The file is a few MB; cap it so a bad response cannot exhaust memory.
	// Decode entries one by one: the upstream file also contains non-model
	// records such as "sample_spec", and one odd record must not fail the
	// whole refresh.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("price feed: %w", err)
	}
	entries := make(map[string]litellmEntry, len(raw))
	for key, value := range raw {
		var e litellmEntry
		if json.Unmarshal(value, &e) == nil {
			entries[key] = e
		}
	}
	return normalizeLiteLLM(entries), nil
}

// normalizeLiteLLM flattens provider-prefixed keys and resolves duplicates.
// A bare name (2) outranks other providers (1); "openai/" (3) outranks both,
// since local Codex sessions are billed by OpenAI.
func normalizeLiteLLM(raw map[string]litellmEntry) map[string]ModelPrice {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type candidate struct {
		price ModelPrice
		prio  int
	}
	best := map[string]candidate{}
	for _, key := range keys {
		e := raw[key]
		p := ModelPrice{
			Input:       e.Input * 1e6,
			CachedInput: e.CachedInput * 1e6,
			CacheWrite:  e.CacheWrite * 1e6,
			Output:      e.Output * 1e6,
		}
		if p == (ModelPrice{}) {
			continue
		}
		name := key
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if name == "" {
			continue
		}
		prio := 1
		if !strings.Contains(key, "/") {
			prio = 2
		}
		if strings.HasPrefix(key, "openai/") {
			prio = 3
		}
		if prev, ok := best[name]; ok && prev.prio >= prio {
			continue
		}
		best[name] = candidate{price: p, prio: prio}
	}

	out := make(map[string]ModelPrice, len(best))
	for name, c := range best {
		out[name] = c.price
	}
	return out
}

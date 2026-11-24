package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFetchLiteLLMOverHTTP(t *testing.T) {
	// The real feed also carries non-model records and the odd bad field;
	// neither may break a refresh.
	body := `{
		"sample_spec": {"input_cost_per_token": 0.0, "output_cost_per_token": 0.0, "litellm_provider": "one of ..."},
		"openai/gpt-test": {"input_cost_per_token": 1.25e-6, "output_cost_per_token": 1e-5, "cache_read_input_token_cost": 1.25e-7},
		"azure/gpt-test": {"input_cost_per_token": 9e-6, "output_cost_per_token": 9e-5},
		"vendor/broken": {"input_cost_per_token": "not-a-number"}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	models, err := FetchLiteLLM(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := models["gpt-test"]
	if !ok || p.Input != 1.25 || p.Output != 10 || p.CachedInput != 0.125 {
		t.Fatalf("gpt-test = %+v ok=%v, want OpenAI's entry", p, ok)
	}
	if _, ok := models["broken"]; ok {
		t.Error("a malformed entry should be skipped, not kept")
	}
}

func TestNormalizeLiteLLM(t *testing.T) {
	raw := map[string]litellmEntry{
		"gpt-x":              {Input: 1e-6, Output: 2e-6, CachedInput: 0.25e-6, CacheWrite: 0.5e-6},
		"anthropic/claude-y": {Input: 3e-6, Output: 4e-6},
		"openai/gpt-y":       {Input: 5e-6, Output: 6e-6},
		"azure/gpt-y":        {Input: 7e-6, Output: 8e-6},
		"openai/free-model":  {}, // no prices: must be dropped
	}
	got := normalizeLiteLLM(raw)
	check := func(name string, want ModelPrice) {
		t.Helper()
		p, ok := got[name]
		if !ok || p != want {
			t.Errorf("price[%q] = %+v (ok=%v), want %+v", name, p, ok, want)
		}
	}
	check("gpt-x", ModelPrice{Input: 1, CachedInput: 0.25, CacheWrite: 0.5, Output: 2})
	check("claude-y", ModelPrice{Input: 3, Output: 4})
	// OpenAI's own entry wins over Azure for the same bare name.
	check("gpt-y", ModelPrice{Input: 5, Output: 6})
	if _, ok := got["free-model"]; ok {
		t.Error("a priceless entry should not be in the table")
	}
}

func TestLoadPricesPrecedence(t *testing.T) {
	dir := t.TempDir()
	if err := SaveOnline(dir, &OnlinePriceFile{
		FetchedAt: time.Now(), Source: LiteLLMURL,
		Models: map[string]ModelPrice{
			"gpt-6.1":   {Input: 9, Output: 9}, // overrides the built-in
			"new-model": {Input: 2, Output: 3},
		},
	}); err != nil {
		t.Fatal(err)
	}
	user := `{"models":{"gpt-6.1":{"input":7,"output":7}}}`
	if err := os.WriteFile(filepath.Join(dir, pricesFile), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}

	tbl := LoadPrices(dir)
	if p, ok := tbl.PriceOf("gpt-6.1"); !ok || p.Input != 7 {
		t.Errorf("user override lost: %+v ok=%v", p, ok)
	}
	if p, ok := tbl.PriceOf("new-model"); !ok || p.Input != 2 {
		t.Errorf("online price missing: %+v ok=%v", p, ok)
	}
	if p, ok := tbl.PriceOf("gpt-5.5"); !ok || p.Input != 1.25 {
		t.Errorf("built-in default lost: %+v ok=%v", p, ok)
	}

	// Simulate the app saving a newer fetch and rebuilding the table. The
	// online layer changes, but the user's own entry is re-applied on top.
	if err := SaveOnline(dir, &OnlinePriceFile{
		FetchedAt: time.Now(), Source: LiteLLMURL,
		Models: map[string]ModelPrice{
			"gpt-6.1":   {Input: 99, Output: 99},
			"new-model": {Input: 20, Output: 20},
		},
	}); err != nil {
		t.Fatal(err)
	}
	tbl.Replace(LoadPrices(dir))
	if p, _ := tbl.PriceOf("gpt-6.1"); p.Input != 7 {
		t.Errorf("Replace overwrote a user price: %+v", p)
	}
	if p, _ := tbl.PriceOf("new-model"); p.Input != 20 {
		t.Errorf("Replace did not refresh the online price: %+v", p)
	}
}

func TestOnlineCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if LoadOnline(dir) != nil {
		t.Fatal("empty dir should have no fetched prices")
	}
	want := &OnlinePriceFile{
		FetchedAt: time.Now(), Source: "unit-test",
		Models: map[string]ModelPrice{"m": {Input: 1}},
	}
	if err := SaveOnline(dir, want); err != nil {
		t.Fatal(err)
	}
	got := LoadOnline(dir)
	if got == nil || got.Source != "unit-test" || got.Models["m"].Input != 1 {
		t.Fatalf("round trip = %+v", got)
	}
}

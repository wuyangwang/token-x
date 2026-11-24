package core

import (
	"path/filepath"
	"testing"
)

// cachedSource stands in for a provider whose backing file can be checked
// cheaply. parses counts how many times the file actually had to be read.
type cachedSource struct {
	state    FileState
	cost     float64
	parses   int
	recorded bool
}

func (s *cachedSource) Name() string               { return "fake" }
func (s *cachedSource) Available() error           { return nil }
func (s *cachedSource) DeleteSession(string) error { return nil }
func (s *cachedSource) Sessions(Filter) ([]Session, error) {
	return s.parse()
}

func (s *cachedSource) Refresh(prev map[string]FileCache) ([]Session, map[string]FileCache, error) {
	const key = "transcript.jsonl"
	if c, ok := prev[key]; ok && c.State == s.state {
		return c.Sessions, map[string]FileCache{key: c}, nil
	}
	sessions, _ := s.parse()
	return sessions, map[string]FileCache{key: {State: s.state, Sessions: sessions}}, nil
}

func (s *cachedSource) parse() ([]Session, error) {
	s.parses++
	return []Session{{
		ID: "s1", Source: s.Name(),
		Cost: CostValue{Amount: s.cost, Estimated: !s.recorded},
	}}, nil
}

func TestRefreshFreezesHistoricalCost(t *testing.T) {
	src := &cachedSource{state: FileState{ModTime: 1, Size: 100}, cost: 5}
	reg := NewRegistry(src)

	sessions, caches, err := reg.Refresh(nil)
	if err != nil || len(sessions) != 1 || sessions[0].Cost.Amount != 5 {
		t.Fatalf("first refresh: %+v %+v %v", sessions, caches, err)
	}
	if src.parses != 1 {
		t.Fatalf("parses = %d, want 1", src.parses)
	}

	// The online price table moved to a much higher price. A scan with an
	// unchanged file must keep the amount computed the first time.
	src.cost = 99
	sessions, caches, err = reg.Refresh(caches)
	if err != nil || sessions[0].Cost.Amount != 5 {
		t.Fatalf("cached session was repriced: %+v %v", sessions, err)
	}
	if src.parses != 1 {
		t.Fatalf("unchanged file was reparsed: %d", src.parses)
	}

	// Only a genuinely changed file is parsed again, and only then does it
	// pick up the new price.
	src.state = FileState{ModTime: 2, Size: 100}
	sessions, caches, err = reg.Refresh(caches)
	if err != nil || sessions[0].Cost.Amount != 99 {
		t.Fatalf("changed file kept stale price: %+v %v", sessions, err)
	}
	if src.parses != 2 {
		t.Fatalf("changed file parses = %d, want 2", src.parses)
	}
}

func TestRefreshKeepsRecordedCost(t *testing.T) {
	src := &cachedSource{state: FileState{ModTime: 1, Size: 10}, cost: 3, recorded: true}
	reg := NewRegistry(src)
	_, caches, err := reg.Refresh(nil)
	if err != nil {
		t.Fatal(err)
	}
	src.cost = 50 // price-table estimate changed
	sessions, _, err := reg.Refresh(caches)
	if err != nil {
		t.Fatal(err)
	}
	if sessions[0].Cost.Estimated || sessions[0].Cost.Amount != 3 {
		t.Fatalf("agent-recorded cost was overwritten: %+v", sessions[0].Cost)
	}
}

func TestSessionCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if c := LoadSessionCache(dir); c == nil || len(c.Sources) != 0 {
		t.Fatalf("empty cache = %+v", c)
	}
	want := NewSessionCache(map[string]map[string]FileCache{
		"codex": {
			"a.jsonl": {
				State:    FileState{ModTime: 7, Size: 42},
				Sessions: []Session{{ID: "x", Cost: CostValue{Amount: 1.5, Estimated: true}}},
			},
		},
	})
	if err := SaveSessionCache(dir, want); err != nil {
		t.Fatal(err)
	}
	got := LoadSessionCache(dir)
	if got.Version != sessionCacheVersion {
		t.Fatalf("version = %d", got.Version)
	}
	entry := got.Sources["codex"]["a.jsonl"]
	if entry.State != (FileState{ModTime: 7, Size: 42}) || len(entry.Sessions) != 1 ||
		entry.Sessions[0].Cost.Amount != 1.5 {
		t.Fatalf("round trip = %+v", entry)
	}
	if len(got.Sessions()) != 1 {
		t.Fatalf("flattened sessions = %d", len(got.Sessions()))
	}
}

func TestFileStateOfMissingFile(t *testing.T) {
	if _, ok := FileStateOf(filepath.Join(t.TempDir(), "none")); ok {
		t.Error("a missing file must not produce a valid state")
	}
}

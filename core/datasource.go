package core

import (
	"fmt"
	"sort"
)

// DataSource is the plugin seam for one agent's local data. v1 ships two
// implementations (codex, opencode); later agents plug in here.
type DataSource interface {
	// Name is the source label shown in the UI, e.g. "codex".
	Name() string
	// Available reports why the source cannot be read, or nil when it can.
	Available() error
	// Sessions lists sessions newest first.
	Sessions(filter Filter) ([]Session, error)
	// DeleteSession permanently removes one session and its children.
	DeleteSession(id string) error
}

// CachedSource is implemented by sources whose data lives in backing files
// that can be checked cheaply. Refresh reuses the parse result of unchanged
// files, so a large archive is read once instead of on every launch. A reused
// session keeps its original cost: a later price-table refresh never
// reprices history. Sources without this interface are simply rescanned.
type CachedSource interface {
	DataSource
	// Refresh returns all current sessions and the per-file cache to persist.
	// Entries in prev with a matching FileState are reused verbatim.
	Refresh(prev map[string]FileCache) ([]Session, map[string]FileCache, error)
}

// Registry is the list of sources the app reads.
type Registry struct {
	Sources []DataSource
}

// NewRegistry builds a registry from the given sources. Sources are shown
// in the order passed.
func NewRegistry(sources ...DataSource) *Registry {
	return &Registry{Sources: sources}
}

// Get returns the source with the given name.
func (r *Registry) Get(name string) (DataSource, error) {
	for _, s := range r.Sources {
		if s.Name() == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("unknown source %q", name)
}

// AllSessions lists sessions from every available source, merged newest
// first. Unavailable sources are skipped, not errors.
func (r *Registry) AllSessions(filter Filter) ([]Session, error) {
	var out []Session
	var errs []error
	for _, s := range r.Sources {
		if err := s.Available(); err != nil {
			continue
		}
		sessions, err := s.Sessions(filter)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
			continue
		}
		out = append(out, sessions...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if len(errs) > 0 {
		return out, errs[0]
	}
	return out, nil
}

// Refresh is the cache-aware counterpart of AllSessions. It reuses unchanged
// file results from prev and returns the cache to write back. Sources that
// do not implement CachedSource fall back to a full Sessions scan and get no
// cache entry.
func (r *Registry) Refresh(prev map[string]map[string]FileCache) ([]Session, map[string]map[string]FileCache, error) {
	var out []Session
	next := map[string]map[string]FileCache{}
	var errs []error
	for _, s := range r.Sources {
		if err := s.Available(); err != nil {
			continue
		}
		if cs, ok := s.(CachedSource); ok {
			sessions, cache, err := cs.Refresh(prev[s.Name()])
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
				continue
			}
			out = append(out, sessions...)
			next[s.Name()] = cache
			continue
		}
		sessions, err := s.Sessions(Filter{IncludeArchived: true})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
			continue
		}
		out = append(out, sessions...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if len(errs) > 0 {
		return out, next, errs[0]
	}
	return out, next, nil
}

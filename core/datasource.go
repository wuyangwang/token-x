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

// Registry is the list of sources the app reads.
type Registry struct {
	Sources []DataSource
}

// NewRegistry builds the default registry. Later sources append here.
func NewRegistry(codex, opencode DataSource) *Registry {
	return &Registry{Sources: []DataSource{codex, opencode}}
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

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// sessionCacheVersion must be bumped whenever the cached shapes change, so
// an older cache is ignored instead of silently misread.
const sessionCacheVersion = 1

const sessionCacheFile = "sessions.json"

// FileState is the fingerprint of one backing file: modification time and
// size are enough to tell whether a transcript changed since the last scan.
type FileState struct {
	ModTime int64 `json:"m"`
	Size    int64 `json:"s"`
}

// FileStateOf stats path and returns its fingerprint. The second result is
// false when the file is gone or unreadable; callers then parse it afresh.
func FileStateOf(path string) (FileState, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return FileState{}, false
	}
	return FileState{ModTime: info.ModTime().UnixNano(), Size: info.Size()}, true
}

// FileCache is one file's parse result and the state it was parsed at. A
// session's cost is stored here as computed at that time: reusing the entry
// keeps historical prices fixed even after the online table is refreshed.
type FileCache struct {
	State    FileState `json:"state"`
	Sessions []Session `json:"sessions"`
}

// SessionCache is the disk snapshot, keyed by source name then file path.
// It is a performance cache only: nothing is lost when it is deleted.
type SessionCache struct {
	Version int                             `json:"version"`
	Sources map[string]map[string]FileCache `json:"sources"`
}

// LoadSessionCache reads the snapshot. An absent, old or damaged cache is
// treated as empty, which simply means the next scan is a full one.
func LoadSessionCache(dir string) *SessionCache {
	data, err := os.ReadFile(filepath.Join(dir, sessionCacheFile))
	if err != nil {
		return newSessionCache()
	}
	var c SessionCache
	if json.Unmarshal(data, &c) != nil || c.Version != sessionCacheVersion {
		return newSessionCache()
	}
	if c.Sources == nil {
		c.Sources = map[string]map[string]FileCache{}
	}
	return &c
}

func newSessionCache() *SessionCache {
	return NewSessionCache(nil)
}

// NewSessionCache wraps a set of per-source caches in a versioned snapshot
// ready to be saved.
func NewSessionCache(sources map[string]map[string]FileCache) *SessionCache {
	if sources == nil {
		sources = map[string]map[string]FileCache{}
	}
	return &SessionCache{Version: sessionCacheVersion, Sources: sources}
}

// Sessions flattens every cached session. Ordering is the caller's job,
// since a fresh scan re-sorts everything by update time anyway.
func (c *SessionCache) Sessions() []Session {
	if c == nil {
		return nil
	}
	var out []Session
	for _, files := range c.Sources {
		for _, entry := range files {
			out = append(out, entry.Sessions...)
		}
	}
	return out
}

// SaveSessionCache writes the snapshot atomically. Failure is harmless —
// the next launch just starts from an empty cache.
func SaveSessionCache(dir string, c *SessionCache) error {
	if c == nil {
		return os.ErrInvalid
	}
	c.Version = sessionCacheVersion
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, sessionCacheFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

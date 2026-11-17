package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"token-x/core"
)

// DeleteSession permanently removes a session: every rollout file with its
// UUID (plain and zst, sessions and archived_sessions) and its line in
// session_index.jsonl. The Codex Desktop SQLite history is left alone.
func (s *Source) DeleteSession(id string) error {
	if id == "" {
		return fmt.Errorf("empty session id")
	}
	if err := s.Available(); err != nil {
		return err
	}

	// Guard against a destructive misparse: only proceed when the id looks
	// like the UUID that rollout filenames and the index carry.
	if len(id) != 36 || strings.Count(id, "-") != 4 {
		return fmt.Errorf("not a codex session id: %q", id)
	}

	removed := 0
	roots := []string{filepath.Join(s.home, "sessions"), filepath.Join(s.home, "archived_sessions")}
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".jsonl.zst") {
				return nil
			}
			if rolloutUUID(name) == id {
				if err := os.Remove(path); err != nil {
					return err
				}
				removed++
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if removed == 0 {
		return fmt.Errorf("no rollout file for session %s", id)
	}
	return s.removeFromIndex(id)
}

// removeFromIndex rewrites session_index.jsonl without the id, atomically.
func (s *Source) removeFromIndex(id string) error {
	path := filepath.Join(s.home, "session_index.jsonl")
	file, err := os.Open(path)
	if err != nil {
		return nil // no index: nothing to clean
	}
	kept := []string{}
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		var line struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.ID == id {
			continue
		}
		kept = append(kept, sc.Text())
	}
	file.Close()
	if err := sc.Err(); err != nil {
		return err
	}

	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(out)
	for _, line := range kept {
		if _, err := w.WriteString(line + "\n"); err != nil {
			out.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var _ core.DataSource = (*Source)(nil)

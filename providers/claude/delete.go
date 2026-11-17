package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeleteSession permanently removes a Claude Code session's transcript,
// its subagent transcripts, and its lines in history.jsonl / stats-cache
// is left alone. A session id is a UUID; that is checked before deleting.
func (s *Source) DeleteSession(id string) error {
	if id == "" {
		return fmt.Errorf("empty session id")
	}
	if err := s.Available(); err != nil {
		return err
	}
	if len(id) != 36 || strings.Count(id, "-") != 4 {
		return fmt.Errorf("not a claude session id: %q", id)
	}
	root := filepath.Join(s.home, "projects")
	removed := 0
	var firstErr error
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			if strings.HasSuffix(d.Name(), ".jsonl") && strings.TrimSuffix(d.Name(), ".jsonl") == id {
				if e := os.Remove(path); e != nil {
					firstErr = e
				} else {
					removed++
				}
			}
			return nil
		}
		// The transcript of a sidechain session lives in a directory
		// named after the parent session.
		if d.Name() == id {
			if e := os.RemoveAll(path); e == nil {
				removed++
			} else if firstErr == nil {
				firstErr = e
			}
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return err
	}
	if firstErr != nil {
		return firstErr
	}
	if removed == 0 {
		return fmt.Errorf("no transcript for session %s", id)
	}
	return s.removeFromHistory(id)
}

// removeFromHistory drops entries whose sessionId matches id from
// history.jsonl, rewriting the file atomically.
func (s *Source) removeFromHistory(id string) error {
	path := filepath.Join(s.home, "history.jsonl")
	file, err := os.Open(path)
	if err != nil {
		return nil // no history file: nothing to clean
	}
	kept := []string{}
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		var line struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.SessionID == id {
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

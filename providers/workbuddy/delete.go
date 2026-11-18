package workbuddy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeleteSession permanently removes a WorkBuddy session: its transcript,
// the file-rollback journal and the directory of rendered tool results that
// share its id. The running app's session registry (sessions/<pid>.json) is
// left alone; WorkBuddy owns that file.
func (s *Source) DeleteSession(id string) error {
	if id == "" {
		return fmt.Errorf("empty session id")
	}
	if err := s.Available(); err != nil {
		return err
	}
	// Guard against a destructive misparse: a WorkBuddy session id is a UUID.
	if len(id) != 36 || strings.Count(id, "-") != 4 {
		return fmt.Errorf("not a workbuddy session id: %q", id)
	}
	files, err := s.scan()
	if err != nil {
		return err
	}
	removed := 0
	for _, f := range files {
		match := f.id == id
		if !match {
			match, err = fileHasSession(f.path, id)
			if err != nil {
				continue // unreadable transcript: not a match
			}
		}
		if !match {
			continue
		}
		if err := os.Remove(f.path); err != nil {
			return err
		}
		removed++
		if f.id == id {
			dir := filepath.Dir(f.path)
			// The journal and tool results are only ever keyed by the
			// UUID-based filename, so never RemoveAll an unknown name.
			_ = os.Remove(filepath.Join(dir, id+".file-rollback.ndjson"))
			_ = os.RemoveAll(filepath.Join(dir, id))
		}
	}
	if removed == 0 {
		return fmt.Errorf("no transcript for session %s", id)
	}
	return nil
}

// fileHasSession checks the sessionId recorded inside a transcript, for
// files whose name does not match the id shown in the UI.
func fileHasSession(path, id string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var ev event
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.SessionID == id {
			return true, nil
		}
	}
	return false, sc.Err()
}

// Package gemini reads Gemini CLI session files: one JSON object per
// session under GEMINI_DATA_DIR/tmp/<project>/chats/session-*.json.
package gemini

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"token-x/core"
)

type Source struct {
	home   string
	prices *core.PriceTable
}

func New(home string, prices *core.PriceTable) *Source {
	if home == "" {
		home = os.Getenv("GEMINI_DATA_DIR")
	}
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".gemini")
		} else {
			home = ".gemini"
		}
	}
	return &Source{home: home, prices: prices}
}

func (s *Source) Name() string { return "gemini" }

func (s *Source) Available() error {
	info, err := os.Stat(filepath.Join(s.home, "tmp"))
	if err != nil {
		return fmt.Errorf("gemini tmp dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("gemini tmp dir is not a directory")
	}
	return nil
}

type sessionFile struct {
	path    string
	project string
}

func (s *Source) scan() ([]sessionFile, error) {
	root := filepath.Join(s.home, "tmp")
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	var files []sessionFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if !strings.HasPrefix(name, "session-") || (!strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".jsonl")) {
			return nil
		}
		project := ""
		if rel, relErr := filepath.Rel(root, path); relErr == nil {
			project = filepath.Dir(rel)
		}
		files = append(files, sessionFile{path: path, project: project})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, err
}

type sessionJSON struct {
	SessionID   string    `json:"sessionId"`
	ProjectHash string    `json:"projectHash"`
	StartTime   time.Time `json:"startTime"`
	LastUpdated time.Time `json:"lastUpdated"`
	Messages    []struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Content   json.RawMessage `json:"content"`
		Tokens    *struct {
			Input   int64 `json:"input"`
			Output  int64 `json:"output"`
			Cached  int64 `json:"cached"`
			Thoughts int64 `json:"thoughts"`
			Tool    int64 `json:"tool"`
			Total   int64 `json:"total"`
		} `json:"tokens"`
		Model string `json:"model"`
	} `json:"messages"`
}

func (s *Source) Sessions(_ core.Filter) ([]core.Session, error) {
	files, err := s.scan()
	if err != nil {
		return nil, err
	}
	var out []core.Session
	for _, f := range files {
		sess, parseErr := s.parse(f)
		if parseErr != nil {
			continue
		}
		out = append(out, sess)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

func (s *Source) parse(f sessionFile) (core.Session, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return core.Session{}, err
	}
	var raw sessionJSON
	if json.Unmarshal(data, &raw) != nil {
		return core.Session{}, fmt.Errorf("not a gemini session: %s", f.path)
	}
	sess := core.Session{
		Source:  s.Name(),
		ID:      raw.SessionID,
		Project: f.project,
		Created: raw.StartTime,
		Updated: raw.LastUpdated,
	}
	if sess.ID == "" {
		sess.ID = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(f.path), ".json"), ".jsonl")
	}
	models := map[string]bool{}
	perModel := map[string]core.TokenUsage{}
	for _, m := range raw.Messages {
		if m.Tokens == nil {
			// The first user message doubles as a title preview.
			if m.Type == "user" && sess.Title == "" {
				sess.Title = firstText(m.Content)
			}
			continue
		}
		u := core.TokenUsage{
			Input:    m.Tokens.Input,
			CachedIn: m.Tokens.Cached,
			Output:   m.Tokens.Output + m.Tokens.Thoughts,
		}
		if m.Model == "" {
			m.Model = "gemini"
		}
		models[m.Model] = true
		perModel[m.Model] = perModel[m.Model].Add(u)
		if t, e := time.Parse(time.RFC3339Nano, m.Timestamp); e == nil {
			if sess.Created.IsZero() {
				sess.Created = t
			}
			if t.After(sess.Updated) {
				sess.Updated = t
			}
		}
	}
	var names []string
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	sess.Model = strings.Join(names, ", ")
	for _, name := range names {
		sess.Tokens = sess.Tokens.Add(perModel[name])
		amount, _ := s.prices.Calc(name, perModel[name])
		sess.Cost.Amount += amount
	}
	sess.Cost.Estimated = true
	if sess.Title == "" {
		sess.Title = shortID(sess.ID)
	}
	return sess, nil
}

// firstText lifts the plain text of a Gemini content block for the title.
func firstText(raw json.RawMessage) string {
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		for _, b := range blocks {
			if b.Text != "" {
				return truncate(b.Text, 50)
			}
		}
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return truncate(s, 50)
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (s *Source) DeleteSession(id string) error {
	if id == "" {
		return fmt.Errorf("empty session id")
	}
	if err := s.Available(); err != nil {
		return err
	}
	files, err := s.scan()
	if err != nil {
		return err
	}
	removed := 0
	for _, f := range files {
		data, readErr := os.ReadFile(f.path)
		if readErr != nil {
			continue
		}
		var raw struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(data, &raw) != nil || raw.SessionID != id {
			continue
		}
		if err := os.Remove(f.path); err != nil {
			return err
		}
		removed++
	}
	if removed == 0 {
		return fmt.Errorf("no session file for %s", id)
	}
	return nil
}

var _ core.DataSource = (*Source)(nil)

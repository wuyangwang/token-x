// Package claude reads the local session transcripts of Claude Code:
// JSONL files under CLAUDE_CONFIG_DIR/projects (default ~/.claude/projects).
// Each assistant line repeats the usage of one API turn, so messages are
// deduplicated by their message id before summing.
package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"token-x/core"
)

// Source implements core.DataSource over Claude Code's transcript store.
type Source struct {
	home   string // CLAUDE_CONFIG_DIR, default ~/.claude
	prices *core.PriceTable
}

// New builds a Claude Code source. An empty home uses CLAUDE_CONFIG_DIR or
// the default ~/.claude.
func New(home string, prices *core.PriceTable) *Source {
	if home == "" {
		home = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".claude")
		} else {
			home = ".claude"
		}
	}
	return &Source{home: home, prices: prices}
}

func (s *Source) Name() string { return "claude" }

func (s *Source) Available() error {
	info, err := os.Stat(filepath.Join(s.home, "projects"))
	if err != nil {
		return fmt.Errorf("claude projects dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("claude projects dir is not a directory")
	}
	return nil
}

// ---- JSONL event structures ----

type event struct {
	Type        string    `json:"type"`
	UUID        string    `json:"uuid"`
	Timestamp   time.Time `json:"timestamp"`
	SessionID   string    `json:"sessionId"`
	Cwd         string    `json:"cwd"`
	CostUSD     float64   `json:"costUSD"`
	Summary     string    `json:"summary"`
	CustomTitle string    `json:"customTitle"`
	Message     *message  `json:"message"`
}

type message struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Usage *usage `json:"usage"`
}

type usage struct {
	Input              int64 `json:"input_tokens"`
	Output             int64 `json:"output_tokens"`
	CacheCreationInput int64 `json:"cache_creation_input_tokens"`
	CacheReadInput     int64 `json:"cache_read_input_tokens"`
}

func (u usage) toCore() core.TokenUsage {
	return core.TokenUsage{
		Input:      u.Input,
		CachedIn:   u.CacheReadInput,
		CacheWrite: u.CacheCreationInput,
		Output:     u.Output,
	}
}

// ---- file scanning ----

type sessionFile struct {
	path    string
	id      string
	project string
}

func (s *Source) scan() ([]sessionFile, error) {
	root := filepath.Join(s.home, "projects")
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	var files []sessionFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		id := strings.TrimSuffix(d.Name(), ".jsonl")
		if id == "" {
			return nil
		}
		project := ""
		if rel, relErr := filepath.Rel(root, path); relErr == nil {
			project = filepath.Dir(rel)
			if project == "." {
				project = ""
			}
		}
		files = append(files, sessionFile{path: path, id: id, project: project})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, err
}

// ---- DataSource ----

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

// Refresh skips transcripts whose path, mtime and size have not changed.
// Reused sessions keep the cost computed on their first parse, which is what
// makes historical amounts stable when the price table is updated.
func (s *Source) Refresh(prev map[string]core.FileCache) ([]core.Session, map[string]core.FileCache, error) {
	files, err := s.scan()
	if err != nil {
		return nil, nil, err
	}
	next := make(map[string]core.FileCache, len(files))
	var out []core.Session
	for _, f := range files {
		state, ok := core.FileStateOf(f.path)
		if cached, hit := prev[f.path]; ok && hit && cached.State == state {
			out = append(out, cached.Sessions...)
			next[f.path] = core.FileCache{State: state, Sessions: cached.Sessions}
			continue
		}
		sess, parseErr := s.parse(f)
		if parseErr != nil {
			continue
		}
		out = append(out, sess)
		next[f.path] = core.FileCache{State: state, Sessions: []core.Session{sess}}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, next, nil
}

// titles reads summary/custom-title lines from every transcript so a title
// at the end of a file still names the session.
func (s *Source) titles() map[string]string {
	out := map[string]string{}
	files, err := s.scan()
	if err != nil {
		return out
	}
	for _, f := range files {
		file, err := os.Open(f.path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(file)
		sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
		for sc.Scan() {
			var ev event
			if json.Unmarshal(sc.Bytes(), &ev) != nil {
				continue
			}
			if ev.Type == "custom-title" && ev.CustomTitle != "" {
				out[f.id] = ev.CustomTitle
			} else if ev.Type == "summary" && ev.Summary != "" {
				if _, ok := out[f.id]; !ok {
					out[f.id] = ev.Summary
				}
			}
		}
		file.Close()
	}
	return out
}

func (s *Source) parse(f sessionFile) (core.Session, error) {
	file, err := os.Open(f.path)
	if err != nil {
		return core.Session{}, err
	}
	defer file.Close()

	var sess core.Session
	sess.Source = s.Name()
	sess.ID = f.id
	sess.Project = f.project
	// One turn emits several assistant lines with the same message id, all
	// carrying the same usage. Keep the last value per id and sum those.
	byMessage := map[string]core.TokenUsage{}
	messageModels := map[string]string{}
	var order []string
	var models []string
	modelSeen := map[string]bool{}
	var cost float64
	var recorded bool

	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var ev event
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if !ev.Timestamp.IsZero() {
			if sess.Created.IsZero() {
				sess.Created = ev.Timestamp
			}
			sess.Updated = ev.Timestamp
		}
		if ev.Cwd != "" && sess.Project == "" {
			sess.Project = ev.Cwd
		}
		if ev.Type == "summary" && ev.Summary != "" && sess.Title == "" {
			sess.Title = ev.Summary
		}
		if ev.Type == "custom-title" && ev.CustomTitle != "" {
			sess.Title = ev.CustomTitle
		}
		if ev.Message == nil || ev.Message.Usage == nil {
			continue
		}
		key := ev.Message.ID
		if key == "" {
			key = ev.UUID
		}
		if key == "" {
			key = fmt.Sprintf("line-%d", len(order))
		}
		if _, ok := byMessage[key]; !ok {
			order = append(order, key)
		}
		byMessage[key] = ev.Message.Usage.toCore()
		if ev.Message.Model != "" {
			messageModels[key] = ev.Message.Model
			if !modelSeen[ev.Message.Model] {
				modelSeen[ev.Message.Model] = true
				models = append(models, ev.Message.Model)
			}
		}
		if ev.CostUSD > 0 {
			cost += ev.CostUSD
			recorded = true
		}
	}
	if err := sc.Err(); err != nil {
		return core.Session{}, err
	}
	for _, key := range order {
		sess.Tokens = sess.Tokens.Add(byMessage[key])
	}
	if sess.Updated.IsZero() {
		sess.Updated = sess.Created
	}
	if sess.Title == "" {
		sess.Title = shortID(f.id)
	}
	sess.Model = strings.Join(models, ", ")
	if recorded {
		sess.Cost = core.CostValue{Amount: cost, Estimated: false}
	} else if len(models) > 0 && len(order) > 0 {
		// No recorded cost: price each turn with its own model.
		var amount float64
		for _, key := range order {
			a, _ := s.prices.Calc(messageModels[key], byMessage[key])
			amount += a
		}
		sess.Cost = core.CostValue{Amount: amount, Estimated: true}
	}
	return sess, nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

var _ core.DataSource = (*Source)(nil)

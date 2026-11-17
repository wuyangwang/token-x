// Package codex reads the local data of the Codex CLI: rollout JSONL files
// under CODEX_HOME (default ~\.codex), organized by date, plus zstd
// archives in archived_sessions.
package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"token-x/core"
)

// Source implements core.DataSource over the codex rollout store.
type Source struct {
	home   string // CODEX_HOME
	prices *core.PriceTable
}

// New builds a codex source. An empty home falls back to the default
// location (~\.codex).
func New(home string, prices *core.PriceTable) *Source {
	if home == "" {
		if env := os.Getenv("CODEX_HOME"); env != "" {
			home = env
		} else {
			home = filepath.Join(mustUserHome(), ".codex")
		}
	}
	return &Source{home: home, prices: prices}
}

func mustUserHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

func (s *Source) Name() string { return "codex" }

func (s *Source) Available() error {
	info, err := os.Stat(filepath.Join(s.home, "sessions"))
	if err != nil {
		return fmt.Errorf("codex sessions dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("codex sessions dir is not a directory")
	}
	return nil
}

// ---- rollout event parsing ----

type rolloutEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMeta struct {
	SessionID    string `json:"session_id"`
	Cwd          string `json:"cwd"`
	Originator   string `json:"originator"`
	ModelProvider string `json:"model_provider"`
}

type turnContext struct {
	TurnID string `json:"turn_id"`
	Model  string `json:"model"`
}

type tokenCountPayload struct {
	Type string `json:"type"` // "token_count"
	Info *struct {
		TotalTokenUsage *tokenUsage `json:"total_token_usage"`
		LastTokenUsage  *tokenUsage `json:"last_token_usage"`
	} `json:"info"`
}

type tokenUsageRecordPayload struct {
	TurnID string      `json:"turn_id"`
	Usage  *tokenUsage `json:"usage"`
}

type tokenUsage struct {
	Input            int64 `json:"input_tokens"`
	CachedInput      int64 `json:"cached_input_tokens"`
	CacheWriteInput  int64 `json:"cache_write_input_tokens"`
	Output           int64 `json:"output_tokens"`
	ReasoningOutput  int64 `json:"reasoning_output_tokens"`
}

func (t tokenUsage) toCore() core.TokenUsage {
	return core.TokenUsage{
		Input:      t.Input,
		CachedIn:   t.CachedInput,
		CacheWrite: t.CacheWriteInput,
		Output:     t.Output,
		Reasoning:  t.ReasoningOutput,
	}
}

// rolloutAccumulates token usage per session from one rollout file.
type rolloutAccumulator struct {
	lastTotal *tokenUsage // previous cumulative total, for the delta path
}

// apply parses one line and updates the accumulator. It returns the per-
// response usage increment (may be zero), and the model to attribute it to.
func (a *rolloutAccumulator) apply(ev rolloutEvent, model string) (core.TokenUsage, bool) {
	switch ev.Type {
	case "event_msg":
		var p tokenCountPayload
		if json.Unmarshal(ev.Payload, &p) != nil || p.Type != "token_count" || p.Info == nil {
			return core.TokenUsage{}, false
		}
		// last_token_usage is the usage of the most recent response; the
		// cumulative total is the fallback for older files.
		if p.Info.LastTokenUsage != nil {
			return p.Info.LastTokenUsage.toCore(), true
		}
		if p.Info.TotalTokenUsage == nil {
			return core.TokenUsage{}, false
		}
		total := *p.Info.TotalTokenUsage
		var inc tokenUsage
		if a.lastTotal != nil {
			inc = tokenUsage{
				Input:           total.Input - a.lastTotal.Input,
				CachedInput:     total.CachedInput - a.lastTotal.CachedInput,
				CacheWriteInput: total.CacheWriteInput - a.lastTotal.CacheWriteInput,
				Output:          total.Output - a.lastTotal.Output,
			}
		} else {
			inc = total
		}
		a.lastTotal = &total
		return inc.toCore(), true
	case "token_usage_record":
		var p tokenUsageRecordPayload
		if json.Unmarshal(ev.Payload, &p) != nil || p.Usage == nil {
			return core.TokenUsage{}, false
		}
		return p.Usage.toCore(), true
	}
	return core.TokenUsage{}, false
}

// ---- file scanning ----

type rolloutFile struct {
	path  string
	uuid  string
	zst   bool
}

func (s *Source) scan(filter core.Filter) ([]rolloutFile, error) {
	var files []rolloutFile
	seen := map[string]bool{}
	roots := []string{filepath.Join(s.home, "sessions")}
	if filter.IncludeArchived {
		roots = append(roots, filepath.Join(s.home, "archived_sessions"))
	}
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			name := d.Name()
			var zst bool
			switch {
			case strings.HasSuffix(name, ".jsonl"):
			case strings.HasSuffix(name, ".jsonl.zst"):
				zst = true
			default:
				return nil
			}
			uuid := rolloutUUID(name)
			if uuid == "" || seen[uuid] {
				return nil // a .zst copy of a rollout we already counted
			}
			seen[uuid] = true
			files = append(files, rolloutFile{path: path, uuid: uuid, zst: zst})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

// rolloutUUID extracts the session UUID from a rollout filename, e.g.
// rollout-2026-11-17T01-25-03-01a1e71a-b6e4-....jsonl.
func rolloutUUID(name string) string {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".zst"), ".jsonl")
	if !strings.HasPrefix(base, "rollout-") || len(base) < len("rollout-")+36 {
		return ""
	}
	return base[len(base)-36:]
}

// open returns a line reader for a rollout file, decompressing .zst on the fly.
func (f rolloutFile) open() (*bufio.Scanner, func() error, error) {
	file, err := os.Open(f.path)
	if err != nil {
		return nil, nil, err
	}
	var r io.Reader = file
	closeAll := func() error { return file.Close() }
	if f.zst {
		dec, err := zstd.NewReader(file)
		if err != nil {
			file.Close()
			return nil, nil, err
		}
		r = dec.IOReadCloser()
		closeAll = func() error { dec.Close(); return file.Close() }
	}
	sc := bufio.NewScanner(bytes.NewReader(nil)) // replaced below
	sc = bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024) // rollout lines can hold full prompts
	return sc, closeAll, nil
}

// ---- DataSource ----

func (s *Source) Sessions(filter core.Filter) ([]core.Session, error) {
	files, err := s.scan(filter)
	if err != nil {
		return nil, err
	}
	index := s.threadNames()

	var out []core.Session
	for _, f := range files {
		sess, err := s.parse(f)
		if err != nil {
			continue // unreadable rollout: skip, never fail the whole scan
		}
		if t, ok := index[sess.ID]; ok && t != "" {
			sess.Title = t
		}
		out = append(out, sess)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// threadNames reads session_index.jsonl (id → thread name); best effort.
func (s *Source) threadNames() map[string]string {
	out := map[string]string{}
	file, err := os.Open(filepath.Join(s.home, "session_index.jsonl"))
	if err != nil {
		return out
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		var line struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.ID != "" {
			out[line.ID] = line.ThreadName
		}
	}
	return out
}

func (s *Source) parse(f rolloutFile) (core.Session, error) {
	sc, closeFile, err := f.open()
	if err != nil {
		return core.Session{}, err
	}
	defer closeFile()

	var sess core.Session
	sess.Source = s.Name()
	sess.ID = f.uuid
	var acc rolloutAccumulator
	model := "unknown"
	usage := core.TokenUsage{}
	metaSeen := false

	for sc.Scan() {
		var ev rolloutEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "session_meta":
			var p sessionMeta
			if json.Unmarshal(ev.Payload, &p) == nil {
				sess.ID = p.SessionID
				sess.Project = p.Cwd
				sess.Agent = p.Originator
				sess.Created = ev.Timestamp
				metaSeen = true
			}
			if sess.Created.IsZero() {
				sess.Created = ev.Timestamp
			}
		case "turn_context":
			var p turnContext
			if json.Unmarshal(ev.Payload, &p) == nil && p.Model != "" {
				model = p.Model
			}
		case "event_msg", "token_usage_record":
			if inc, ok := acc.apply(ev, model); ok {
				usage = usage.Add(inc)
			}
		}
		if !ev.Timestamp.IsZero() {
			sess.Updated = ev.Timestamp
		}
	}
	if err := sc.Err(); err != nil {
		return core.Session{}, err
	}
	if !metaSeen && sess.Created.IsZero() {
		return core.Session{}, fmt.Errorf("no session_meta in %s", f.path)
	}
	if sess.Updated.IsZero() {
		sess.Updated = sess.Created
	}
	sess.Model = model
	sess.Tokens = usage
	amount, known := s.prices.Calc(sess.Model, usage)
	sess.Cost = core.CostValue{Amount: amount, Estimated: true}
	_ = known // estimated either way: relay billing may differ from list price
	return sess, nil
}

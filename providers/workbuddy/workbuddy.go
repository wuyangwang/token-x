// Package workbuddy reads the local transcripts of the WorkBuddy desktop app:
// JSONL session files under WORKBUDDY_HOME/projects (default ~/.workbuddy).
// One event carrying providerData.rawUsage is one model request, so a turn
// that emits both a reasoning and a function_call line is counted once.
package workbuddy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"token-x/core"
)

// Source implements core.DataSource over WorkBuddy's transcript store.
type Source struct {
	home   string // WORKBUDDY_HOME, default ~/.workbuddy
	prices *core.PriceTable
}

// New builds a WorkBuddy source. An empty home uses WORKBUDDY_HOME or the
// default location (~/.workbuddy).
func New(home string, prices *core.PriceTable) *Source {
	if home == "" {
		home = os.Getenv("WORKBUDDY_HOME")
	}
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".workbuddy")
		} else {
			home = ".workbuddy"
		}
	}
	return &Source{home: home, prices: prices}
}

func (s *Source) Name() string { return "workbuddy" }

func (s *Source) Available() error {
	info, err := os.Stat(filepath.Join(s.home, "projects"))
	if err != nil {
		return fmt.Errorf("workbuddy projects dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workbuddy projects dir is not a directory")
	}
	return nil
}

// ---- transcript events ----

type event struct {
	Type         string         `json:"type"`
	ID           string         `json:"id"`
	Timestamp    int64          `json:"timestamp"` // Unix milliseconds
	SessionID    string         `json:"sessionId"`
	Cwd          string         `json:"cwd"`
	AiTitle      string         `json:"aiTitle"`
	Role         string         `json:"role"`
	Content      []contentBlock `json:"content"`
	ProviderData *providerData  `json:"providerData"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type providerData struct {
	ConversationRequestID string    `json:"conversationRequestId"`
	MessageID             string    `json:"messageId"`
	StepSeq               int       `json:"stepSeq"`
	Model                 string    `json:"model"`
	RequestModelID        string    `json:"requestModelId"`
	RawUsage              *rawUsage `json:"rawUsage"`
	Usage                 *usage    `json:"usage"`
}

type rawUsage struct {
	PromptTokens             int64      `json:"prompt_tokens"`
	CompletionTokens         int64      `json:"completion_tokens"`
	PromptTokensDetails      *breakdown `json:"prompt_tokens_details"`
	CompletionTokensDetails  *breakdown `json:"completion_tokens_details"`
	PromptCacheHitTokens     int64      `json:"prompt_cache_hit_tokens"`
	CacheCreationInputTokens int64      `json:"cache_creation_input_tokens"`
	CompletionThinkingTokens int64      `json:"completion_thinking_tokens"`
}

type breakdown struct {
	CachedTokens    int64 `json:"cached_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

// usage is WorkBuddy's normalized summary of rawUsage. It is the fallback
// for versions that no longer write the raw OpenAI-style fields.
type usage struct {
	Requests            int         `json:"requests"`
	InputTokens         int64       `json:"inputTokens"`
	OutputTokens        int64       `json:"outputTokens"`
	InputTokensDetails  []breakdown `json:"inputTokensDetails"`
	OutputTokensDetails []breakdown `json:"outputTokensDetails"`
}

func (u *rawUsage) toCore() core.TokenUsage {
	cached := u.PromptCacheHitTokens
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > cached {
		cached = u.PromptTokensDetails.CachedTokens
	}
	input := u.PromptTokens - cached // prompt_tokens includes the cached part
	if input < 0 {
		input = 0
	}
	reasoning := u.CompletionThinkingTokens
	if u.CompletionTokensDetails != nil && u.CompletionTokensDetails.ReasoningTokens > reasoning {
		reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	if reasoning > u.CompletionTokens {
		reasoning = u.CompletionTokens
	}
	return core.TokenUsage{
		Input:      input,
		CachedIn:   cached,
		CacheWrite: u.CacheCreationInputTokens,
		Output:     u.CompletionTokens,
		Reasoning:  reasoning,
	}
}

func (u *usage) toCore() (core.TokenUsage, bool) {
	if u == nil || u.Requests < 1 {
		return core.TokenUsage{}, false
	}
	var cached, reasoning int64
	for _, d := range u.InputTokensDetails {
		cached += d.CachedTokens
	}
	for _, d := range u.OutputTokensDetails {
		reasoning += d.ReasoningTokens
	}
	input := u.InputTokens - cached
	if input < 0 {
		input = 0
	}
	if reasoning > u.OutputTokens {
		reasoning = u.OutputTokens
	}
	return core.TokenUsage{Input: input, CachedIn: cached, Output: u.OutputTokens, Reasoning: reasoning}, true
}

// ---- file scanning ----

type sessionFile struct {
	path    string
	id      string // filename without extension
	project string // encoded project directory name
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
			continue // unreadable transcript: skip, never fail the whole scan
		}
		out = append(out, sess)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// Refresh reuses unchanged transcripts so old sessions keep the price that
// was in effect when they were first parsed.
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

type turn struct {
	model string
	usage core.TokenUsage
}

func (s *Source) parse(f sessionFile) (core.Session, error) {
	file, err := os.Open(f.path)
	if err != nil {
		return core.Session{}, err
	}
	defer file.Close()

	sess := core.Session{Source: s.Name(), ID: f.id}
	// A request can be logged on several lines with the same request id,
	// message id and step; keep one usage record per step.
	seen := map[string]bool{}
	var turns []turn
	var models []string
	modelSeen := map[string]bool{}
	var titleFromUser string
	lineNo := 0

	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		lineNo++
		var ev event
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Timestamp > 0 {
			when := time.UnixMilli(ev.Timestamp)
			if sess.Created.IsZero() {
				sess.Created = when
			}
			sess.Updated = when
		}
		if ev.SessionID != "" {
			sess.ID = ev.SessionID
		}
		if ev.Cwd != "" && sess.Project == "" {
			sess.Project = ev.Cwd
		}
		switch ev.Type {
		case "ai-title":
			if ev.AiTitle != "" {
				sess.Title = truncate(ev.AiTitle, 50)
			}
		case "message":
			if ev.Role == "user" && titleFromUser == "" {
				for _, b := range ev.Content {
					if q := userQuery(b.Text); q != "" {
						titleFromUser = truncate(q, 50)
						break
					}
				}
			}
		}
		pd := ev.ProviderData
		if pd == nil {
			continue
		}
		model := pd.Model
		if model == "" {
			model = pd.RequestModelID
		}
		if model == "" {
			model = "workbuddy"
		}
		u, ok := pd.Usage.toCore()
		if pd.RawUsage != nil {
			u, ok = pd.RawUsage.toCore(), true
		}
		if !ok {
			continue
		}
		if !modelSeen[model] {
			modelSeen[model] = true
			models = append(models, model)
		}
		key := pd.ConversationRequestID + "|" + pd.MessageID + "|" + strconv.Itoa(pd.StepSeq)
		if pd.ConversationRequestID == "" && pd.MessageID == "" {
			key = ev.ID
			if key == "" {
				key = fmt.Sprintf("line-%d", lineNo)
			}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		turns = append(turns, turn{model: model, usage: u})
	}
	if err := sc.Err(); err != nil {
		return core.Session{}, err
	}
	if sess.Project == "" {
		sess.Project = f.project
	}
	if sess.Title == "" {
		sess.Title = titleFromUser
	}
	if sess.Title == "" {
		sess.Title = shortID(sess.ID)
	}
	if sess.Updated.IsZero() {
		sess.Updated = sess.Created
	}
	sess.Model = strings.Join(models, ", ")
	for _, t := range turns {
		sess.Tokens = sess.Tokens.Add(t.usage)
		amount, _ := s.prices.Calc(t.model, t.usage)
		sess.Cost.Amount += amount
	}
	sess.Cost.Estimated = len(turns) > 0
	return sess, nil
}

// userQuery lifts the user's own text out of WorkBuddy's wrapped prompt.
// Desktop messages embed a large <system-reminder> block and end with
// <user_query>...</user_query>; a bare text block is used as-is.
func userQuery(text string) string {
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "<") && !strings.Contains(t, "<user_query>") {
		return ""
	}
	if i := strings.LastIndex(t, "<user_query>"); i >= 0 {
		rest := t[i+len("<user_query>"):]
		if j := strings.Index(rest, "</user_query>"); j >= 0 {
			return strings.TrimSpace(rest[:j])
		}
	}
	return t
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

var _ core.DataSource = (*Source)(nil)

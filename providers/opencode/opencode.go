// Package opencode reads the local data of OpenCode v2.x: a single SQLite
// database (WAL) that holds sessions, messages and usage. Reads are
// read-only; deletions go through the official CLI.
package opencode

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, keeps CGO_ENABLED=0

	"token-x/core"
)

// Source implements core.DataSource over the opencode SQLite store.
type Source struct {
	dataDir string
}

// New builds an opencode source. An empty dataDir falls back to the
// default location (~\.local\share\opencode).
func New(dataDir string) *Source {
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dataDir = filepath.Join(home, ".local", "share", "opencode")
	}
	return &Source{dataDir: dataDir}
}

func (s *Source) Name() string { return "opencode" }

func (s *Source) Available() error {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(s.dataDir, "opencode.db"))+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("opencode.db: %w", err)
	}
	// Fail early (and clearly) when the schema of a future version moved.
	for _, col := range []string{"cost", "tokens_input", "parent_id", "directory", "model"} {
		if !s.hasColumn(db, "session_v2", col) {
			return fmt.Errorf("opencode.db: session_v2 is missing column %q (unsupported version?)", col)
		}
	}
	return nil
}

func (s *Source) hasColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			continue
		}
		if name == column {
			return true
		}
	}
	return false
}

func (s *Source) openRO() (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(s.dataDir, "opencode.db")) + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return db, nil
}

// ---- session_v2 row ----

type modelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Variant    string `json:"variant"`
}

func (s *Source) Sessions(filter core.Filter) ([]core.Session, error) {
	db, err := s.openRO()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// project.worktree is the root path; directory is where the session ran.
	rows, err := db.Query(`
		SELECT s.id, s.title, s.directory, COALESCE(p.worktree, s.directory),
		       s.model, s.agent, s.parent_id,
		       s.cost, s.tokens_input, s.tokens_output, s.tokens_reasoning,
		       s.tokens_cache_read, s.tokens_cache_write,
		       s.time_created, s.time_updated
		FROM session_v2 s
		LEFT JOIN project p ON p.id = s.project_id
		ORDER BY s.time_updated DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.Session
	for rows.Next() {
		var (
			sess      core.Session
			modelJSON string
			parent    sql.NullString
			agent     sql.NullString
			cost      float64
			createdMS, updatedMS int64
			directory, worktree string
		)
		if err := rows.Scan(&sess.ID, &sess.Title, &directory, &worktree,
			&modelJSON, &agent, &parent,
			&cost, &sess.Tokens.Input, &sess.Tokens.Output, &sess.Tokens.Reasoning,
			&sess.Tokens.CachedIn, &sess.Tokens.CacheWrite,
			&createdMS, &updatedMS); err != nil {
			continue
		}
		sess.Source = s.Name()
		sess.Project = worktree
		sess.Agent = agent.String
		sess.Parent = parent.String
		sess.Cost = core.CostValue{Amount: cost, Estimated: false}
		var m modelRef
		if json.Unmarshal([]byte(modelJSON), &m) == nil && m.ID != "" {
			sess.Model = m.ID
		} else {
			sess.Model = "unknown"
		}
		sess.Created = time.UnixMilli(createdMS)
		sess.Updated = time.UnixMilli(updatedMS)
		if sess.Title == "" {
			sess.Title = sess.ID
		}
		if filter.Project != "" && !stringsHasPrefixFold(sess.Project, filter.Project) {
			continue
		}
		out = append(out, sess)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

func stringsHasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && equalFold(s[:len(prefix)], prefix)
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// DeleteSession permanently deletes a session and its children through the
// official CLI, which handles cascades and the live WAL correctly.
func (s *Source) DeleteSession(id string) error {
	if id == "" {
		return fmt.Errorf("empty session id")
	}
	out, err := runCLI(s.dataDir, "session", "delete", id)
	if err != nil {
		return fmt.Errorf("opencode session delete: %v: %s", err, out)
	}
	return nil
}

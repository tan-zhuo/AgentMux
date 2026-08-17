package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS folders (
  id        TEXT PRIMARY KEY,
  name      TEXT NOT NULL,
  parent_id TEXT REFERENCES folders(id) ON DELETE CASCADE,
  sort      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS projects (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  folder_id   TEXT REFERENCES folders(id) ON DELETE SET NULL,
  sort        INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS servers (
  id                TEXT PRIMARY KEY,
  name              TEXT NOT NULL,
  host              TEXT NOT NULL,
  port              INTEGER NOT NULL DEFAULT 22,
  username          TEXT NOT NULL,
  auth_type         TEXT NOT NULL DEFAULT 'agent',
  key_path          TEXT NOT NULL DEFAULT '',
  secret_password   BLOB,
  secret_passphrase BLOB,
  jump_server_id    TEXT REFERENCES servers(id) ON DELETE SET NULL,
  tags              TEXT NOT NULL DEFAULT '[]',
  favorite          INTEGER NOT NULL DEFAULT 0,
  host_key          TEXT NOT NULL DEFAULT '',
  created_at        INTEGER NOT NULL,
  last_ok_at        INTEGER
);

CREATE TABLE IF NOT EXISTS workspaces (
  id                    TEXT PRIMARY KEY,
  project_id            TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  server_id             TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
  name                  TEXT NOT NULL,
  remote_path           TEXT NOT NULL,
  default_tmux_session  TEXT NOT NULL DEFAULT '',
  default_agent_command TEXT NOT NULL DEFAULT '',
  env                   TEXT NOT NULL DEFAULT '{}',
  sort                  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_workspaces_project ON workspaces(project_id);
CREATE INDEX IF NOT EXISTS idx_workspaces_server  ON workspaces(server_id);

CREATE TABLE IF NOT EXISTS agents (
  id            TEXT PRIMARY KEY,
  workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  command       TEXT NOT NULL,
  tmux_session  TEXT NOT NULL,
  tmux_window   TEXT NOT NULL DEFAULT '',
  tmux_pane_id  TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL DEFAULT 'unknown',
  last_seen     INTEGER,
  pid           INTEGER,
  progress_text TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agents_workspace ON agents(workspace_id);

CREATE TABLE IF NOT EXISTS terminal_tabs (
  id           TEXT PRIMARY KEY,
  title        TEXT NOT NULL,
  server_id    TEXT NOT NULL DEFAULT '',
  workspace_id TEXT NOT NULL DEFAULT '',
  agent_id     TEXT NOT NULL DEFAULT '',
  tmux_session TEXT NOT NULL DEFAULT '',
  kind         TEXT NOT NULL DEFAULT 'shell',
  command      TEXT NOT NULL DEFAULT '',
  sort         INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS settings (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS memories (
  id              TEXT PRIMARY KEY,
  kind            TEXT NOT NULL,
  scope           TEXT NOT NULL DEFAULT 'global',
  project_id      TEXT REFERENCES projects(id) ON DELETE CASCADE,
  agent_id        TEXT REFERENCES agents(id)   ON DELETE SET NULL,
  server_id       TEXT REFERENCES servers(id)  ON DELETE SET NULL,
  title           TEXT NOT NULL DEFAULT '',
  body            TEXT NOT NULL,
  redacted        INTEGER NOT NULL DEFAULT 0,
  source          TEXT NOT NULL DEFAULT '',
  importance      REAL NOT NULL DEFAULT 0.5,
  -- float32 little-endian, L2-normalised. NULL until the embedder has run,
  -- which is what makes a memory written while Ollama is down still a memory.
  embedding       BLOB,
  embedding_model TEXT NOT NULL DEFAULT '',
  dim             INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL,
  last_used_at    INTEGER,
  use_count       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_memories_scope    ON memories(scope, project_id);
CREATE INDEX IF NOT EXISTS idx_memories_kind     ON memories(kind);
CREATE INDEX IF NOT EXISTS idx_memories_vecspace ON memories(embedding_model, dim);
`

// Store is the encrypted local database. Every method is safe for concurrent
// use; SQLite is opened in WAL mode with a single writer guarded by wmu.
type Store struct {
	db     *sql.DB
	cipher *Cipher
	wmu    sync.Mutex

	// KeyInFile mirrors Cipher.FellBackToFile for the UI.
	KeyInFile bool
	// Dir is the application data directory.
	Dir string
}

// AppDir returns (and creates) the application data directory.
//
// AGENTMUX_DATA_DIR overrides the default location, which gives you a separate
// profile: a scratch instance for demos or testing that cannot touch the
// servers, keys and layout of your real one.
func AppDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("AGENTMUX_DATA_DIR")); override != "" {
		if err := os.MkdirAll(override, 0o700); err != nil {
			return "", err
		}
		return override, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "AgentMux")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Open initialises the database and the secret cipher.
func Open() (*Store, error) {
	dir, err := AppDir()
	if err != nil {
		return nil, err
	}

	key, usedFile, err := LoadOrCreateMasterKey(dir)
	if err != nil {
		return nil, fmt.Errorf("master key: %w", err)
	}
	c, err := NewCipher(key)
	if err != nil {
		return nil, err
	}
	c.FellBackToFile = usedFile

	dsn := "file:" + filepath.ToSlash(filepath.Join(dir, "agentmux.db")) + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// modernc's driver serialises writes better with a bounded pool.
	db.SetMaxOpenConns(4)

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	migrate(db)

	return &Store{db: db, cipher: c, KeyInFile: usedFile, Dir: dir}, nil
}

// migrate applies additive column changes for databases written by older
// builds. Each statement fails harmlessly when the column already exists.
func migrate(db *sql.DB) {
	for _, stmt := range []string{
		`ALTER TABLE agents ADD COLUMN tmux_pane_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE terminal_tabs ADD COLUMN command TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = db.Exec(stmt)
	}
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// --- settings ---------------------------------------------------------------

// GetSetting returns the stored value or def when absent.
func (s *Store) GetSetting(key, def string) string {
	var v string
	if err := s.db.QueryRow(`SELECT v FROM settings WHERE k = ?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

// SetSetting upserts a settings row.
func (s *Store) SetSetting(key, value string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`INSERT INTO settings(k, v) VALUES(?, ?)
		ON CONFLICT(k) DO UPDATE SET v = excluded.v`, key, value)
	return err
}

// --- helpers ----------------------------------------------------------------

func jsonEncode(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func decodeStrings(raw string) []string {
	out := []string{}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func decodeMap(raw string) map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = map[string]string{}
	}
	return out
}

func nullableString(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}

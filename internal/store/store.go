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
  kind              TEXT NOT NULL DEFAULT 'ssh',
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

CREATE TABLE IF NOT EXISTS skills (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  description     TEXT NOT NULL DEFAULT '',
  trigger_text    TEXT NOT NULL DEFAULT '',
  scope           TEXT NOT NULL DEFAULT 'global',
  project_ids     TEXT NOT NULL DEFAULT '[]',
  agent_types     TEXT NOT NULL DEFAULT '[]',
  steps           TEXT NOT NULL DEFAULT '[]',
  constraints     TEXT NOT NULL DEFAULT '[]',
  examples        TEXT NOT NULL DEFAULT '{}',
  version         INTEGER NOT NULL DEFAULT 1,
  status          TEXT NOT NULL DEFAULT 'draft',
  created_by      TEXT NOT NULL DEFAULT 'user',
  origin_run_id   TEXT NOT NULL DEFAULT '',
  confidence      REAL,
  -- Only an active skill is ever embedded. A draft that could already be
  -- matched would be shaping plans before anyone approved it, which would make
  -- the review step decorative.
  embedding       BLOB,
  embedding_model TEXT NOT NULL DEFAULT '',
  dim             INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  usage_count     INTEGER NOT NULL DEFAULT 0,
  last_used_at    INTEGER,
  success_count   INTEGER NOT NULL DEFAULT 0,
  failure_count   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_skills_status ON skills(status);

CREATE TABLE IF NOT EXISTS skill_versions (
  id         TEXT PRIMARY KEY,
  skill_id   TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
  version    INTEGER NOT NULL,
  snapshot   TEXT NOT NULL,
  note       TEXT NOT NULL DEFAULT '',
  changed_by TEXT NOT NULL DEFAULT 'user',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_skill_versions ON skill_versions(skill_id, version DESC);

CREATE TABLE IF NOT EXISTS orch_runs (
  id          TEXT PRIMARY KEY,
  goal        TEXT NOT NULL,
  trigger     TEXT NOT NULL DEFAULT 'human',
  project_id  TEXT REFERENCES projects(id) ON DELETE SET NULL,
  status      TEXT NOT NULL DEFAULT 'running',
  model       TEXT NOT NULL DEFAULT '',
  skill_ids   TEXT NOT NULL DEFAULT '[]',
  started_at  INTEGER NOT NULL,
  ended_at    INTEGER,
  summary     TEXT NOT NULL DEFAULT '',
  error       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_orch_runs_started ON orch_runs(started_at DESC);

CREATE TABLE IF NOT EXISTS orch_steps (
  id             TEXT PRIMARY KEY,
  run_id         TEXT NOT NULL REFERENCES orch_runs(id) ON DELETE CASCADE,
  seq            INTEGER NOT NULL,
  phase          TEXT NOT NULL,
  tool           TEXT NOT NULL DEFAULT '',
  args           TEXT NOT NULL DEFAULT '{}',
  result         TEXT NOT NULL DEFAULT '',
  reasoning      TEXT NOT NULL DEFAULT '',
  skill_id       TEXT NOT NULL DEFAULT '',
  memory_ids     TEXT NOT NULL DEFAULT '[]',
  injection_flag INTEGER NOT NULL DEFAULT 0,
  risk           TEXT NOT NULL DEFAULT 'read',
  outcome        TEXT NOT NULL DEFAULT '',
  duration_ms    INTEGER NOT NULL DEFAULT 0,
  created_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_orch_steps_run ON orch_steps(run_id, seq);

CREATE TABLE IF NOT EXISTS approvals (
  id         TEXT PRIMARY KEY,
  run_id     TEXT NOT NULL REFERENCES orch_runs(id) ON DELETE CASCADE,
  tool       TEXT NOT NULL,
  args       TEXT NOT NULL DEFAULT '{}',
  risk       TEXT NOT NULL DEFAULT 'act',
  rationale  TEXT NOT NULL DEFAULT '',
  target     TEXT NOT NULL DEFAULT '',
  skill_id   TEXT NOT NULL DEFAULT '',
  injection_flag INTEGER NOT NULL DEFAULT 0,
  status     TEXT NOT NULL DEFAULT 'pending',
  decided_at INTEGER,
  note       TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_approvals_pending ON approvals(status, created_at);
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
		// How much the orchestrator is allowed to do on a server without
		// asking. Existing servers default to 'normal', which asks: a machine
		// configured before this column existed cannot have consented to
		// anything.
		`ALTER TABLE servers ADD COLUMN trust_level TEXT NOT NULL DEFAULT 'normal'`,
		// How the host is reached. Everything written before local hosts existed
		// was reached over SSH, which is what the default says.
		`ALTER TABLE servers ADD COLUMN kind TEXT NOT NULL DEFAULT 'ssh'`,
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

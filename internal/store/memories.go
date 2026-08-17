package store

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Vectors are stored as raw float32, little-endian, so a row costs exactly
// 4×dim bytes and decoding is a copy rather than a parse. JSON would roughly
// triple the size and cost a full unmarshal on every search.

// EncodeVector packs a vector for storage.
func EncodeVector(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// DecodeVector unpacks a stored vector. A blob whose length is not a multiple
// of four is corrupt rather than empty, so it is reported instead of truncated.
func DecodeVector(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("vector blob is %d bytes, not a multiple of 4", len(b))
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v, nil
}

const memoryCols = `id, kind, scope, COALESCE(project_id, ''), COALESCE(agent_id, ''),
	COALESCE(server_id, ''), title, body, redacted, source, importance,
	embedding_model, dim, embedding IS NOT NULL, created_at, last_used_at, use_count`

func scanMemory(sc interface{ Scan(...any) error }) (Memory, error) {
	var (
		m        Memory
		lastUsed sql.NullInt64
	)
	err := sc.Scan(&m.ID, &m.Kind, &m.Scope, &m.ProjectID, &m.AgentID, &m.ServerID,
		&m.Title, &m.Body, &m.Redacted, &m.Source, &m.Importance,
		&m.EmbeddingModel, &m.Dim, &m.HasVector, &m.CreatedAt, &lastUsed, &m.UseCount)
	if err != nil {
		return Memory{}, err
	}
	if lastUsed.Valid {
		v := lastUsed.Int64
		m.LastUsedAt = &v
	}
	return m, nil
}

// where builds the shared filter clause. Keeping listing and vector loading on
// one implementation is what stops the two from drifting into disagreeing about
// what "this project's memories" means.
func (f MemoryFilter) where() (string, []any) {
	var (
		clauses []string
		args    []any
	)
	if f.Scope != "" {
		clauses = append(clauses, `scope = ?`)
		args = append(args, f.Scope)
	}
	if f.ProjectID != "" {
		clauses = append(clauses, `project_id = ?`)
		args = append(args, f.ProjectID)
	}
	if f.AgentID != "" {
		clauses = append(clauses, `agent_id = ?`)
		args = append(args, f.AgentID)
	}
	if len(f.Kinds) > 0 {
		clauses = append(clauses, `kind IN (`+strings.TrimSuffix(strings.Repeat("?,", len(f.Kinds)), ",")+`)`)
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	if t := strings.TrimSpace(f.Text); t != "" {
		clauses = append(clauses, `(title LIKE ? ESCAPE '\' OR body LIKE ? ESCAPE '\')`)
		like := "%" + escapeLike(t) + "%"
		args = append(args, like, like)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// escapeLike neutralises the wildcards in user input, so searching for "100%"
// looks for that text instead of matching everything.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// InsertMemory writes a new memory. The embedding may be nil: a memory written
// while the embedder is unreachable is still worth keeping, and Reindex will
// pick it up later.
func (s *Store) InsertMemory(m Memory) (Memory, error) {
	if strings.TrimSpace(m.Body) == "" {
		return Memory{}, errors.New("a memory needs a body")
	}
	if m.Kind == "" {
		m.Kind = MemProjectFact
	}
	if m.Scope == "" {
		m.Scope = ScopeGlobal
	}
	if m.Importance <= 0 {
		m.Importance = 0.5
	}

	s.wmu.Lock()
	defer s.wmu.Unlock()

	m.ID = uuid.NewString()
	m.CreatedAt = time.Now().Unix()

	var blob any
	if len(m.Embedding) > 0 {
		blob = EncodeVector(m.Embedding)
		m.Dim = len(m.Embedding)
		m.HasVector = true
	}

	_, err := s.db.Exec(`INSERT INTO memories
		(id, kind, scope, project_id, agent_id, server_id, title, body, redacted,
		 source, importance, embedding, embedding_model, dim, created_at, use_count)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0)`,
		m.ID, string(m.Kind), string(m.Scope),
		nullableString(&m.ProjectID), nullableString(&m.AgentID), nullableString(&m.ServerID),
		m.Title, m.Body, m.Redacted, m.Source, m.Importance,
		blob, m.EmbeddingModel, m.Dim, m.CreatedAt)
	return m, err
}

// SetMemoryVector attaches or replaces the embedding of one memory.
func (s *Store) SetMemoryVector(id string, vec []float32, model string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`UPDATE memories SET embedding = ?, embedding_model = ?, dim = ? WHERE id = ?`,
		EncodeVector(vec), model, len(vec), id)
	return err
}

// GetMemory loads one memory, without its vector.
func (s *Store) GetMemory(id string) (Memory, error) {
	row := s.db.QueryRow(`SELECT `+memoryCols+` FROM memories WHERE id = ?`, id)
	m, err := scanMemory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Memory{}, fmt.Errorf("memory %s: %w", id, ErrNotFound)
	}
	return m, err
}

// ListMemories returns matching memories, newest first.
func (s *Store) ListMemories(f MemoryFilter) ([]Memory, error) {
	clause, args := f.where()
	q := `SELECT ` + memoryCols + ` FROM memories` + clause + ` ORDER BY created_at DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, f.Offset)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Memory{}
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MemoryIDs returns just the ids matching a filter.
//
// Semantic search needs to know which rows are eligible, not what they say —
// pulling the bodies as well would move megabytes of text to build a set of
// keys and throw the text away.
func (s *Store) MemoryIDs(f MemoryFilter) ([]string, error) {
	clause, args := f.where()
	rows, err := s.db.Query(`SELECT id FROM memories`+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CountMemories counts matching rows, for paging and for the stats header.
func (s *Store) CountMemories(f MemoryFilter) (int, error) {
	clause, args := f.where()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM memories`+clause, args...).Scan(&n)
	return n, err
}

// DeleteMemory removes one memory.
func (s *Store) DeleteMemory(id string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`DELETE FROM memories WHERE id = ?`, id)
	return err
}

// TouchMemories records that these memories were retrieved and used. Recording
// it is what later lets an unused memory be aged out on evidence rather than on
// a guess.
func (s *Store) TouchMemories(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()

	q := `UPDATE memories SET use_count = use_count + 1, last_used_at = ? WHERE id IN (` +
		strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `)`
	args := make([]any, 0, len(ids)+1)
	args = append(args, time.Now().Unix())
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.db.Exec(q, args...)
	return err
}

// VectorRow is one candidate for a similarity search.
type VectorRow struct {
	ID  string
	Vec []float32
}

// LoadVectors reads every vector in one embedding space. Rows embedded by a
// different model are skipped rather than compared: their numbers are equally
// valid and completely unrelated, so mixing them returns confident nonsense.
func (s *Store) LoadVectors(model string, dim int) ([]VectorRow, error) {
	rows, err := s.db.Query(
		`SELECT id, embedding FROM memories
		 WHERE embedding IS NOT NULL AND embedding_model = ? AND dim = ?`, model, dim)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []VectorRow{}
	for rows.Next() {
		var (
			id   string
			blob []byte
		)
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		v, err := DecodeVector(blob)
		if err != nil {
			return nil, fmt.Errorf("memory %s: %w", id, err)
		}
		out = append(out, VectorRow{ID: id, Vec: v})
	}
	return out, rows.Err()
}

// MemoriesToEmbed returns memories that have no usable vector for this model —
// never embedded, or embedded by a model that is no longer the configured one.
//
// The dimension is not part of the test on purpose: it is a property of the
// model, so a row matching the model name is by definition in the right space.
// Testing it as well would have made every correctly embedded row look pending.
func (s *Store) MemoriesToEmbed(model string, limit int) ([]Memory, error) {
	q := `SELECT ` + memoryCols + ` FROM memories
		WHERE embedding IS NULL OR embedding_model <> ?
		ORDER BY created_at DESC`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.Query(q, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Memory{}
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// VectorSpaces reports how the library is split across embedding models.
func (s *Store) VectorSpaces() ([]VectorSpace, error) {
	rows, err := s.db.Query(
		`SELECT embedding_model, dim, COUNT(*) FROM memories
		 WHERE embedding IS NOT NULL GROUP BY embedding_model, dim ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []VectorSpace{}
	for rows.Next() {
		var v VectorSpace
		if err := rows.Scan(&v.Model, &v.Dim, &v.Count); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

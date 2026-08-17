package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

const serverCols = `id, name, host, port, username, auth_type, key_path,
	secret_password IS NOT NULL AND length(secret_password) > 0,
	secret_passphrase IS NOT NULL AND length(secret_passphrase) > 0,
	jump_server_id, tags, favorite, host_key, created_at, last_ok_at, trust_level`

func scanServer(sc interface{ Scan(...any) error }) (Server, error) {
	var (
		s        Server
		jump     sql.NullString
		tagsRaw  string
		fav      int
		lastOK   sql.NullInt64
		hasPass  int
		hasPhras int
	)
	err := sc.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &s.Username, &s.AuthType, &s.KeyPath,
		&hasPass, &hasPhras, &jump, &tagsRaw, &fav, &s.HostKey, &s.CreatedAt, &lastOK, &s.TrustLevel)
	if err != nil {
		return Server{}, err
	}
	s.HasPassword = hasPass == 1
	s.HasPassphrase = hasPhras == 1
	if jump.Valid {
		v := jump.String
		s.JumpServerID = &v
	}
	if lastOK.Valid {
		v := lastOK.Int64
		s.LastOKAt = &v
	}
	s.Tags = decodeStrings(tagsRaw)
	s.Favorite = fav == 1
	return s, nil
}

// ListServers returns every server ordered by favourite then name.
func (s *Store) ListServers() ([]Server, error) {
	rows, err := s.db.Query(`SELECT ` + serverCols + ` FROM servers ORDER BY favorite DESC, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Server{}
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, srv)
	}
	return out, rows.Err()
}

// GetServer loads one server by id.
func (s *Store) GetServer(id string) (Server, error) {
	row := s.db.QueryRow(`SELECT `+serverCols+` FROM servers WHERE id = ?`, id)
	srv, err := scanServer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Server{}, fmt.Errorf("server %s: %w", id, ErrNotFound)
	}
	return srv, err
}

// SaveServer inserts or updates a server, encrypting any supplied secrets.
func (s *Store) SaveServer(in ServerInput) (Server, error) {
	if strings.TrimSpace(in.Name) == "" {
		return Server{}, errors.New("name is required")
	}
	if strings.TrimSpace(in.Host) == "" {
		return Server{}, errors.New("host is required")
	}
	if strings.TrimSpace(in.Username) == "" {
		return Server{}, errors.New("username is required")
	}
	if in.Port == 0 {
		in.Port = 22
	}
	if in.AuthType == "" {
		in.AuthType = AuthAgent
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}
	switch in.TrustLevel {
	case "":
		// Anything unspecified asks before acting. A server has to be told to
		// trust the orchestrator; it never happens by omission.
		in.TrustLevel = TrustNormal
	case TrustTrusted, TrustNormal, TrustProduction:
	default:
		return Server{}, fmt.Errorf("%q is not a trust level", in.TrustLevel)
	}
	if in.JumpServerID != nil && *in.JumpServerID == in.ID && in.ID != "" {
		return Server{}, errors.New("a server cannot be its own jump host")
	}

	s.wmu.Lock()
	defer s.wmu.Unlock()

	fav := 0
	if in.Favorite {
		fav = 1
	}

	if in.ID == "" {
		in.ID = uuid.NewString()
		pw, err := s.cipher.Seal(deref(in.Password))
		if err != nil {
			return Server{}, err
		}
		pp, err := s.cipher.Seal(deref(in.Passphrase))
		if err != nil {
			return Server{}, err
		}
		_, err = s.db.Exec(`INSERT INTO servers
			(id, name, host, port, username, auth_type, key_path, secret_password, secret_passphrase,
			 jump_server_id, tags, favorite, host_key, created_at, trust_level)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'',?,?)`,
			in.ID, in.Name, in.Host, in.Port, in.Username, string(in.AuthType), in.KeyPath,
			pw, pp, nullableString(in.JumpServerID), jsonEncode(in.Tags), fav, time.Now().Unix(),
			string(in.TrustLevel))
		if err != nil {
			return Server{}, err
		}
		return s.getServerLocked(in.ID)
	}

	// Update. Secrets are only touched when the caller sent a non-nil pointer.
	_, err := s.db.Exec(`UPDATE servers SET
			name = ?, host = ?, port = ?, username = ?, auth_type = ?, key_path = ?,
			jump_server_id = ?, tags = ?, favorite = ?, trust_level = ?
		WHERE id = ?`,
		in.Name, in.Host, in.Port, in.Username, string(in.AuthType), in.KeyPath,
		nullableString(in.JumpServerID), jsonEncode(in.Tags), fav, string(in.TrustLevel), in.ID)
	if err != nil {
		return Server{}, err
	}
	if in.Password != nil {
		blob, err := s.cipher.Seal(*in.Password)
		if err != nil {
			return Server{}, err
		}
		if _, err := s.db.Exec(`UPDATE servers SET secret_password = ? WHERE id = ?`, blob, in.ID); err != nil {
			return Server{}, err
		}
	}
	if in.Passphrase != nil {
		blob, err := s.cipher.Seal(*in.Passphrase)
		if err != nil {
			return Server{}, err
		}
		if _, err := s.db.Exec(`UPDATE servers SET secret_passphrase = ? WHERE id = ?`, blob, in.ID); err != nil {
			return Server{}, err
		}
	}
	return s.getServerLocked(in.ID)
}

func (s *Store) getServerLocked(id string) (Server, error) {
	row := s.db.QueryRow(`SELECT `+serverCols+` FROM servers WHERE id = ?`, id)
	return scanServer(row)
}

// DeleteServer removes a server and, by cascade, its workspaces and agents.
func (s *Store) DeleteServer(id string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`DELETE FROM servers WHERE id = ?`, id)
	return err
}

// Secrets returns the decrypted password and passphrase for a server. This is
// backend-only: no bound service method exposes it to the frontend.
func (s *Store) Secrets(id string) (password, passphrase string, err error) {
	var pw, pp []byte
	err = s.db.QueryRow(`SELECT secret_password, secret_passphrase FROM servers WHERE id = ?`, id).Scan(&pw, &pp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("server %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return "", "", err
	}
	if password, err = s.cipher.Open(pw); err != nil {
		return "", "", err
	}
	if passphrase, err = s.cipher.Open(pp); err != nil {
		return "", "", err
	}
	return password, passphrase, nil
}

// PinHostKey records the trust-on-first-use host key for a server.
func (s *Store) PinHostKey(id, key string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`UPDATE servers SET host_key = ? WHERE id = ?`, key, id)
	return err
}

// MarkServerOK stamps the last successful connection time.
func (s *Store) MarkServerOK(id string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`UPDATE servers SET last_ok_at = ? WHERE id = ?`, time.Now().Unix(), id)
	return err
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

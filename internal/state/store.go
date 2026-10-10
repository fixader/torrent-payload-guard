package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Record struct {
	Hash, Name, Category, Tags, PayloadStatus, ActionTaken, ReportedTo, ReportStatus string
	DangerousFiles                                                                   []string
	FirstSeenAt, LastSeenAt                                                          string
	AddedOn                                                                          int64
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	// The dashboard reads the state database while the scanner writes to it.
	// WAL keeps those operations from blocking each other, while busy_timeout
	// makes short-lived filesystem/backup locks wait instead of disabling an
	// entire poll cycle with SQLITE_BUSY.
	if _, err = db.Exec(`PRAGMA busy_timeout = 10000`); err != nil {
		db.Close()
		return nil, err
	}
	var journalMode string
	if err = db.QueryRow(`PRAGMA journal_mode = WAL`).Scan(&journalMode); err != nil {
		db.Close()
		return nil, err
	}
	if !strings.EqualFold(journalMode, "wal") {
		db.Close()
		return nil, fmt.Errorf("enable SQLite WAL mode: got %q", journalMode)
	}
	if _, err = db.Exec(`PRAGMA synchronous = NORMAL`); err != nil {
		db.Close()
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS torrents (
		hash TEXT PRIMARY KEY, name TEXT NOT NULL, category TEXT, tags TEXT,
		first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL,
		payload_status TEXT NOT NULL, action_taken TEXT NOT NULL DEFAULT '',
		dangerous_files TEXT NOT NULL DEFAULT '[]', reported_to TEXT NOT NULL DEFAULT 'none',
		report_status TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`ALTER TABLE torrents ADD COLUMN added_on INTEGER NOT NULL DEFAULT 0`); err != nil &&
		!strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Get(ctx context.Context, hash string) (Record, bool, error) {
	var r Record
	var files string
	err := s.db.QueryRowContext(ctx, `SELECT hash,name,category,tags,payload_status,action_taken,
		dangerous_files,reported_to,report_status,added_on FROM torrents WHERE hash=?`, hash).Scan(
		&r.Hash, &r.Name, &r.Category, &r.Tags, &r.PayloadStatus, &r.ActionTaken,
		&files, &r.ReportedTo, &r.ReportStatus, &r.AddedOn)
	if err == sql.ErrNoRows {
		return r, false, nil
	}
	if err == nil {
		_ = json.Unmarshal([]byte(files), &r.DangerousFiles)
	}
	return r, err == nil, err
}

func (s *Store) List(ctx context.Context, limit int) ([]Record, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT hash,name,category,tags,payload_status,action_taken,
		dangerous_files,reported_to,report_status,first_seen_at,last_seen_at,added_on
		FROM torrents ORDER BY
		CASE payload_status WHEN 'dangerous' THEN 0 WHEN 'suspicious' THEN 1 WHEN 'allowed' THEN 2 ELSE 3 END,
		last_seen_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Record, 0)
	for rows.Next() {
		var r Record
		var files string
		if err := rows.Scan(&r.Hash, &r.Name, &r.Category, &r.Tags, &r.PayloadStatus,
			&r.ActionTaken, &files, &r.ReportedTo, &r.ReportStatus, &r.FirstSeenAt,
			&r.LastSeenAt, &r.AddedOn); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(files), &r.DangerousFiles)
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) Save(ctx context.Context, r Record) error {
	files, _ := json.Marshal(r.DangerousFiles)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO torrents
		(hash,name,category,tags,first_seen_at,last_seen_at,payload_status,action_taken,dangerous_files,reported_to,report_status,added_on)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(hash) DO UPDATE SET name=excluded.name,category=excluded.category,tags=excluded.tags,
		last_seen_at=excluded.last_seen_at,payload_status=excluded.payload_status,
		action_taken=excluded.action_taken,dangerous_files=excluded.dangerous_files,
		reported_to=excluded.reported_to,report_status=excluded.report_status,added_on=excluded.added_on`,
		r.Hash, r.Name, r.Category, r.Tags, now, now, r.PayloadStatus, r.ActionTaken,
		string(files), r.ReportedTo, r.ReportStatus, r.AddedOn)
	return err
}

func (s *Store) DeleteMissing(ctx context.Context, present map[string]struct{}) error {
	rows, err := s.db.QueryContext(ctx, `SELECT hash FROM torrents`)
	if err != nil {
		return err
	}
	var missing []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			rows.Close()
			return err
		}
		if _, ok := present[hash]; !ok {
			missing = append(missing, hash)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, hash := range missing {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM torrents WHERE hash=?`, hash); err != nil {
			return err
		}
	}
	return nil
}

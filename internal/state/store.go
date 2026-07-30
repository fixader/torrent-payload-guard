package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	_ "modernc.org/sqlite"
)

type Record struct {
	Hash, Name, Category, Tags, PayloadStatus, ActionTaken, ReportedTo, ReportStatus string
	DangerousFiles                                                                   []string
	FirstSeenAt, LastSeenAt                                                          string
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
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
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Get(ctx context.Context, hash string) (Record, bool, error) {
	var r Record
	var files string
	err := s.db.QueryRowContext(ctx, `SELECT hash,name,category,tags,payload_status,action_taken,
		dangerous_files,reported_to,report_status FROM torrents WHERE hash=?`, hash).Scan(
		&r.Hash, &r.Name, &r.Category, &r.Tags, &r.PayloadStatus, &r.ActionTaken,
		&files, &r.ReportedTo, &r.ReportStatus)
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
		dangerous_files,reported_to,report_status,first_seen_at,last_seen_at
		FROM torrents ORDER BY last_seen_at DESC LIMIT ?`, limit)
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
			&r.LastSeenAt); err != nil {
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
		(hash,name,category,tags,first_seen_at,last_seen_at,payload_status,action_taken,dangerous_files,reported_to,report_status)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(hash) DO UPDATE SET name=excluded.name,category=excluded.category,tags=excluded.tags,
		last_seen_at=excluded.last_seen_at,payload_status=excluded.payload_status,
		action_taken=excluded.action_taken,dangerous_files=excluded.dangerous_files,
		reported_to=excluded.reported_to,report_status=excluded.report_status`,
		r.Hash, r.Name, r.Category, r.Tags, now, now, r.PayloadStatus, r.ActionTaken,
		string(files), r.ReportedTo, r.ReportStatus)
	return err
}

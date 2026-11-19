package assistant

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the canvas_assistant_sessions table in the
// desktop's single app database. Same table shape as MySQLStore's minus the
// MySQL engine clauses; messages 是 TEXT 列(MySQL JSON ↔ SQLite TEXT 是
// 仓库既定不对称),时间列 TEXT 定宽 RFC3339 纳秒 UTC(sqlitedb.TimeLayout,
// 与 canvases 同规)。
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

var _ Store = (*SQLiteStore)(nil)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS canvas_assistant_sessions (
	id         INTEGER      NOT NULL PRIMARY KEY AUTOINCREMENT,
	canvas_id  INTEGER      NOT NULL,
	title      TEXT         NOT NULL DEFAULT '',
	mode       TEXT         NOT NULL DEFAULT 'ask',
	messages   TEXT         NOT NULL,
	created_at TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// EnsureSchema creates the canvas_assistant_sessions table when missing.
// 空起步新表、无历史加列,幂等。
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, sqliteSchema)
	return err
}

func scanSQLiteSession(scanner func(dest ...any) error) (Session, error) {
	var (
		s                    Session
		rawMsgs              []byte
		createdAt, updatedAt string
	)
	if err := scanner(&s.ID, &s.CanvasID, &s.Title, &s.Mode, &rawMsgs, &createdAt, &updatedAt); err != nil {
		return Session{}, err
	}
	var err error
	if s.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
		return Session{}, err
	}
	if s.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
		return Session{}, err
	}
	msgs, err := decodeMessages(rawMsgs)
	if err != nil {
		return Session{}, err
	}
	s.Messages = msgs
	return s, nil
}

func (s *SQLiteStore) Create(ctx context.Context, canvasID int64, mode string) (Session, error) {
	if mode == "" {
		mode = DefaultMode
	}
	now := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO canvas_assistant_sessions (canvas_id, title, mode, messages, created_at, updated_at)
		 VALUES (?, '', ?, '[]', ?, ?)`,
		canvasID, mode, now, now)
	if err != nil {
		return Session{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Session{}, err
	}
	return s.Get(ctx, canvasID, id)
}

func (s *SQLiteStore) List(ctx context.Context, canvasID int64) ([]Session, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+sessionColumns+` FROM canvas_assistant_sessions
		WHERE canvas_id = ?
		ORDER BY created_at DESC, id DESC`, canvasID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		s, err := scanSQLiteSession(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Get(ctx context.Context, canvasID, id int64) (Session, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+sessionColumns+` FROM canvas_assistant_sessions
		WHERE id = ? AND canvas_id = ?`, id, canvasID)
	sess, err := scanSQLiteSession(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	return sess, nil
}

// Update patches title / mode / messages(非 nil 字段才写);updated_at 由
// 写入显式带上(SQLite 没有 ON UPDATE CURRENT_TIMESTAMP),照 canvases
// updateColumn 的「探存在 → 更新 → 回读」路径。
func (s *SQLiteStore) Update(ctx context.Context, canvasID, id int64, u *UpdateInput) (Session, error) {
	if _, err := s.Get(ctx, canvasID, id); err != nil {
		return Session{}, err
	}
	if u == nil {
		return s.Get(ctx, canvasID, id)
	}
	now := sqlitedb.FormatTime(time.Now())
	if u.Title != nil {
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE canvas_assistant_sessions SET title = ?, updated_at = ? WHERE id = ?`,
			*u.Title, now, id); err != nil {
			return Session{}, err
		}
	}
	if u.Mode != nil {
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE canvas_assistant_sessions SET mode = ?, updated_at = ? WHERE id = ?`,
			*u.Mode, now, id); err != nil {
			return Session{}, err
		}
	}
	if u.Messages != nil {
		raw, err := encodeMessages(*u.Messages)
		if err != nil {
			return Session{}, err
		}
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE canvas_assistant_sessions SET messages = ?, updated_at = ? WHERE id = ?`,
			raw, now, id); err != nil {
			return Session{}, err
		}
	}
	return s.Get(ctx, canvasID, id)
}

func (s *SQLiteStore) Delete(ctx context.Context, canvasID, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM canvas_assistant_sessions WHERE id = ? AND canvas_id = ?`, id, canvasID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteByCanvas(ctx context.Context, canvasID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM canvas_assistant_sessions WHERE canvas_id = ?`, canvasID)
	return err
}

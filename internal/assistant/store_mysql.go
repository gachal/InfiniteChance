package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// MySQLStore backs Store with the canvas_assistant_sessions table
// (91 号票)。messages 是 JSON 一列,照画布 graph「一列文档」哲学——消息
// 不做行级查询,MySQL JSON 列负责校验与归一(与 canvases.graph 同款,
// MySQL JSON ↔ SQLite TEXT 是仓库既定不对称,对账只比名字不比类型)。
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

var _ Store = (*MySQLStore)(nil)

const mysqlSchema = `
CREATE TABLE IF NOT EXISTS canvas_assistant_sessions (
	id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	canvas_id  BIGINT UNSIGNED NOT NULL,
	title      VARCHAR(128) NOT NULL DEFAULT '',
	mode       VARCHAR(16)  NOT NULL DEFAULT 'ask',
	messages   JSON         NOT NULL,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
	                       ON UPDATE CURRENT_TIMESTAMP(6),
	KEY idx_canvas_assistant_sessions_canvas (canvas_id, created_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the canvas_assistant_sessions table when missing.
// 空起步新表、无历史加列,幂等。
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, mysqlSchema)
	return err
}

const sessionColumns = `id, canvas_id, title, mode, messages, created_at, updated_at`

func scanSession(scanner func(dest ...any) error) (Session, error) {
	var (
		s                    Session
		rawMsgs              []byte
		createdAt, updatedAt sql.NullTime
	)
	if err := scanner(&s.ID, &s.CanvasID, &s.Title, &s.Mode, &rawMsgs, &createdAt, &updatedAt); err != nil {
		return Session{}, err
	}
	if createdAt.Valid {
		s.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		s.UpdatedAt = updatedAt.Time
	}
	msgs, err := decodeMessages(rawMsgs)
	if err != nil {
		return Session{}, err
	}
	s.Messages = msgs
	return s, nil
}

func (s *MySQLStore) Create(ctx context.Context, canvasID int64, mode string) (Session, error) {
	if mode == "" {
		mode = DefaultMode
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO canvas_assistant_sessions (canvas_id, title, mode, messages) VALUES (?, '', ?, ?)`,
		canvasID, mode, `[]`)
	if err != nil {
		return Session{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Session{}, err
	}
	return s.Get(ctx, canvasID, id)
}

func (s *MySQLStore) List(ctx context.Context, canvasID int64) ([]Session, error) {
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
		s, err := scanSession(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *MySQLStore) Get(ctx context.Context, canvasID, id int64) (Session, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+sessionColumns+` FROM canvas_assistant_sessions
		WHERE id = ? AND canvas_id = ?`, id, canvasID)
	sess, err := scanSession(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	return sess, nil
}

// Update patches title / mode / messages(非 nil 字段才写)。updated_at 由
// MySQL 的 ON UPDATE CURRENT_TIMESTAMP 兜底;「值全未变」的行 MySQL 计
// 0 affected 不推时间戳,属可接受(会话未变即无新动静)。
func (s *MySQLStore) Update(ctx context.Context, canvasID, id int64, u *UpdateInput) (Session, error) {
	if _, err := s.Get(ctx, canvasID, id); err != nil {
		return Session{}, err
	}
	if u == nil {
		return s.Get(ctx, canvasID, id)
	}
	if u.Title != nil {
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE canvas_assistant_sessions SET title = ? WHERE id = ?`, *u.Title, id); err != nil {
			return Session{}, err
		}
	}
	if u.Mode != nil {
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE canvas_assistant_sessions SET mode = ? WHERE id = ?`, *u.Mode, id); err != nil {
			return Session{}, err
		}
	}
	if u.Messages != nil {
		raw, err := encodeMessages(*u.Messages)
		if err != nil {
			return Session{}, err
		}
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE canvas_assistant_sessions SET messages = ? WHERE id = ?`, raw, id); err != nil {
			return Session{}, err
		}
	}
	return s.Get(ctx, canvasID, id)
}

func (s *MySQLStore) Delete(ctx context.Context, canvasID, id int64) error {
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

func (s *MySQLStore) DeleteByCanvas(ctx context.Context, canvasID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM canvas_assistant_sessions WHERE canvas_id = ?`, canvasID)
	return err
}

// —— messages JSON 编解码(双方言共用,放本文件供两 store 引用)——

// encodeMessages serializes the persisted turns;nil 切片落「[]」而非
// null(JSON 列 NOT NULL + 前端读端省判空)。
func encodeMessages(msgs []Message) ([]byte, error) {
	if msgs == nil {
		msgs = []Message{}
	}
	return json.Marshal(msgs)
}

// decodeMessages parses the stored JSON;空列(理论不出现,防御兜底)按
// 无消息处理。
func decodeMessages(raw []byte) ([]Message, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var msgs []Message
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

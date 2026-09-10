package channel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the channels table in the desktop's single
// app database. Same rows as MySQLStore's; the vendor secret lives in
// api_key in plaintext — it must be replayable to sign upstream requests;
// the admin API is the only writer and never returns it.
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS channels (
	id           INTEGER      PRIMARY KEY AUTOINCREMENT,
	name         TEXT         NOT NULL,
	type         TEXT         NOT NULL,
	base_url     TEXT         NOT NULL,
	api_key      TEXT         NOT NULL,
	model_map    TEXT         NOT NULL,
	capabilities TEXT         NULL,
	priority     INTEGER      NOT NULL DEFAULT 0,
	weight       INTEGER      NOT NULL DEFAULT 0,
	enabled      INTEGER      NOT NULL DEFAULT 1,
	created_at   TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at   TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// EnsureSchema creates the channels table when missing. 桌面库从零建表:
// 07 号票新增的 capabilities 列直接进 DDL,MySQL 侧 information_schema
// 加宽老表的迁移在这边没有对应物。时间戳统一由写入方用
// sqlitedb.FormatTime 显式提供(默认值仅作 DDL 兜底,其精度是毫秒,
// 不是 sqlitedb.ParseTime 期望的 9 位定宽形式)。幂等。
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, sqliteSchema)
	return err
}

// scanSQLiteRow maps one channels row; the JSON columns come through their
// payloads because SQLite hands TEXT back as strings. 时间列先扫成字符串再
// 用 sqlitedb.ParseTime 转回 time.Time,与 MySQL 版返回相同的 Go 类型。
// NULL capabilities scans to nil — HasCapability reads that as legacy
// chat-only.
func scanSQLiteRow(scan rowScanner) (Channel, error) {
	var ch Channel
	var rawModelMap, rawCapabilities []byte
	var createdAt, updatedAt string
	err := scan.Scan(&ch.ID, &ch.Name, &ch.Type, &ch.BaseURL, &ch.APIKey, &rawModelMap, &rawCapabilities,
		&ch.Priority, &ch.Weight, &ch.Enabled, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, err
	}
	ch.ModelMap = map[string]string{}
	if len(rawModelMap) > 0 {
		if err := json.Unmarshal(rawModelMap, &ch.ModelMap); err != nil {
			return Channel{}, err
		}
	}
	if len(rawCapabilities) > 0 {
		if err := json.Unmarshal(rawCapabilities, &ch.Capabilities); err != nil {
			return Channel{}, err
		}
	}
	if ch.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
		return Channel{}, err
	}
	if ch.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
		return Channel{}, err
	}
	return ch, nil
}

func (s *SQLiteStore) byID(ctx context.Context, id int64) (Channel, error) {
	return scanSQLiteRow(s.DB.QueryRowContext(ctx,
		`SELECT `+channelColumns+` FROM channels WHERE id = ?`, id))
}

func (s *SQLiteStore) List(ctx context.Context) ([]Channel, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+channelColumns+` FROM channels ORDER BY priority DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		ch, err := scanSQLiteRow(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (s *SQLiteStore) Get(ctx context.Context, id int64) (Channel, error) {
	return s.byID(ctx, id)
}

func (s *SQLiteStore) Create(ctx context.Context, ch Channel) (Channel, error) {
	modelMap, err := json.Marshal(ch.ModelMap)
	if err != nil {
		return Channel{}, err
	}
	capabilities, err := json.Marshal(ch.Capabilities)
	if err != nil {
		return Channel{}, err
	}
	// 时间戳显式写入,对应 MySQL 交给服务端 DEFAULT CURRENT_TIMESTAMP(6)。
	now := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO channels (name, type, base_url, api_key, model_map, capabilities, priority, weight, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ch.Name, ch.Type, ch.BaseURL, ch.APIKey, modelMap, capabilities, ch.Priority, ch.Weight, ch.Enabled, now, now)
	if err != nil {
		return Channel{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Channel{}, err
	}
	return s.byID(ctx, id)
}

func (s *SQLiteStore) Update(ctx context.Context, ch Channel) (Channel, error) {
	modelMap, err := json.Marshal(ch.ModelMap)
	if err != nil {
		return Channel{}, err
	}
	capabilities, err := json.Marshal(ch.Capabilities)
	if err != nil {
		return Channel{}, err
	}
	// api_key 为空表示保留原密钥:CASE 在同一行内原子取值,避免先读后写的竞态。
	// updated_at 显式补写,对应 MySQL 的 ON UPDATE CURRENT_TIMESTAMP(6)。
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE channels SET
			name = ?, type = ?, base_url = ?,
			api_key = CASE WHEN ? = '' THEN api_key ELSE ? END,
			model_map = ?, capabilities = ?, priority = ?, weight = ?, enabled = ?,
			updated_at = ?
		 WHERE id = ?`,
		ch.Name, ch.Type, ch.BaseURL,
		ch.APIKey, ch.APIKey,
		modelMap, capabilities, ch.Priority, ch.Weight, ch.Enabled,
		sqlitedb.FormatTime(time.Now()),
		ch.ID); err != nil {
		return Channel{}, err
	}
	// 统一交给 byID 判定:行不存在时它返回 ErrNotFound。
	return s.byID(ctx, ch.ID)
}

func (s *SQLiteStore) Delete(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

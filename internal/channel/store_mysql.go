package channel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// MySQLStore backs Store with the channels table. The vendor secret lives in
// api_key in plaintext — it must be replayable to sign upstream requests;
// the admin API is the only writer and never returns it.
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

const schema = `
CREATE TABLE IF NOT EXISTS channels (
	id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name       VARCHAR(64)  NOT NULL,
	type       VARCHAR(32)  NOT NULL,
	base_url   VARCHAR(512) NOT NULL,
	api_key    TEXT         NOT NULL,
	model_map  JSON         NOT NULL,
	priority   INT          NOT NULL DEFAULT 0,
	weight     INT          NOT NULL DEFAULT 0,
	enabled    TINYINT(1)   NOT NULL DEFAULT 1,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the channels table when missing. It runs at gateway
// startup and is idempotent.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

const channelColumns = `id, name, type, base_url, api_key, model_map, priority, weight, enabled, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

// scanRow maps one channels row; rawModelMap goes through JSON because the
// column is a MySQL JSON value handed to us as bytes.
func scanRow(scan rowScanner) (Channel, error) {
	var ch Channel
	var rawModelMap []byte
	err := scan.Scan(&ch.ID, &ch.Name, &ch.Type, &ch.BaseURL, &ch.APIKey, &rawModelMap,
		&ch.Priority, &ch.Weight, &ch.Enabled, &ch.CreatedAt, &ch.UpdatedAt)
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
	return ch, nil
}

func (s *MySQLStore) byID(ctx context.Context, id int64) (Channel, error) {
	return scanRow(s.DB.QueryRowContext(ctx,
		`SELECT `+channelColumns+` FROM channels WHERE id = ?`, id))
}

func (s *MySQLStore) List(ctx context.Context) ([]Channel, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+channelColumns+` FROM channels ORDER BY priority DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		ch, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (s *MySQLStore) Get(ctx context.Context, id int64) (Channel, error) {
	return s.byID(ctx, id)
}

func (s *MySQLStore) Create(ctx context.Context, ch Channel) (Channel, error) {
	modelMap, err := json.Marshal(ch.ModelMap)
	if err != nil {
		return Channel{}, err
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO channels (name, type, base_url, api_key, model_map, priority, weight, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ch.Name, ch.Type, ch.BaseURL, ch.APIKey, modelMap, ch.Priority, ch.Weight, ch.Enabled)
	if err != nil {
		return Channel{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Channel{}, err
	}
	return s.byID(ctx, id)
}

func (s *MySQLStore) Update(ctx context.Context, ch Channel) (Channel, error) {
	modelMap, err := json.Marshal(ch.ModelMap)
	if err != nil {
		return Channel{}, err
	}
	// api_key 为空表示保留原密钥:CASE 在同一行内原子取值,避免先读后写的竞态。
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE channels SET
			name = ?, type = ?, base_url = ?,
			api_key = CASE WHEN ? = '' THEN api_key ELSE ? END,
			model_map = ?, priority = ?, weight = ?, enabled = ?
		 WHERE id = ?`,
		ch.Name, ch.Type, ch.BaseURL,
		ch.APIKey, ch.APIKey,
		modelMap, ch.Priority, ch.Weight, ch.Enabled,
		ch.ID); err != nil {
		return Channel{}, err
	}
	// MySQL 只计「被更改」的行:affected=0 可能是行不存在,
	// 也可能是新值与旧值完全一致,统一交给 byID 判定。
	return s.byID(ctx, ch.ID)
}

func (s *MySQLStore) Delete(ctx context.Context, id int64) error {
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

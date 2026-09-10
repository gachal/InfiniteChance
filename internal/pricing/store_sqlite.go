package pricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteStore backs Store with the model_prices table in the desktop's
// single app database. The track payload lives in one validated JSON config
// column (stored as TEXT), so the Go structs are the schema.
type SQLiteStore struct {
	DB *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{DB: db} }

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS model_prices (
	public_model TEXT    NOT NULL PRIMARY KEY,
	unit         TEXT    NOT NULL,
	config       TEXT    NOT NULL,
	created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// EnsureSchema creates the model_prices table when missing. It runs at
// startup and is idempotent.
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, sqliteSchema)
	return err
}

// scanSQLiteRow maps one model_prices row; rawConfig goes through the track
// payload structs because the column is a JSON payload stored as TEXT.
// 时间列先扫成字符串再用 sqlitedb.ParseTime 转回 time.Time,与 MySQL 版
// 返回相同的 Go 类型。
func scanSQLiteRow(scan rowScanner) (Price, error) {
	var p Price
	var rawConfig []byte
	var createdAt, updatedAt string
	err := scan.Scan(&p.PublicModel, &p.Unit, &rawConfig, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Price{}, ErrNotFound
	}
	if err != nil {
		return Price{}, err
	}
	switch p.Unit {
	case UnitToken:
		p.Token = &TokenPrice{}
		if err := json.Unmarshal(rawConfig, p.Token); err != nil {
			return Price{}, err
		}
	case UnitCall, UnitSecond:
		p.Call = &CallPrice{}
		if err := json.Unmarshal(rawConfig, p.Call); err != nil {
			return Price{}, err
		}
	default:
		return Price{}, errors.New("pricing: unknown unit " + string(p.Unit) + " in stored row")
	}
	if p.CreatedAt, err = sqlitedb.ParseTime(createdAt); err != nil {
		return Price{}, err
	}
	if p.UpdatedAt, err = sqlitedb.ParseTime(updatedAt); err != nil {
		return Price{}, err
	}
	return p, nil
}

func (s *SQLiteStore) List(ctx context.Context) ([]Price, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+priceColumns+` FROM model_prices ORDER BY public_model ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prices []Price
	for rows.Next() {
		p, err := scanSQLiteRow(rows)
		if err != nil {
			return nil, err
		}
		prices = append(prices, p)
	}
	return prices, rows.Err()
}

func (s *SQLiteStore) ByModel(ctx context.Context, publicModel string) (Price, error) {
	return scanSQLiteRow(s.DB.QueryRowContext(ctx,
		`SELECT `+priceColumns+` FROM model_prices WHERE public_model = ?`, publicModel))
}

func (s *SQLiteStore) Upsert(ctx context.Context, p Price) (Price, error) {
	// unit 决定 config 列的载荷形状(双轨不变量:恰好一个载荷)。
	var payload any = p.Token
	if p.Unit == UnitCall || p.Unit == UnitSecond {
		payload = p.Call
	}
	config, err := json.Marshal(payload)
	if err != nil {
		return Price{}, err
	}
	now := sqlitedb.FormatTime(time.Now())
	// MySQL 的 ON DUPLICATE KEY UPDATE:VALUES(col) 对应 excluded.col;
	// MySQL 在新值与旧值完全一致时不算更改、updated_at 保持不动,
	// DO UPDATE 的 WHERE 复刻这一语义。时间戳显式写入,对应 MySQL 的
	// DEFAULT/ON UPDATE CURRENT_TIMESTAMP(6)。
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO model_prices (public_model, unit, config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(public_model) DO UPDATE SET
			unit = excluded.unit, config = excluded.config, updated_at = ?
		 WHERE unit <> excluded.unit OR config <> excluded.config`,
		p.PublicModel, string(p.Unit), config, now, now, now); err != nil {
		return Price{}, err
	}
	return s.ByModel(ctx, p.PublicModel)
}

func (s *SQLiteStore) Delete(ctx context.Context, publicModel string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM model_prices WHERE public_model = ?`, publicModel)
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

package pricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// MySQLStore backs Store with the model_prices table. The track payload
// lives in one validated JSON config column, so adding the call track
// (ticket 07) needs no migration — the Go structs are the schema.
type MySQLStore struct {
	DB *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

const schema = `
CREATE TABLE IF NOT EXISTS model_prices (
	public_model VARCHAR(200) NOT NULL PRIMARY KEY,
	unit         VARCHAR(16)  NOT NULL,
	config       JSON         NOT NULL,
	created_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the model_prices table when missing. It runs at
// gateway startup and is idempotent.
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

const priceColumns = `public_model, unit, config, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

// scanRow maps one model_prices row; rawConfig goes through the track
// payload structs because the column is a MySQL JSON value handed to us as
// bytes.
func scanRow(scan rowScanner) (Price, error) {
	var p Price
	var rawConfig []byte
	err := scan.Scan(&p.PublicModel, &p.Unit, &rawConfig, &p.CreatedAt, &p.UpdatedAt)
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
	return p, nil
}

func (s *MySQLStore) List(ctx context.Context) ([]Price, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+priceColumns+` FROM model_prices ORDER BY public_model ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prices []Price
	for rows.Next() {
		p, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		prices = append(prices, p)
	}
	return prices, rows.Err()
}

func (s *MySQLStore) ByModel(ctx context.Context, publicModel string) (Price, error) {
	return scanRow(s.DB.QueryRowContext(ctx,
		`SELECT `+priceColumns+` FROM model_prices WHERE public_model = ?`, publicModel))
}

func (s *MySQLStore) Upsert(ctx context.Context, p Price) (Price, error) {
	// unit 决定 config 列的载荷形状(双轨不变量:恰好一个载荷)。
	var payload any = p.Token
	if p.Unit == UnitCall || p.Unit == UnitSecond {
		payload = p.Call
	}
	config, err := json.Marshal(payload)
	if err != nil {
		return Price{}, err
	}
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO model_prices (public_model, unit, config)
		 VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE unit = VALUES(unit), config = VALUES(config)`,
		p.PublicModel, string(p.Unit), config); err != nil {
		return Price{}, err
	}
	return s.ByModel(ctx, p.PublicModel)
}

func (s *MySQLStore) Delete(ctx context.Context, publicModel string) error {
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

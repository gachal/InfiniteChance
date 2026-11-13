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
	capabilities JSON         NULL,
	created_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at   TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`

// EnsureSchema creates the model_prices table when missing and widens an
// already deployed one in place. Idempotent. 88 号票的加列分支同时做一次性
// 渠道能力回填:列刚加上才跑,列已存在绝不重放 —— 管理员事后在价格页的
// 人工收窄不会被启动覆盖(ADR 0008:migrations 增量文件从不执行,
// EnsureSchema 是唯一运行时路径)。channels 表与本表同库,且三个装配入口
// 的 EnsureSchema 顺序都保证 channels 先就位;孤库(无 channels 表)静默
// 跳过回填。
func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, schema); err != nil {
		return err
	}
	added, err := s.ensureColumn(ctx, "model_prices", "capabilities",
		"ALTER TABLE model_prices ADD COLUMN capabilities JSON NULL")
	if err != nil {
		return err
	}
	if added {
		if err := s.backfillCapabilities(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ensureColumn widens one table in place when the column is missing
// (84 号票先例,character 版):检查必须按表参数化,表名是调用方的编译期
// 常量。返回列是否为本次刚加。
func (s *MySQLStore) ensureColumn(ctx context.Context, table, column, ddl string) (bool, error) {
	var count int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
		table, column).Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	if _, err := s.DB.ExecContext(ctx, ddl); err != nil {
		return false, err
	}
	return true, nil
}

// backfillCapabilities does the one-shot channel-capability inheritance
// (88 号票,语义见 backfill.go):channels 表不存在时静默跳过。
func (s *MySQLStore) backfillCapabilities(ctx context.Context) error {
	var tables int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.TABLES
		 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels'`).Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		return nil
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT model_map, capabilities FROM channels WHERE enabled <> 0`)
	if err != nil {
		return err
	}
	defer rows.Close()
	channels, err := scanChannelCaps(rows)
	if err != nil {
		return err
	}
	return backfillPriceCapabilities(ctx, s.DB, channels)
}

const priceColumns = `public_model, unit, config, capabilities, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

// scanRow maps one model_prices row; rawConfig goes through the track
// payload structs because the column is a MySQL JSON value handed to us as
// bytes. capabilities 列(88 号票)NULL 扫 []byte 得 nil,非空才反序列化。
func scanRow(scan rowScanner) (Price, error) {
	var p Price
	var rawConfig, rawCapabilities []byte
	err := scan.Scan(&p.PublicModel, &p.Unit, &rawConfig, &rawCapabilities, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Price{}, ErrNotFound
	}
	if err != nil {
		return Price{}, err
	}
	if len(rawCapabilities) > 0 {
		if err := json.Unmarshal(rawCapabilities, &p.Capabilities); err != nil {
			return Price{}, err
		}
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
	case UnitCharacter:
		p.Character = &CharacterPrice{}
		if err := json.Unmarshal(rawConfig, p.Character); err != nil {
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
	// unit 决定 config 列的载荷形状(多轨不变量:恰好一个载荷)。
	var payload any = p.Token
	if p.Unit == UnitCall || p.Unit == UnitSecond {
		payload = p.Call
	}
	if p.Unit == UnitCharacter {
		payload = p.Character
	}
	config, err := json.Marshal(payload)
	if err != nil {
		return Price{}, err
	}
	// 空能力落真 NULL(nil → NULL),非空落 JSON 数组;读回空 = 不出现
	// 在任何下拉(88 号票合法状态)。
	var capabilities any
	if len(p.Capabilities) > 0 {
		encoded, err := json.Marshal(p.Capabilities)
		if err != nil {
			return Price{}, err
		}
		capabilities = string(encoded)
	}
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO model_prices (public_model, unit, config, capabilities)
		 VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE unit = VALUES(unit), config = VALUES(config),
			capabilities = VALUES(capabilities)`,
		p.PublicModel, string(p.Unit), config, capabilities); err != nil {
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

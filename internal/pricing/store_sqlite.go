package pricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	capabilities TEXT    NULL,
	created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now')),
	updated_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))
)`

// EnsureSchema creates the model_prices table when missing and widens an
// already deployed one in place. Idempotent. 88 号票的加列分支同时做一次性
// 渠道能力回填:列刚加上才跑,列已存在绝不重放 —— 管理员事后在价格页的
// 人工收窄不会被启动覆盖(ADR 0008:migrations 增量文件从不执行,
// EnsureSchema 是唯一运行时路径)。
func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, sqliteSchema); err != nil {
		return err
	}
	added, err := s.ensureColumn(ctx, "model_prices", "capabilities",
		`ALTER TABLE model_prices ADD COLUMN capabilities TEXT`)
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
// (84 号票先例,character 版):PRAGMA 不支持占位符,表名用调用方的编译期
// 常量拼进语句。返回列是否为本次刚加。
func (s *SQLiteStore) ensureColumn(ctx context.Context, table, column, ddl string) (bool, error) {
	rows, err := s.DB.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if _, err := s.DB.ExecContext(ctx, ddl); err != nil {
		return false, err
	}
	return true, nil
}

// backfillCapabilities does the one-shot channel-capability inheritance
// (88 号票): only called from the add-column branch, so it runs exactly once
// per deployed database. For each price row with NULL capabilities it takes
// the union of capability bits of enabled channels whose model_map contains
// the public model name; hunyuan-world-panorama 单名特判仅 ["panorama"](它
// 走 images 渠道,照并集会得 images,违背全景档口径)。Rows on no enabled
// channel's model_map stay NULL = 不出现在任何下拉。channels 表不存在
// (孤库起步)时静默跳过:无渠道即无并集可继承。
func (s *SQLiteStore) backfillCapabilities(ctx context.Context) error {
	var tables int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'channels'`).Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		return nil
	}
	enabledChannels, err := s.channelCapabilities(ctx)
	if err != nil {
		return err
	}
	return backfillPriceCapabilities(ctx, s.DB, enabledChannels)
}

// channelCapabilities reads (model_map → capabilities) facts of enabled
// channels for the one-shot backfill. 渠道能力位 NULL 的历史行不贡献并集
// (只继承显式声明的能力位)。
func (s *SQLiteStore) channelCapabilities(ctx context.Context) ([]channelCaps, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT model_map, capabilities, enabled FROM channels WHERE enabled <> 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChannelCaps(rows)
}

// scanSQLiteRow maps one model_prices row; rawConfig goes through the track
// payload structs because the column is a JSON payload stored as TEXT.
// 时间列先扫成字符串再用 sqlitedb.ParseTime 转回 time.Time,与 MySQL 版
// 返回相同的 Go 类型。capabilities 列(88 号票)NULL 扫 []byte 得 nil,
// 非空才反序列化。
func scanSQLiteRow(scan rowScanner) (Price, error) {
	var p Price
	var rawConfig, rawCapabilities []byte
	var createdAt, updatedAt string
	err := scan.Scan(&p.PublicModel, &p.Unit, &rawConfig, &rawCapabilities, &createdAt, &updatedAt)
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
	// 空能力落真 NULL(nil → NULL),非空落 JSON 数组文本,与 MySQL JSON
	// 列语义对齐;读回空 = 不出现在任何下拉。
	var capabilities any
	if len(p.Capabilities) > 0 {
		encoded, err := json.Marshal(p.Capabilities)
		if err != nil {
			return Price{}, err
		}
		capabilities = string(encoded)
	}
	now := sqlitedb.FormatTime(time.Now())
	// MySQL 的 ON DUPLICATE KEY UPDATE:VALUES(col) 对应 excluded.col;
	// MySQL 在新值与旧值完全一致时不算更改、updated_at 保持不动,
	// DO UPDATE 的 WHERE 复刻这一语义。时间戳显式写入,对应 MySQL 的
	// DEFAULT/ON UPDATE CURRENT_TIMESTAMP(6)。88 号票 WHERE 守卫扩入
	// capabilities 列:只改能力的更新也要前进 updated_at(NULL 与 NULL
	// 比较得 NULL = 不算更改,与值全同一致)。
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO model_prices (public_model, unit, config, capabilities, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(public_model) DO UPDATE SET
			unit = excluded.unit, config = excluded.config, capabilities = excluded.capabilities,
			updated_at = ?
		 WHERE unit <> excluded.unit OR config <> excluded.config OR capabilities <> excluded.capabilities`,
		p.PublicModel, string(p.Unit), config, capabilities, now, now, now); err != nil {
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

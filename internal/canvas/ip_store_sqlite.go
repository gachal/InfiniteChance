package canvas

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// SQLiteIPStore backs IPStore with the ips table in the desktop's single
// app database (50 号票)。时间列说明见 store_sqlite.go:定宽 TEXT,写入
// 显式给 FormatTime。
type SQLiteIPStore struct {
	DB *sql.DB
}

func NewSQLiteIPStore(db *sql.DB) *SQLiteIPStore { return &SQLiteIPStore{DB: db} }

// SQLiteIPStore 与 MySQLIPStore 同构实现 canvas.IPStore。
var _ IPStore = (*SQLiteIPStore)(nil)

// EnsureSchema creates the ips table when missing (canvases store 的
// EnsureSchema 也建同一张表,双保险幂等) and runs the self-heal adoption.
// 依赖 canvases.ip_id 列已就位——装配顺序 canvases → assets → ips
// (canvas/server main.go 与桌面 runtime.go 同序)。
func (s *SQLiteIPStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, sqliteIPsSchema); err != nil {
		return err
	}
	return s.selfHeal(ctx)
}

// selfHeal 是 50 号票的启动自愈迁移:仅当「存在 ip_id=0 的画布行且 ips
// 表为空」时建「默认 IP」并把全部 ip_id=0 画布收编到它名下;新库不预建;
// 条件不看名字,默认 IP 改名后重跑不会重复建。幂等。
func (s *SQLiteIPStore) selfHeal(ctx context.Context) error {
	var zeroCanvases, ipCount int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM canvases WHERE ip_id = 0`).Scan(&zeroCanvases); err != nil {
		return err
	}
	if zeroCanvases == 0 {
		return nil
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ips`).Scan(&ipCount); err != nil {
		return err
	}
	if ipCount > 0 {
		// ips 非空但仍有 ip_id=0 画布:不自动收编(名字不可知,按票面
		// 条件不动)。正常路径到不了这里——画布创建/移动都校验 IP 存在,
		// IP 删除有画布即拒。
		return nil
	}
	now := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO ips (name, description, created_at, updated_at) VALUES (?, '', ?, ?)`,
		DefaultIPName, now, now)
	if err != nil {
		return err
	}
	defaultID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE canvases SET ip_id = ?, updated_at = ? WHERE ip_id = 0`,
		defaultID, sqlitedb.FormatTime(time.Now()))
	return err
}

const sqliteIPColumns = `i.id, i.name, i.description, i.cover_asset_id, i.created_at, i.updated_at`

// sqliteIPListSelect 的排序键 = ips.updated_at 与名下画布 max(updated_at)
// 取大者(SQLite 两参 max 即标量函数,TEXT 定宽时间字典序=时间序)。
const sqliteIPListSelect = `SELECT ` + sqliteIPColumns + `,
	(SELECT COUNT(*) FROM canvases c WHERE c.ip_id = i.id) AS canvas_count,
	max(i.updated_at, COALESCE((SELECT MAX(c2.updated_at) FROM canvases c2 WHERE c2.ip_id = i.id), i.updated_at)) AS sort_key
	FROM ips i
	ORDER BY sort_key DESC, i.id DESC`

func (s *SQLiteIPStore) ListIPs(ctx context.Context) ([]IP, error) {
	rows, err := s.DB.QueryContext(ctx, sqliteIPListSelect)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IP
	for rows.Next() {
		ip, err := scanSQLiteIPRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ip)
	}
	return out, rows.Err()
}

// scanSQLiteIPRow 按列表投影的列序扫描一行(canvas_count 与排序键随行;
// 排序键的时间形态落 LatestAt,即卡片墙「最近更新」)。
func scanSQLiteIPRow(rows *sql.Rows) (IP, error) {
	var ip IP
	var createdAt, updatedAt, sortKey string
	if err := rows.Scan(&ip.ID, &ip.Name, &ip.Description, &ip.CoverAssetID,
		&createdAt, &updatedAt, &ip.CanvasCount, &sortKey); err != nil {
		return IP{}, err
	}
	t, err := sqlitedb.ParseTime(createdAt)
	if err != nil {
		return IP{}, err
	}
	ip.CreatedAt = t
	u, err := sqlitedb.ParseTime(updatedAt)
	if err != nil {
		return IP{}, err
	}
	ip.UpdatedAt = u
	latest, err := sqlitedb.ParseTime(sortKey)
	if err != nil {
		return IP{}, err
	}
	ip.LatestAt = latest
	return ip, nil
}

const sqliteIPGetSelect = `SELECT i.id, i.name, i.description, i.cover_asset_id,
	(SELECT COUNT(*) FROM canvases c WHERE c.ip_id = i.id), i.created_at, i.updated_at
	FROM ips i WHERE i.id = ?`

func (s *SQLiteIPStore) GetIP(ctx context.Context, id int64) (IP, error) {
	var ip IP
	var createdAt, updatedAt string
	err := s.DB.QueryRowContext(ctx, sqliteIPGetSelect, id).
		Scan(&ip.ID, &ip.Name, &ip.Description, &ip.CoverAssetID, &ip.CanvasCount, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IP{}, ErrIPNotFound
	}
	if err != nil {
		return IP{}, err
	}
	t, err := sqlitedb.ParseTime(createdAt)
	if err != nil {
		return IP{}, err
	}
	ip.CreatedAt = t
	u, err := sqlitedb.ParseTime(updatedAt)
	if err != nil {
		return IP{}, err
	}
	ip.UpdatedAt = u
	// Get 的 LatestAt 即自身 updated_at(详情页头部展示;排序键只在列表口径)。
	ip.LatestAt = u
	return ip, nil
}

func (s *SQLiteIPStore) CreateIP(ctx context.Context, name, description string, coverAssetID int64) (IP, error) {
	now := sqlitedb.FormatTime(time.Now())
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO ips (name, description, cover_asset_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		name, description, coverAssetID, now, now)
	if err != nil {
		return IP{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return IP{}, err
	}
	return s.GetIP(ctx, id)
}

// UpdateIP applies the non-nil fields; 先探存在,读回路径与 MySQL 版一致。
func (s *SQLiteIPStore) UpdateIP(ctx context.Context, id int64, u *IPUpdate) (IP, error) {
	if _, err := s.GetIP(ctx, id); err != nil {
		return IP{}, err
	}
	sets := []string{"updated_at = ?"}
	args := []any{sqlitedb.FormatTime(time.Now())}
	if u.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *u.Name)
	}
	if u.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *u.Description)
	}
	if u.CoverAssetID != nil {
		sets = append(sets, "cover_asset_id = ?")
		args = append(args, *u.CoverAssetID)
	}
	args = append(args, id)
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE ips SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return IP{}, err
	}
	return s.GetIP(ctx, id)
}

// DeleteIP 删除前核对两条件:名下有画布或有直接归属素材都拒(ErrIPNotEmpty)。
// 计数走 canvases/assets 表(同库);不级联不归档。
func (s *SQLiteIPStore) DeleteIP(ctx context.Context, id int64) error {
	if _, err := s.GetIP(ctx, id); err != nil {
		return err
	}
	if err := s.assertEmpty(ctx, id); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM ips WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrIPNotFound
	}
	return nil
}

// assertEmpty 是删除拦截的两条件(50 号票):画布与直接归属素材各自计数。
func (s *SQLiteIPStore) assertEmpty(ctx context.Context, id int64) error {
	var canvasCount int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM canvases WHERE ip_id = ?`, id).Scan(&canvasCount); err != nil {
		return err
	}
	if canvasCount > 0 {
		return ErrIPNotEmpty
	}
	var assetCount int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM assets WHERE ip_id = ?`, id).Scan(&assetCount); err != nil {
		return err
	}
	if assetCount > 0 {
		return ErrIPNotEmpty
	}
	return nil
}

func (s *SQLiteIPStore) IPExists(ctx context.Context, id int64) (bool, error) {
	var one int
	err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM ips WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

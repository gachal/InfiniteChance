package canvas

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// MySQLIPStore backs IPStore with the ips table on the canvas database
// (50 号票)。时间列靠表默认值(CURRENT_TIMESTAMP(6) + ON UPDATE),
// 读回即 time.Time。
type MySQLIPStore struct {
	DB *sql.DB
}

func NewMySQLIPStore(db *sql.DB) *MySQLIPStore { return &MySQLIPStore{DB: db} }

// MySQLIPStore 与 SQLiteIPStore 同构实现 canvas.IPStore。
var _ IPStore = (*MySQLIPStore)(nil)

// EnsureSchema creates the ips table when missing (canvases store 的
// EnsureSchema 也建同一张表,双保险幂等) and runs the self-heal adoption.
// 依赖 canvases.ip_id 列已就位——canvas/server main.go 装配顺序
// canvases → assets → ips。
func (s *MySQLIPStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, ipsSchema); err != nil {
		return err
	}
	return s.selfHeal(ctx)
}

// selfHeal 是 50 号票的启动自愈迁移:仅当「存在 ip_id=0 的画布行且 ips
// 表为空」时建「默认 IP」并把全部 ip_id=0 画布收编到它名下;新库不预建;
// 条件不看名字,默认 IP 改名后重跑不会重复建。幂等。
func (s *MySQLIPStore) selfHeal(ctx context.Context) error {
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
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO ips (name, description) VALUES (?, '')`, DefaultIPName)
	if err != nil {
		return err
	}
	defaultID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE canvases SET ip_id = ? WHERE ip_id = 0`, defaultID)
	return err
}

// mysqlIPListSelect 的排序键 = ips.updated_at 与名下画布 max(updated_at)
// 取大者(GREATEST 不接受 NULL 参与,画布空时 COALESCE 回自身)。
const mysqlIPListSelect = `SELECT i.id, i.name, i.description, i.cover_asset_id,
	(SELECT COUNT(*) FROM canvases c WHERE c.ip_id = i.id) AS canvas_count,
	i.created_at, i.updated_at,
	GREATEST(i.updated_at, COALESCE((SELECT MAX(c2.updated_at) FROM canvases c2 WHERE c2.ip_id = i.id), i.updated_at)) AS sort_key
	FROM ips i
	ORDER BY sort_key DESC, i.id DESC`

func (s *MySQLIPStore) ListIPs(ctx context.Context) ([]IP, error) {
	rows, err := s.DB.QueryContext(ctx, mysqlIPListSelect)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IP
	for rows.Next() {
		var ip IP
		if err := rows.Scan(&ip.ID, &ip.Name, &ip.Description, &ip.CoverAssetID,
			&ip.CanvasCount, &ip.CreatedAt, &ip.UpdatedAt, &ip.LatestAt); err != nil {
			return nil, err
		}
		out = append(out, ip)
	}
	return out, rows.Err()
}

func (s *MySQLIPStore) GetIP(ctx context.Context, id int64) (IP, error) {
	var ip IP
	err := s.DB.QueryRowContext(ctx, `SELECT i.id, i.name, i.description, i.cover_asset_id,
		(SELECT COUNT(*) FROM canvases c WHERE c.ip_id = i.id), i.created_at, i.updated_at
		FROM ips i WHERE i.id = ?`, id).
		Scan(&ip.ID, &ip.Name, &ip.Description, &ip.CoverAssetID, &ip.CanvasCount, &ip.CreatedAt, &ip.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IP{}, ErrIPNotFound
	}
	if err != nil {
		return IP{}, err
	}
	ip.LatestAt = ip.UpdatedAt
	return ip, nil
}

func (s *MySQLIPStore) CreateIP(ctx context.Context, name, description string, coverAssetID int64) (IP, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO ips (name, description, cover_asset_id) VALUES (?, ?, ?)`,
		name, description, coverAssetID)
	if err != nil {
		return IP{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return IP{}, err
	}
	return s.GetIP(ctx, id)
}

// UpdateIP applies the non-nil fields; 先探存在(MySQL 同值更新算 0
// affected,缺失语义靠探活路径对齐)。updated_at 由 ON UPDATE 自动推进。
func (s *MySQLIPStore) UpdateIP(ctx context.Context, id int64, u *IPUpdate) (IP, error) {
	if _, err := s.GetIP(ctx, id); err != nil {
		return IP{}, err
	}
	sets := []string{}
	args := []any{}
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
	if len(sets) == 0 {
		return s.GetIP(ctx, id)
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
func (s *MySQLIPStore) DeleteIP(ctx context.Context, id int64) error {
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
func (s *MySQLIPStore) assertEmpty(ctx context.Context, id int64) error {
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

func (s *MySQLIPStore) IPExists(ctx context.Context, id int64) (bool, error) {
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

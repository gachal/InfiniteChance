package pricing

// 88 号票的一次性能力回填,双方言共用:仅由各自 store 的加列分支调用
// (列刚加上才跑,列已存在绝不重放)。对 capabilities 为 NULL 的价格行,
// 取「model_map 含该公开模型名的启用渠道的能力位并集」回填;
// hunyuan-world-panorama 单名特判仅 ["panorama"](单一精确名特判,非启发
// 式;它走 images 渠道,照并集会得 images,违背全景档口径)。不在任何启用
// 渠道 model_map 的价格行保持 NULL = 不出现在任何下拉。

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// channelsModelName 是回填的单名特判对象(票面精确名):全景公开模型。
const channelsModelName = "hunyuan-world-panorama"

// rowQuerier 抽象两方言共享的查询与执行面;*sql.DB 双方皆满足。
type rowQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// channelCaps 是回填用的一条启用渠道事实:映射表与显式能力位。
type channelCaps struct {
	ModelMap     map[string]string
	Capabilities []string
}

// scanChannelCaps maps rows of (model_map, capabilities) — enabled 过滤在
// SQL 里做(两方言 enabled 均为 0/1 整型列,`WHERE enabled <> 0` 同义)。
func scanChannelCaps(rows *sql.Rows) ([]channelCaps, error) {
	var out []channelCaps
	for rows.Next() {
		var rawMap, rawCaps []byte
		var enabled int
		var c channelCaps
		if err := rows.Scan(&rawMap, &rawCaps, &enabled); err != nil {
			return nil, err
		}
		if len(rawMap) > 0 {
			if err := json.Unmarshal(rawMap, &c.ModelMap); err != nil {
				return nil, fmt.Errorf("pricing: decode channel model_map: %w", err)
			}
		}
		if len(rawCaps) > 0 {
			if err := json.Unmarshal(rawCaps, &c.Capabilities); err != nil {
				return nil, fmt.Errorf("pricing: decode channel capabilities: %w", err)
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// backfillPriceCapabilities runs the union inheritance over q (either
// dialect). 幂等且只被加列分支调一次;UPDATE 再带
// `AND capabilities IS NULL` 双保险。
func backfillPriceCapabilities(ctx context.Context, q rowQuerier, channels []channelCaps) error {
	rows, err := q.QueryContext(ctx,
		`SELECT public_model FROM model_prices WHERE capabilities IS NULL`)
	if err != nil {
		return err
	}
	var models []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			rows.Close()
			return err
		}
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, m := range models {
		union := map[string]bool{}
		for _, ch := range channels {
			if _, ok := ch.ModelMap[m]; !ok {
				continue
			}
			for _, c := range ch.Capabilities {
				union[c] = true
			}
		}
		if len(union) == 0 {
			continue
		}
		caps := []string{string(CapPanorama)}
		if m != channelsModelName {
			caps = slices.Sorted(maps.Keys(union))
		}
		encoded, err := json.Marshal(caps)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx,
			`UPDATE model_prices SET capabilities = ? WHERE public_model = ? AND capabilities IS NULL`,
			string(encoded), m); err != nil {
			return err
		}
	}
	return nil
}

package pricing

// 88 号票的一次性能力回填,双方言共用:仅由各自 store 的加列分支调用
// (列刚加上才跑,列已存在绝不重放)。对 capabilities 为 NULL 的价格行,
// 取「model_map 含该公开模型名的启用渠道的生效能力集并集」回填;渠道能力
// 位 NULL 的历史行按 07 号票生效集口径视作仅 chat(缺省视为仅 chat,生图
// 等必须显式开启),其中 chat 只对 token 轨模型成立 —— 非聊天模型剥掉这
// 一默认贡献,无其余并集即保持 NULL。hunyuan-world-panorama 单名特判仅
// ["panorama"](单一精确名特判,非启发式;它走 images 渠道,照并集会得
// images,违背全景档口径)。不在任何启用渠道 model_map 的价格行保持
// NULL = 不出现在任何下拉。

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// panoramaModelName 是回填的单名特判对象(票面精确名):全景公开模型。
const panoramaModelName = "hunyuan-world-panorama"

// rowQuerier 抽象两方言共享的查询与执行面;*sql.DB 双方皆满足。
type rowQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// channelCaps 是回填用的一条启用渠道事实:映射表与生效能力集。
type channelCaps struct {
	ModelMap     map[string]string
	Capabilities []string
}

// scanChannelCaps maps rows of (model_map, capabilities)。capabilities 为
// NULL 的历史渠道行不落空:按 07 号票生效集口径置 ["chat"](与
// channel.Channel.HasCapability 的 NULL→仅 chat 语义一致,取
// channel.CapChat 常量)。enabled 过滤在 SQL 里做(两方言 enabled 均为
// 0/1 整型列,`WHERE enabled <> 0` 同义)。
func scanChannelCaps(rows *sql.Rows) ([]channelCaps, error) {
	var out []channelCaps
	for rows.Next() {
		var rawMap, rawCaps []byte
		var c channelCaps
		if err := rows.Scan(&rawMap, &rawCaps); err != nil {
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
		if len(c.Capabilities) == 0 {
			c.Capabilities = []string{string(channel.CapChat)}
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
		`SELECT public_model, unit FROM model_prices WHERE capabilities IS NULL`)
	if err != nil {
		return err
	}
	// nullModel 是一条待回填价格行:公开模型名与计价轨道(chat 归属判断用)。
	type nullModel struct {
		model string
		unit  Unit
	}
	var models []nullModel
	for rows.Next() {
		var m nullModel
		var unit string
		if err := rows.Scan(&m.model, &unit); err != nil {
			rows.Close()
			return err
		}
		m.unit = Unit(unit)
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, row := range models {
		union := map[string]bool{}
		for _, ch := range channels {
			if _, ok := ch.ModelMap[row.model]; !ok {
				continue
			}
			for _, c := range ch.Capabilities {
				union[c] = true
			}
		}
		// chat 只对 token 轨模型成立(聊天请求只打 token 轨):非聊天
		// 模型剥掉生效集默认贡献的 chat,剥完无并集即保持 NULL —— 生图
		// 等能力必须显式开启的原则不变(07 号票生效集口径)。
		if row.unit != UnitToken {
			delete(union, string(CapChat))
		}
		if len(union) == 0 {
			continue
		}
		caps := slices.Sorted(maps.Keys(union))
		if row.model == panoramaModelName {
			caps = []string{string(CapPanorama)}
		}
		encoded, err := json.Marshal(caps)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx,
			`UPDATE model_prices SET capabilities = ? WHERE public_model = ? AND capabilities IS NULL`,
			string(encoded), row.model); err != nil {
			return err
		}
	}
	return nil
}

package relay

import (
	"context"
	"encoding/json"
	"log"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/objectstore"
	"github.com/gachal/InfiniteChance/internal/settings"
)

// ImagePersist lands relay-delivered image URLs in the configured bucket
// and rewrites them to the durable public address (23 号票 relay_persist):
// 直连 /v1/images 的产物本会以厂商临时 URL(约 24h 过期)透传,调用方
// 「后面再调用」就裂图。开启后每张产物落桶 + 落 asset 行,url 列即回写
// 出去的完整地址 —— 管理端素材页可查可删可审计。
//
// 尽力而为是本组件的纪律:转存/落行失败只记日志、该张保持厂商原地址,
// 请求不失败、计费照旧(结算与转存解耦,结算在 rewriteBody 之前已定格)。
type ImagePersist struct {
	// Cloud answers the settings-selected cloud driver with the snapshot
	// that produced it — 写入目标与回写基址同源,不会写进 A 桶却回写 B
	// 的地址。*objectstore.Dynamic 实现它。
	Cloud CloudStore
	// Assets files one row per persisted image.
	Assets asset.Store
}

// CloudStore is the settings-backed seam ImagePersist needs; it is satisfied
// by *objectstore.Dynamic.
type CloudStore interface {
	Cloud(ctx context.Context) (objectstore.Store, settings.StorageConfig)
}

// maxPromptRunes caps the prompt provenance kept on relay asset rows: 生图
// 提示词偶有超长粘贴,TEXT 列 64KB 边界内留足溯源价值即可,不让一行流水
// 的体积被请求体牵着走。
const maxPromptRunes = 8192

// rewriteBody archives every data[].url item and swaps it for the durable
// public address, then answers the body to send the client. b64_json 形态
// 不动(改写 response_format 语义越界);关着转存、没有云驱动、缺
// public_base_url、响应体不可解析、或一张都没搬动时,原字节原样返回 ——
// key 顺序与空白不被无谓的重排。
func (p *ImagePersist) rewriteBody(ctx context.Context, b []byte, model, prompt string) []byte {
	if p == nil || p.Cloud == nil || p.Assets == nil {
		return b
	}
	st, cfg := p.Cloud.Cloud(ctx)
	if st == nil || !cfg.RelayPersist {
		return b
	}
	base := cfg.ActivePublicBase()
	if base == "" {
		// 读侧兜底:管理侧校验本不该让这个组合入库,手改库才会出现。
		return b
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return b
	}
	var data []map[string]json.RawMessage
	if raw, ok := fields["data"]; ok {
		if err := json.Unmarshal(raw, &data); err != nil {
			return b
		}
	}

	moved := false
	for _, item := range data {
		raw, ok := item["url"]
		if !ok {
			continue
		}
		var url string
		if err := json.Unmarshal(raw, &url); err != nil || url == "" {
			continue
		}
		public, ok := p.persistOne(ctx, st, base, url, model, prompt)
		if !ok {
			continue
		}
		encoded, err := json.Marshal(public)
		if err != nil {
			continue
		}
		item["url"] = encoded
		moved = true
	}
	if !moved {
		return b
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return b
	}
	fields["data"] = encoded
	out, err := json.Marshal(fields)
	if err != nil {
		return b
	}
	return out
}

// persistOne archives one delivered URL and files its row, answering the
// durable public address. 任何一步失败都回收已写的字节(uploads 先例,
// 不留无行的孤儿对象)并回答 false —— 调用方保留厂商原地址。
func (p *ImagePersist) persistOne(ctx context.Context, st objectstore.Store, base, url, model, prompt string) (string, bool) {
	stored, err := asset.TransferRelay(ctx, st, nil, url)
	if err != nil {
		log.Printf("relay: persist image from %s: %v (keeping vendor url)", url, err)
		return "", false
	}
	public, ok := asset.PublicAddress(asset.Asset{ObjectKey: stored.Key}, base)
	if !ok {
		return "", false
	}
	if _, err := p.Assets.Create(ctx, asset.Asset{
		Kind:        asset.KindImage,
		Model:       model,
		Prompt:      truncateRunes(prompt, maxPromptRunes),
		URL:         public, // 完整回写地址即行上的记录事实(23 号票)
		ObjectKey:   stored.Key,
		ContentType: stored.ContentType,
		SizeBytes:   stored.SizeBytes,
	}); err != nil {
		if delErr := st.Delete(context.WithoutCancel(ctx), stored.Key); delErr != nil {
			log.Printf("relay: cleanup persisted object %q after row failure: %v", stored.Key, delErr)
		}
		log.Printf("relay: persist asset row for %s: %v (keeping vendor url)", stored.Key, err)
		return "", false
	}
	return public, true
}

// truncateRunes cuts s to at most n runes; prompt 溯源不需要 64KB 全文。
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

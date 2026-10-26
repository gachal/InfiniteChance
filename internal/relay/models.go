package relay

import (
	"maps"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
)

// /v1/models(06 号票):面向调用方的模型目录 —— 全部启用渠道映射出的
// 公开模型,去重后按名称排序,OpenAI SDK 客户端据此做模型自动发现。渠道
// 维度的细节(优先级/权重/熔断)不进目录:熔断是暂态,模型很快回来,
// 不该从目录里消失。

// modelsOwnedBy marks catalog entries as this gateway's own (OpenAI uses
// "system"/"openai" for the same slot).
const modelsOwnedBy = "infinitechance"

type modelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type modelList struct {
	Object string        `json:"object"`
	Data   []modelObject `json:"data"`
}

// ListModels answers the catalog. created is when the model was first hung
// out — the earliest creation time among the enabled channels serving it.
func (h *Handlers) ListModels(c *gin.Context) {
	channels, err := h.listChannels(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}
	firstSeen := map[string]time.Time{}
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		for public := range ch.ModelMap {
			if at, ok := firstSeen[public]; !ok || ch.CreatedAt.Before(at) {
				firstSeen[public] = ch.CreatedAt
			}
		}
	}
	data := make([]modelObject, 0, len(firstSeen))
	for _, name := range slices.Sorted(maps.Keys(firstSeen)) {
		data = append(data, modelObject{
			ID:      name,
			Object:  "model",
			Created: firstSeen[name].Unix(),
			OwnedBy: modelsOwnedBy,
		})
	}
	c.JSON(http.StatusOK, modelList{Object: "list", Data: data})
}

// AudioModels answers GET /v1/audio/models (57 号票):启用渠道中带 audio
// 能力的 model_map 并集,与字符轨配价(unit=character)取交集 —— 目录只列
// 「能被某条 audio 渠道服务且已按字符配价」的合成模型,价格随行走
// PriceSummary(38 号票),画布的 /audio-models 以服务级 key 代理本端点。
// voice-enrollment(定制的调度与计费键,次轨)不进目录 —— 它不是合成模型,
// 音色创建的表单按 target_model 选型。
func (h *Handlers) AudioModels(c *gin.Context) {
	channels, err := h.listChannels(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}
	capable := map[string]bool{}
	for _, ch := range channels {
		if !ch.Enabled || !ch.HasCapability(channel.CapAudio) {
			continue
		}
		for public := range ch.ModelMap {
			capable[public] = true
		}
	}
	prices, err := h.Prices.List(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}
	models := make([]string, 0, len(prices))
	summaries := make(map[string]pricing.PriceSummary, len(prices))
	for _, p := range prices {
		if p.Unit != pricing.UnitCharacter || p.Character == nil {
			continue
		}
		if !capable[p.PublicModel] {
			continue
		}
		models = append(models, p.PublicModel)
		summaries[p.PublicModel] = p.Summary()
	}
	sort.Strings(models)
	c.JSON(http.StatusOK, gin.H{"models": models, "prices": summaries})
}

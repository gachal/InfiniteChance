package relay

import (
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
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
	channels, err := h.Channels.List(c.Request.Context())
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

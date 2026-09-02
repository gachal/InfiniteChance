package pricing

import (
	"errors"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/apikey"
)

// Handlers serves the admin model-price endpoints. The gateway mounts them
// under /admin behind the JWT session middleware. Amounts cross the wire as
// human units (USD per million tokens, ratio ×1.0) and are converted to
// integer micros here, like the key quota API.
type Handlers struct {
	Store Store
}

// RegisterAdminRoutes mounts:
//
//	GET    /admin/prices        — list all model prices
//	PUT    /admin/prices        — upsert one row (keyed by public_model)
//	DELETE /admin/prices?model= — remove one row; query param because model
//	                              names may contain slashes
func RegisterAdminRoutes(group *gin.RouterGroup, h *Handlers) {
	group.GET("/prices", h.List)
	group.PUT("/prices", h.Upsert)
	group.DELETE("/prices", h.Delete)
}

// priceJSON is the wire form: human units at the edge.
type priceJSON struct {
	PublicModel         string    `json:"public_model"`
	Unit                Unit      `json:"unit"`
	InputUSDPerMTokens  float64   `json:"input_usd_per_mtokens"`
	OutputUSDPerMTokens float64   `json:"output_usd_per_mtokens"`
	Ratio               float64   `json:"ratio"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func toPriceJSON(p Price) priceJSON {
	body := priceJSON{
		PublicModel: p.PublicModel,
		Unit:        p.Unit,
		Ratio:       1,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
	if p.Token != nil {
		body.InputUSDPerMTokens = MicrosToUSDPerMTokens(p.Token.InputMicrosPerMTokens)
		body.OutputUSDPerMTokens = MicrosToUSDPerMTokens(p.Token.OutputMicrosPerMTokens)
		body.Ratio = MicrosToRatio(p.Token.RatioMicros)
	}
	return body
}

// usdPerMTokensToMicros converts a human USD-per-million-tokens price to
// micros, rounding to the nearest micro so float artifacts at the API edge
// never accumulate.
func usdPerMTokensToMicros(usd float64) int64 {
	return int64(math.Round(usd * apikey.MicrosPerUSD))
}

// MicrosToUSDPerMTokens converts back for the admin API.
func MicrosToUSDPerMTokens(micros int64) float64 {
	return float64(micros) / apikey.MicrosPerUSD
}

// ratioToMicros converts a ×1.0-based multiplier to ratio micros.
func ratioToMicros(ratio float64) int64 {
	return int64(math.Round(ratio * ratioDenom))
}

// MicrosToRatio converts back for the admin API.
func MicrosToRatio(micros int64) float64 {
	return float64(micros) / ratioDenom
}

type priceInputJSON struct {
	PublicModel         string   `json:"public_model"`
	Unit                Unit     `json:"unit"`
	InputUSDPerMTokens  float64  `json:"input_usd_per_mtokens"`
	OutputUSDPerMTokens float64  `json:"output_usd_per_mtokens"`
	Ratio               *float64 `json:"ratio"`
}

type listResponse struct {
	Prices []priceJSON `json:"prices"`
}

func (h *Handlers) List(c *gin.Context) {
	prices, err := h.Store.List(c.Request.Context())
	if err != nil {
		h.failInternal(c, err)
		return
	}
	bodies := make([]priceJSON, 0, len(prices))
	for _, p := range prices {
		bodies = append(bodies, toPriceJSON(p))
	}
	c.JSON(http.StatusOK, listResponse{Prices: bodies})
}

func (h *Handlers) Upsert(c *gin.Context) {
	var raw priceInputJSON
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的模型价格 JSON")
		return
	}
	ratio := 1.0
	if raw.Ratio != nil {
		ratio = *raw.Ratio
	}
	p := Price{
		PublicModel: raw.PublicModel,
		Unit:        raw.Unit,
		Token: &TokenPrice{
			InputMicrosPerMTokens:  usdPerMTokensToMicros(raw.InputUSDPerMTokens),
			OutputMicrosPerMTokens: usdPerMTokensToMicros(raw.OutputUSDPerMTokens),
			RatioMicros:            ratioToMicros(ratio),
		},
	}
	p, err := p.Normalize()
	if err != nil {
		apierr.InvalidRequest(c, err.Error())
		return
	}

	stored, err := h.Store.Upsert(c.Request.Context(), p)
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, toPriceJSON(stored))
}

func (h *Handlers) Delete(c *gin.Context) {
	model := c.Query("model")
	if model == "" {
		apierr.InvalidRequest(c, "缺少 model 查询参数")
		return
	}
	err := h.Store.Delete(c.Request.Context(), model)
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "该模型没有价格记录")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) failInternal(c *gin.Context, err error) {
	log.Printf("pricing: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.Internal(c, "服务内部错误,请稍后再试")
}

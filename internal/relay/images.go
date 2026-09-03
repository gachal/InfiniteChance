package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
)

// 生图同步转发(07 号票):/v1/images/generations(JSON 体)与
// /v1/images/edits(multipart 表单)共享同一条「校验 → 调度 → 计价 →
// 预扣 → 逐渠道尝试 → 按实结算或退款」的骨架,与聊天轨只有两处不同:
// 候选仅限声明 images 能力的渠道,预扣/结算按「单价 × 尺寸系数 × 张数」
// 计算(结算以响应 data 实交张数为准,多退少补)。失败语义 —— 临时失败
// 换道、预扣原封带往下一渠道、终局全额退款 —— 与缓冲聊天完全一致。

// defaultImageCount is the vendor-side n default (一张). The n/size upper
// bounds are the pricing package's billing shields (pricing.MaxCallItems /
// pricing.MaxSizeRunes) — one owner for the numbers the charge depends on.
const defaultImageCount = int64(1)

// imagesRequest is the slice of an images body the gateway itself needs.
// Everything else rides through untouched.
type imagesRequest struct {
	Model string `json:"model"`
	N     *int64 `json:"n"`
	Size  string `json:"size"`
}

// ImagesGenerations relays POST /v1/images/generations (JSON body).
func (h *Handlers) ImagesGenerations(c *gin.Context) {
	h.images(c, h.prepareImagesGeneration)
}

// ImagesEdits relays POST /v1/images/edits (multipart form).
func (h *Handlers) ImagesEdits(c *gin.Context) {
	h.images(c, h.prepareImagesEdit)
}

// prepareImagesGeneration reads and validates the JSON body, then defers to
// the shared images prepare.
func (h *Handlers) prepareImagesGeneration(c *gin.Context, key apikey.Key) *prepared {
	raw, err := c.GetRawData()
	if err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body could not be read.")
		return nil
	}
	var req imagesRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return nil
	}
	return h.prepareImages(c, key, req, nil, raw)
}

// prepareImagesEdit parses the multipart form (OpenAI edits take the image
// file(s) plus the usual text fields), extracts the billing-relevant fields
// and defers to the shared prepare. The parsed form rides on prepared so
// each channel attempt can rebuild it with its own upstream model name.
func (h *Handlers) prepareImagesEdit(c *gin.Context, key apikey.Key) *prepared {
	form, err := c.MultipartForm()
	if err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			"The request must be multipart/form-data with an 'image' file part.")
		return nil
	}
	req := imagesRequest{Model: formValue(form, "model"), Size: formValue(form, "size")}
	if v := formValue(form, "n"); v != "" {
		n, perr := strconv.ParseInt(v, 10, 64)
		if perr != nil {
			apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
				"'n' must be an integer between 1 and 100.")
			return nil
		}
		req.N = &n
	}
	return h.prepareImages(c, key, req, form, nil)
}

// prepareImages is the shared prefix of both images endpoints: validate,
// schedule image-capable candidates, require a call-track price and take the
// per-call pre-deduction (单价 × 尺寸系数 × n). raw carries the generations
// JSON body (form 非 nil 时为 nil);On any rejection the client has already
// been answered an OpenAI error object and the result is nil.
func (h *Handlers) prepareImages(c *gin.Context, key apikey.Key, req imagesRequest, form *multipart.Form, raw []byte) *prepared {
	ctx := c.Request.Context()

	if req.Model == "" {
		apierr.OpenAI(c, http.StatusBadRequest, CodeMissingModel, TypeInvalidRequestError, "You must provide a 'model' parameter.")
		return nil
	}
	n := defaultImageCount
	if req.N != nil {
		if *req.N < 1 || *req.N > pricing.MaxCallItems {
			apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
				"'n' must be an integer between 1 and "+strconv.FormatInt(pricing.MaxCallItems, 10)+".")
			return nil
		}
		n = *req.N
	}
	size := strings.TrimSpace(req.Size)
	if len([]rune(size)) > pricing.MaxSizeRunes {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			"The 'size' parameter is too long.")
		return nil
	}

	channels, err := h.Channels.List(ctx)
	if err != nil {
		h.failInternal(c, err)
		return nil
	}
	candidates := weightedOrder(eligibleChannels(channels, req.Model, channel.CapImages), h.randIntn)
	if len(candidates) == 0 {
		apierr.OpenAI(c, http.StatusNotFound, CodeModelNotFound, TypeInvalidRequestError,
			"The model '"+req.Model+"' does not exist or no image-capable channel serves it.")
		return nil
	}
	price, err := h.Prices.ByModel(ctx, req.Model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' has no price configured; ask the administrator to add one.")
		return nil
	}
	if err != nil {
		h.failInternal(c, err)
		return nil
	}
	if price.Unit != pricing.UnitCall || price.Call == nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' is not priced for per-call billing.")
		return nil
	}

	// generations 在预扣前做一次重写证明(与聊天轨同):body 必须是可
	// 重写的 JSON 对象;edits 已是解析过的表单,无此步骤。
	if form == nil {
		if _, err := rewriteModel(raw, candidates[0].ModelMap[req.Model]); err != nil {
			apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
			return nil
		}
	}

	// 按次预扣:单价 × 尺寸系数 × 请求张数。免费模型(单价 0)估算为 0,
	// 跳过预扣与后续账务。
	reserved := price.Call.ChargeMicros(size, n)
	if reserved > 0 {
		if _, err := h.Keys.Reserve(ctx, key.ID, reserved, apikey.ReasonEstimate); err != nil {
			h.reserveFailed(c, key, err)
			return nil
		}
	}

	// 审计快照记下价格与请求事实(size、n):按次扣费缺了请求事实无法重算。
	snapshot, err := price.CallSnapshot(size, n)
	if err != nil {
		// call 轨守卫保证了单位,这里只可能是编码失败;快照留空要留痕。
		log.Printf("relay: price snapshot for %s: %v", price.PublicModel, err)
		snapshot = nil
	}

	return &prepared{
		publicModel: req.Model,
		raw:         raw,
		call:        &imagesPrep{size: size, n: n, form: form},
		key:         key, price: price,
		reserved: reserved, snapshot: snapshot,
		candidates: candidates,
		source:     SourceFrom(c),
	}
}

// images runs the shared images failover loop after prepare: candidates in
// scheduling order, breaker-gated, with the chat track's failure semantics —
// a temporary failure moves on while the reserve stays put; the deciding
// attempt bills or refunds. Success settles per delivered image.
func (h *Handlers) images(c *gin.Context, prepare func(*gin.Context, apikey.Key) *prepared) {
	key, _ := apikey.KeyFrom(c)

	p := prepare(c, key)
	if p == nil {
		return
	}

	ctx := c.Request.Context()
	// 账务与留痕使用脱离请求的 context:客户端在转发中途断开时,请求
	// context 随之取消,但已预扣的钱必须退、失败 trail 必须落库。
	billing := context.WithoutCancel(ctx)
	p.started = time.Now()
	run := &failoverRunner{h: h, c: c, billing: billing, p: p, breaker: h.Breaker}

	// 拨号按请求形态分流:edits 带重建表单的 multipart 体,generations 带
	// model 已重写的 JSON 体。
	dial := func(at attempt) (*UpstreamResponse, error) {
		if p.call.form != nil {
			return h.adaptor().ImagesEdits(ctx, at.ch, at.contentType, at.payload)
		}
		return h.adaptor().ImagesGenerations(ctx, at.ch, at.payload)
	}

	for i, cand := range p.candidates {
		if !run.breaker.TryAcquire(cand.ID, time.Now()) {
			continue // 熔断中的渠道不占用尝试
		}
		at, err := p.attemptFor(cand)
		if err != nil {
			run.abortInternal(cand.ID, err)
			return
		}

		upstream, err := dial(at)
		if err == nil && upstream.OK {
			clientBody, delivered, nerr := h.adaptor().NormalizeImages(p.publicModel, upstream.Body)
			// 命中且实交了图:按实交张数结算,响应体回写公开名后透传。
			if nerr == nil && delivered > 0 {
				run.breaker.RecordSuccess(at.ch.ID)
				run.settleImages(at, delivered, time.Since(p.started).Milliseconds())
				c.Data(http.StatusOK, "application/json; charset=utf-8", clientBody)
				return
			}
			// 2xx 但一张图都没交付(或响应体不可用):与上游失败同路 ——
			// 可换道重试。
			f := p.failure()
			if nerr != nil {
				f.normalizeErr = nerr
			} else {
				f.normalizeErr = errors.New("upstream delivered no images")
			}
			if !run.failed(at, f, i < len(p.candidates)-1) {
				return
			}
			continue
		}

		f := p.failure()
		if err != nil {
			f.transportErr = err
		} else {
			f.upstream = upstream
		}
		if !run.failed(at, f, i < len(p.candidates)-1) {
			return
		}
	}
	run.exhausted()
}

// formValue returns the first value of a multipart text field.
func formValue(form *multipart.Form, field string) string {
	if values := form.Value[field]; len(values) > 0 {
		return values[0]
	}
	return ""
}

// rebuildMultipart re-serializes the client's multipart form with the model
// text field replaced by the channel's upstream name. File parts are copied
// byte-for-byte with their original headers (文件名与 Content-Type 都在原
// header 里),so the vendor sees exactly what the client uploaded. Returns
// the body plus the Content-Type carrying the fresh boundary.
func rebuildMultipart(form *multipart.Form, upstreamModel string) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for field, values := range form.Value {
		for _, v := range values {
			if field == "model" {
				v = upstreamModel
			}
			if err := w.WriteField(field, v); err != nil {
				return nil, "", err
			}
		}
	}
	for _, files := range form.File {
		for _, fh := range files {
			// 分节名与文件名都在原 Content-Disposition 里,原样复刻即可。
			part, err := w.CreatePart(fh.Header)
			if err != nil {
				return nil, "", err
			}
			f, err := fh.Open()
			if err != nil {
				return nil, "", err
			}
			_, err = io.Copy(part, f)
			f.Close()
			if err != nil {
				return nil, "", err
			}
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

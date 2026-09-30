package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/usage"
	"github.com/gachal/InfiniteChance/internal/videotask"
)

// 视频异步任务(08 号票):自定义 OpenAI 风格契约 —— POST
// /v1/videos/generations 提交返回 task_id,GET /v1/videos/tasks/{id} 轮询,
// POST /v1/videos/tasks/{id}/cancel 取消。对外状态收敛五态
// (queued/running/succeeded/failed/canceled),归并规则在 videotask 包:
// 上游节流态归并 queued、未知态归并 failed。计费「仅成功计费」:提交时按
// 「每秒单价 × 分辨率系数 × 秒数」预扣,渠道在厂商受理那一刻钉死;轮询把
// 任务推进到终态时一次结清 —— 成功定格为实扣(差额为零不动流水),失败/
// 取消全额退款,终态迁移落一条用量日志(取消归并 upstream_error,摘要列
// 区分,与聊天/生图留痕语义一致)。轮询查询本身的暂时失败不改状态不动账。

// defaultVideoSeconds matches the vendors' common default clip length
// (Kling/万相/Veo 都以 5s 档起步);上限走计价护盾 MaxCallItems。
const defaultVideoSeconds = int64(5)

// maxImageURLRunes bounds the optional image2video reference URL.
const maxImageURLRunes = 4096

// videoRequest is the slice of the submit body the gateway itself needs;
// everything else rides through to the vendor untouched(JSON 全量透传,
// 仅 model 换名)。image 是图生视频的可选首帧;last_image / references /
// ratio 是 25 号票的契约扩展:尾帧、多模态参考(图/视频/音频,≤9 条)与
// 显式宽高比 —— openai 形渠道对新参数全量透传不剥离,ark 形渠道在
// adaptor 内翻译。
type videoRequest struct {
	Model      string           `json:"model"`
	Prompt     string           `json:"prompt"`
	Seconds    *int64           `json:"seconds"`
	Size       string           `json:"size"`
	Ratio      string           `json:"ratio"`
	Image      string           `json:"image"`
	LastImage  string           `json:"last_image"`
	References []videoReference `json:"references"`
}

// 视频参考的 kind 枚举(网关自己的契约,不是上游枚举):上游档位交上游裁,
// 这三种是网关愿意翻译的媒体类型。
const (
	refKindImage = "image"
	refKindVideo = "video"
	refKindAudio = "audio"
)

// maxVideoReferences bounds the references array (对齐生图参考配额,25 号票
// 定案 ≤9 条);超出在计费与拨号之前拒绝,比厂商的不可读错误便宜。
const maxVideoReferences = 9

// videoReference is one entry of the references array: a media URL plus its
// kind. http(s) only — inline data: URIs are the shape 12 号票 already banned
// from gateway media contracts (校验在 prepareVideo)。
type videoReference struct {
	URL  string `json:"url"`
	Kind string `json:"kind"`
}

// checkVideoMediaURL enforces the gateway media contract on one optional
// media reference: http(s) URL the vendor can fetch (内联 data: 400)plus
// the shared length bound. Empty passes — the field is optional.
func checkVideoMediaURL(field, raw string) error {
	u := strings.TrimSpace(raw)
	if u == "" {
		return nil
	}
	if len([]rune(u)) > maxImageURLRunes {
		return fmt.Errorf("The '%s' parameter is too long.", field)
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("The '%s' parameter must be an http(s) URL the vendor can fetch (inline data: URIs are not supported).", field)
	}
	return nil
}

// validateVideoMedia checks the three media fields of the extended contract
// (image 逐字节兼容既有请求,校验口径与 last_image/references 统一收紧到
// http(s))。nil = 全部合规;错误消息是英文(中转面错误文案是英文)。
func validateVideoMedia(req videoRequest) error {
	for _, f := range []struct{ field, url string }{
		{"image", req.Image},
		{"last_image", req.LastImage},
	} {
		if err := checkVideoMediaURL(f.field, f.url); err != nil {
			return err
		}
	}
	if len(req.References) > maxVideoReferences {
		return fmt.Errorf("'references' supports at most %d entries, got %d.", maxVideoReferences, len(req.References))
	}
	for i, r := range req.References {
		switch strings.TrimSpace(r.Kind) {
		case refKindImage, refKindVideo, refKindAudio:
		default:
			return fmt.Errorf("references[%d].kind must be one of image, video, audio.", i)
		}
		if err := checkVideoMediaURL(fmt.Sprintf("references[%d].url", i), r.URL); err != nil {
			return err
		}
	}
	return nil
}

// videoTaskJSON is the external task object: submit answers it fresh from
// the store, poll and cancel answer the stored row.
type videoTaskJSON struct {
	TaskID    string              `json:"task_id"`
	Status    videotask.Status    `json:"status"`
	Model     string              `json:"model"`
	CreatedAt int64               `json:"created_at"`
	Seconds   int64               `json:"seconds"`
	Size      string              `json:"size,omitempty"`
	VideoURL  string              `json:"video_url,omitempty"`
	Error     *videoTaskErrorJSON `json:"error,omitempty"`
}

type videoTaskErrorJSON struct {
	Message string `json:"message"`
}

func videoTaskBody(t videotask.Task) videoTaskJSON {
	body := videoTaskJSON{
		TaskID:    t.ID,
		Status:    t.Status,
		Model:     t.PublicModel,
		CreatedAt: t.CreatedAt.Unix(),
		Seconds:   t.Seconds,
		Size:      t.Size,
		VideoURL:  t.VideoURL,
	}
	if t.Status == videotask.StatusFailed && t.Error != "" {
		body.Error = &videoTaskErrorJSON{Message: t.Error}
	}
	return body
}

// CreateVideoGeneration relays POST /v1/videos/generations: validate →
// schedule video-capable candidates → price(second 轨)→ reserve → try
// channels in order → the first vendor acceptance pins the channel, stores
// the task queued and hands back its task_id. A rejected submit is an
// ordinary failover step(预扣原封带往下一渠道);候选用尽按同步轨收尾。
func (h *Handlers) CreateVideoGeneration(c *gin.Context) {
	key, _ := apikey.KeyFrom(c)

	p := h.prepareVideo(c, key)
	if p == nil {
		return
	}

	ctx := c.Request.Context()
	// 账务与留痕使用脱离请求的 context:提交中途客户端断开,已预扣的
	// 钱也必须退干净(与聊天/生图轨同一纪律)。
	billing := context.WithoutCancel(ctx)
	p.started = time.Now()
	run := &failoverRunner{h: h, c: c, billing: billing, p: p, breaker: h.Breaker}

	for i, cand := range p.candidates {
		if !run.breaker.TryAcquire(cand.ID, time.Now()) {
			continue // 熔断中的渠道不占用尝试
		}
		at, err := p.attemptFor(cand)
		if err != nil {
			run.abortInternal(cand.ID, err)
			return
		}

		upstream, err := h.adaptorFor(at.ch).VideosSubmit(ctx, at.ch, at.payload)
		if err == nil && upstream.OK {
			vendorID, perr := parseVideoSubmit(upstream.Body)
			if perr == nil {
				run.breaker.RecordSuccess(at.ch.ID)
				h.videoSubmitted(c, billing, p, at, vendorID, run.retried)
				return
			}
			// 2xx 但拿不到 task_id:与上游失败同路 —— 可换道重试。
			f := p.failure()
			f.normalizeErr = perr
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

// prepareVideo is the submit prefix: validate, schedule video-capable
// candidates, require a second-track price and take the pre-deduction
// (单价 × 分辨率系数 × 秒数). On any rejection the client has already been
// answered an OpenAI error object and the result is nil.
func (h *Handlers) prepareVideo(c *gin.Context, key apikey.Key) *prepared {
	ctx := c.Request.Context()

	raw, err := c.GetRawData()
	if err != nil {
		refuseBody(c, err)
		return nil
	}
	var req videoRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return nil
	}
	if req.Model == "" {
		apierr.OpenAI(c, http.StatusBadRequest, CodeMissingModel, TypeInvalidRequestError, "You must provide a 'model' parameter.")
		return nil
	}
	if strings.TrimSpace(req.Prompt) == "" {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "You must provide a 'prompt' parameter.")
		return nil
	}
	seconds := defaultVideoSeconds
	if req.Seconds != nil {
		if *req.Seconds < 1 || *req.Seconds > pricing.MaxCallItems {
			apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
				"'seconds' must be an integer between 1 and "+strconv.FormatInt(pricing.MaxCallItems, 10)+".")
			return nil
		}
		seconds = *req.Seconds
	}
	size := strings.TrimSpace(req.Size)
	if len([]rune(size)) > pricing.MaxSizeRunes {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			"The 'size' parameter is too long.")
		return nil
	}
	if len([]rune(strings.TrimSpace(req.Ratio))) > pricing.MaxSizeRunes {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			"The 'ratio' parameter is too long.")
		return nil
	}
	if err := validateVideoMedia(req); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, err.Error())
		return nil
	}

	channels, err := h.listChannels(ctx)
	if err != nil {
		h.failInternal(c, err)
		return nil
	}
	candidates := weightedOrder(eligibleChannels(channels, req.Model, channel.CapVideos), h.randIntn)
	if len(candidates) == 0 {
		apierr.OpenAI(c, http.StatusNotFound, CodeModelNotFound, TypeInvalidRequestError,
			"The model '"+req.Model+"' does not exist or no video-capable channel serves it.")
		return nil
	}
	price, err := h.priceFor(ctx, req.Model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' has no price configured; ask the administrator to add one.")
		return nil
	}
	if err != nil {
		h.failInternal(c, err)
		return nil
	}
	// 计价校验(25 号票放宽):视频按模型所配轨道结算 —— second 轨照旧
	// 「单价 × 系数 × 秒」预扣,token 轨按估算表折算预扣(实报 tokens
	// 终态多退少补);其余轨道仍 model_not_priced。
	switch price.Unit {
	case pricing.UnitSecond:
		if price.Call == nil {
			apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
				"The model '"+req.Model+"' is not priced for per-second video billing.")
			return nil
		}
	case pricing.UnitToken:
		if price.Token == nil {
			apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
				"The model '"+req.Model+"' is not priced for token billing.")
			return nil
		}
	default:
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' is not priced for video billing.")
		return nil
	}

	// 提交前先证明 body 可重写(与聊天/生图同);逐渠道的真正重写在
	// attemptFor 里做(各渠道上游名不同)。
	if _, err := rewriteModel(raw, candidates[0].ModelMap[req.Model]); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return nil
	}

	// 预扣。second 轨:单价 × 分辨率系数 × 秒。token 轨:输出单价 × 估算
	// tokens(估算表按 size 档折算,缺省标量兜底;终态按厂商实报多退少
	// 补)。免费模型估算为 0,跳过账务。
	var reserved int64
	var snapshot []byte
	if price.Unit == pricing.UnitToken {
		estTokens := price.Token.EstimateVideoTokens(size, seconds)
		reserved = price.Token.ChargeMicros(0, estTokens)
		snapshot, err = videoTokenSnapshot(price, size, seconds, estTokens)
	} else {
		reserved = price.Call.ChargeMicros(size, seconds)
		snapshot, err = price.CallSnapshot(size, seconds)
	}
	if err != nil {
		// 轨道守卫已保证单位,这里只可能是编码失败;快照留空要留痕。
		log.Printf("relay: price snapshot for %s: %v", price.PublicModel, err)
		snapshot = nil
	}
	if reserved > 0 {
		if _, err := h.Keys.Reserve(ctx, key.ID, reserved, apikey.ReasonEstimate); err != nil {
			h.reserveFailed(c, key, err)
			return nil
		}
	}

	// 审计快照记下价格与请求事实(second 轨:size、seconds;token 轨:
	// size、seconds、估算 tokens):扣费缺请求事实无法重算。

	return &prepared{
		publicModel: req.Model,
		raw:         raw,
		video:       &videoPrep{size: size, seconds: seconds},
		key:         key, price: price,
		reserved: reserved, snapshot: snapshot,
		candidates: candidates,
		source:     SourceFrom(c),
	}
}

// videoSubmitted pins the accepting channel: the task row is created queued
// with the billing facts from submit time and the client gets the public
// task object. retried is the failover history of the submits abandoned
// before this one was accepted — it stays on the task row so the eventual
// trail row can carry it(带病提交与带病完成同一留痕语义). A failed insert
// means the reserve would leak — refund and answer 500.
func (h *Handlers) videoSubmitted(c *gin.Context, billing context.Context, p *prepared, at attempt, vendorTaskID string, retried []string) {
	id, err := videotask.NewID()
	var task videotask.Task
	if err == nil {
		// 建行走脱离请求的 context:厂商已受理,客户端断开也不能让任务
		// 变成查不到的孤儿。
		task, err = h.Tasks.Create(billing, videotask.Task{
			ID: id, KeyID: p.key.ID, ChannelID: at.ch.ID, ChannelName: at.ch.Name,
			PublicModel: p.publicModel, UpstreamModel: at.upstreamModel,
			UpstreamTaskID: vendorTaskID, Status: videotask.StatusQueued,
			Size: p.video.size, Seconds: p.video.seconds,
			ReservedMicros: p.reserved, PriceSnapshot: p.snapshot,
			Error: upstreamErrorSummary(retried, ""),
		})
	}
	if err != nil {
		h.adjustBalance(billing, p.key.ID, p.reserved, apikey.ReasonRefund)
		log.Printf("relay: store video task for key %d: %v", p.key.ID, err)
		apierr.OpenAI(c, http.StatusInternalServerError, "internal_error", TypeServerError, "The gateway hit an internal error.")
		return
	}
	c.JSON(http.StatusOK, videoTaskBody(task))
}

// videoErrorWithHistory keeps the task row's error column cumulative:
// submit-time failover first, then the deciding summary —— 与同步轨的
// upstream_error 摘要列同一形状,任务行自己攒这段历史。
func videoErrorWithHistory(task videotask.Task, summary string) string {
	if task.Error == "" {
		return summary
	}
	return task.Error + "; " + summary
}

// GetVideoTask relays GET /v1/videos/tasks/{id} — the poll. Terminal tasks
// answer from the store without touching the vendor(产物 URL 本就临时,
// 网关的账本事实才是契约);active tasks query the pinned channel, merge
// the vendor's raw status into the external machine, and close the books
// exactly once on the transition to a terminal state — the store's guarded
// update decides the winner when polls race a cancel.
func (h *Handlers) GetVideoTask(c *gin.Context) {
	key, _ := apikey.KeyFrom(c)
	task, ok := h.ownedVideoTask(c, key, c.Param("id"))
	if !ok {
		return
	}
	if videotask.Terminal(task.Status) {
		c.JSON(http.StatusOK, videoTaskBody(task))
		return
	}

	ctx := c.Request.Context()
	ch, err := h.Channels.Get(ctx, task.ChannelID)
	if errors.Is(err, channel.ErrNotFound) {
		// 渠道被删:没人能代理这个任务了。任务原地不动(可取消退款),
		// 对客户端如实说明。
		apierr.OpenAI(c, http.StatusBadGateway, CodeUpstreamError, CodeUpstreamError,
			"The channel serving this task no longer exists; it can no longer be polled. Cancel the task to release the quota hold.")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}

	upstream, err := h.adaptorFor(ch).VideosQuery(ctx, ch, task.UpstreamTaskID)
	if err != nil || !upstream.OK {
		// 一次轮询失败不是任务失败:任务原地不动、账不动,稍后再试。
		status, summary := http.StatusBadGateway, "(transport error)"
		if err == nil {
			status = upstream.Status
			summary = h.adaptorFor(ch).ErrorSummary(upstream.Body)
		}
		apierr.OpenAI(c, status, CodeUpstreamError, CodeUpstreamError,
			"Upstream task query failed: "+summary)
		return
	}
	rawStatus, videoURL, vendorErr, reportedTokens, perr := parseVideoQuery(upstream.Body)
	if perr != nil {
		// 响应体不可解析同样是暂态上游故障,不能据此推进状态。
		apierr.OpenAI(c, http.StatusBadGateway, CodeUpstreamError, CodeUpstreamError,
			"Upstream task query failed: "+perr.Error())
		return
	}

	billing := context.WithoutCancel(ctx)
	merged := videotask.MergeStatus(rawStatus)
	switch merged {
	case videotask.StatusQueued, videotask.StatusRunning:
		// 推进或刷新:状态只进不退(running 不会被打回 queued),原始
		// 状态串恒记最新一次。守卫 expect=当前态,竞态输了以库里现行为准。
		// 状态迁移走脱离请求的 context:它和账务一体,客户端在决定性的
		// 一次轮询中途断开也不能把迁移弄丢。
		status := merged
		if task.Status == videotask.StatusRunning && merged == videotask.StatusQueued {
			status = videotask.StatusRunning
		}
		if status != task.Status || rawStatus != task.UpstreamStatus {
			fresh, _, err := h.Tasks.Update(billing, task.ID, []videotask.Status{task.Status}, videotask.Patch{
				Status: status, UpstreamStatus: &rawStatus,
			})
			if err != nil {
				h.failInternal(c, err)
				return
			}
			task = fresh
		}
		c.JSON(http.StatusOK, videoTaskBody(task))
	case videotask.StatusSucceeded:
		if videoURL == "" {
			// 成功却没产物:对外不能算成功 —— 仅成功计费的前提是拿得到
			// 产物,与「2xx 但一张图都没交付」同一判法。
			h.failVideoTask(c, billing, task, videotask.Patch{
				Status: videotask.StatusFailed, UpstreamStatus: &rawStatus,
				ErrMsg: strPtr(videoErrorWithHistory(task,
					"upstream reported "+strconv.Quote(rawStatus)+" without a video URL")),
			})
			return
		}
		// 终态结算按任务所配轨道分叉(25 号票):second 轨定格实扣(实扣
		// 即预扣,差额恒零);token 轨按厂商实报 completion_tokens 多退少
		// 补,厂商未报 usage 时回退按预扣估算定格实结并留痕(任务已跑完
		// 不退款,也不虚记)。
		charge := task.ReservedMicros
		var note string
		if unit, token := videoBillingTrack(task); unit == pricing.UnitToken {
			if reportedTokens > 0 && token != nil {
				charge = token.ChargeMicros(0, reportedTokens)
			} else {
				note = "usage missing, billed estimate"
			}
		}
		fresh, won, err := h.Tasks.Update(billing, task.ID, videotask.ActiveStatuses, videotask.Patch{
			Status: videotask.StatusSucceeded, UpstreamStatus: &rawStatus,
			VideoURL: &videoURL, ChargeMicros: &charge,
		})
		if err != nil {
			h.failInternal(c, err)
			return
		}
		if won {
			// 多退少补:差额为零不动账(不落 settle 流水),落成功留痕。
			h.adjustBalance(billing, task.KeyID, task.ReservedMicros-charge, apikey.ReasonSettle)
			h.recordUsage(billing, h.videoUsageEntry(fresh, usage.StatusSuccess, charge, reportedTokens, note))
		}
		c.JSON(http.StatusOK, videoTaskBody(fresh))
	case videotask.StatusCanceled:
		summary := "canceled upstream"
		if vendorErr != "" {
			summary = "canceled upstream: " + vendorErr
		}
		h.failVideoTask(c, billing, task, videotask.Patch{
			Status: videotask.StatusCanceled, UpstreamStatus: &rawStatus,
			ErrMsg: strPtr(videoErrorWithHistory(task, summary)),
		})
	default:
		// failed(含未知态归并):摘要优先厂商给的失败原因,缺了就报
		// 原始状态串。
		summary := vendorErr
		if summary == "" {
			summary = "upstream reported task status " + strconv.Quote(rawStatus)
		}
		h.failVideoTask(c, billing, task, videotask.Patch{
			Status: videotask.StatusFailed, UpstreamStatus: &rawStatus,
			ErrMsg: strPtr(videoErrorWithHistory(task, summary)),
		})
	}
}

// CancelVideoTask relays POST /v1/videos/tasks/{id}/cancel. In-flight tasks
// stop at the vendor(尽力而为)and close as canceled with the whole
// reserve refunded —— 取消不扣费;渠道已删也照常本地取消。已经终态的任务
// 原样回答:取消不改写历史,succeeded 仍保持已计费。
func (h *Handlers) CancelVideoTask(c *gin.Context) {
	key, _ := apikey.KeyFrom(c)
	task, ok := h.ownedVideoTask(c, key, c.Param("id"))
	if !ok {
		return
	}
	if videotask.Terminal(task.Status) {
		c.JSON(http.StatusOK, videoTaskBody(task))
		return
	}

	ctx := c.Request.Context()
	if ch, err := h.Channels.Get(ctx, task.ChannelID); err == nil {
		if upstream, uerr := h.adaptorFor(ch).VideosCancel(ctx, ch, task.UpstreamTaskID); uerr != nil || !upstream.OK {
			// 厂商侧取消失败不拦本地取消:对外「已取消且不扣费」由网关
			// 兑付,厂商那边是否真停下只进日志。
			reason := "(transport error)"
			if uerr == nil {
				reason = h.adaptorFor(ch).ErrorSummary(upstream.Body)
			}
			log.Printf("relay: upstream cancel for task %s (vendor %s) failed: %s",
				task.ID, task.UpstreamTaskID, reason)
		}
	} else if !errors.Is(err, channel.ErrNotFound) {
		h.failInternal(c, err)
		return
	}

	billing := context.WithoutCancel(ctx)
	summary := videoErrorWithHistory(task, "canceled by client")
	fresh, won, err := h.Tasks.Update(billing, task.ID, videotask.ActiveStatuses, videotask.Patch{
		Status: videotask.StatusCanceled,
		ErrMsg: strPtr(summary),
	})
	if err != nil {
		h.failInternal(c, err)
		return
	}
	if won {
		h.adjustBalance(billing, task.KeyID, task.ReservedMicros, apikey.ReasonRefund)
		h.recordUsage(billing, h.videoUsageEntry(fresh, usage.StatusUpstreamError, 0, 0, ""))
	}
	c.JSON(http.StatusOK, videoTaskBody(fresh))
}

// failVideoTask drives an active task to failed or upstream-canceled on the
// poll path: the guarded update is the single writer, and the winner refunds
// the whole reserve and leaves the upstream_error trail. Update rides the
// detached billing context — the transition and the books are one unit. The
// client gets the fresh row either way(输了竞态就答现状).
func (h *Handlers) failVideoTask(c *gin.Context, billing context.Context, task videotask.Task, p videotask.Patch) {
	fresh, won, err := h.Tasks.Update(billing, task.ID, videotask.ActiveStatuses, p)
	if err != nil {
		h.failInternal(c, err)
		return
	}
	if won {
		h.adjustBalance(billing, task.KeyID, task.ReservedMicros, apikey.ReasonRefund)
		h.recordUsage(billing, h.videoUsageEntry(fresh, usage.StatusUpstreamError, 0, 0, ""))
	}
	c.JSON(http.StatusOK, videoTaskBody(fresh))
}

// videoUsageEntry builds the trail row for a task's terminal transition:
// the pinned channel facts live on the task row, the lifetime is wall-clock
// since submit, and the unit follows the track the task settled on (the
// task's own snapshot decides — 25 号票起视频也可能是 token 轨:行上
// completion_tokens 记视频 token 数,快照带上实报与留痕). The row's
// upstream_error column carries the task's accumulated history — submit-time
// failover first, then the closing summary.
func (h *Handlers) videoUsageEntry(task videotask.Task, status string, charge, reportedTokens int64, note string) usage.Log {
	unit, _ := videoBillingTrack(task)
	entry := usage.Log{
		KeyID: task.KeyID, ChannelID: task.ChannelID, ChannelName: task.ChannelName,
		PublicModel: task.PublicModel, UpstreamModel: task.UpstreamModel,
		Unit:       string(unit),
		DurationMS: time.Since(task.CreatedAt).Milliseconds(),
		Status:     status, ChargeMicros: charge, PriceSnapshot: task.PriceSnapshot,
		UpstreamError: task.Error,
	}
	if unit == pricing.UnitToken {
		entry.CompletionTokens = reportedTokens
		if status == usage.StatusSuccess {
			if snap := videoSettleSnapshot(task, reportedTokens, note); snap != nil {
				entry.PriceSnapshot = snap
			}
		}
	}
	if note != "" {
		if entry.UpstreamError == "" {
			entry.UpstreamError = note
		} else {
			entry.UpstreamError += "; " + note
		}
	}
	return entry
}

// ownedVideoTask loads the task and checks the caller owns it; a missing or
// foreign id answers 404 task_not_found — existence is not leaked.
func (h *Handlers) ownedVideoTask(c *gin.Context, key apikey.Key, id string) (videotask.Task, bool) {
	task, err := h.Tasks.Get(c.Request.Context(), id)
	if errors.Is(err, videotask.ErrNotFound) {
		apierr.OpenAI(c, http.StatusNotFound, CodeTaskNotFound, TypeInvalidRequestError, "No such task: "+id)
		return videotask.Task{}, false
	}
	if err != nil {
		h.failInternal(c, err)
		return videotask.Task{}, false
	}
	if task.KeyID != key.ID {
		apierr.OpenAI(c, http.StatusNotFound, CodeTaskNotFound, TypeInvalidRequestError, "No such task: "+id)
		return videotask.Task{}, false
	}
	return task, true
}

// parseVideoSubmit extracts the vendor task handle from a 2xx submit body.
func parseVideoSubmit(body []byte) (string, error) {
	var r struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("upstream submit body is not a JSON object: %w", err)
	}
	if strings.TrimSpace(r.TaskID) == "" {
		return "", errors.New("upstream submit response carries no task_id")
	}
	return strings.TrimSpace(r.TaskID), nil
}

// parseVideoQuery reads the vendor poll body: the raw status string (the
// caller merges it), the deliverable URL when succeeded, the vendor's
// failure message when failed, and the vendor-reported video tokens when it
// chose to report usage (25 号票:token 轨终态按 completion_tokens 结算;
// 缺报由调用方按预扣定格)。A body that does not parse is a transient
// upstream fault — the caller must not advance the task on it.
func parseVideoQuery(body []byte) (rawStatus, videoURL, failMessage string, completionTokens int64, err error) {
	var r struct {
		TaskStatus string `json:"task_status"`
		VideoURL   string `json:"video_url"`
		Error      *struct {
			Message string `json:"message"`
		} `json:"error"`
		Usage *struct {
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", "", "", 0, fmt.Errorf("upstream task body is not a JSON object: %w", err)
	}
	if r.Error != nil {
		failMessage = r.Error.Message
	}
	if r.Usage != nil {
		completionTokens = max(r.Usage.CompletionTokens, 0)
	}
	return r.TaskStatus, strings.TrimSpace(r.VideoURL), strings.TrimSpace(failMessage), completionTokens, nil
}

// videoSnapshot is the shape of a video task's submit-time price snapshot:
// the second track stores pricing.CallSnapshot's {unit, call, request{size,
// n}}; the token track (25 号票) stores {unit, token, request{size,
// seconds, est_tokens}} plus, on the settle row, a settle section with the
// vendor-reported tokens and any billing note. Both shapes unmarshal here —
// audits recompute from the usage row's snapshot, not the task row.
type videoSnapshot struct {
	Unit    pricing.Unit        `json:"unit"`
	Token   *pricing.TokenPrice `json:"token,omitempty"`
	Call    *pricing.CallPrice  `json:"call,omitempty"`
	Request *videoSnapRequest   `json:"request,omitempty"`
	Settle  *videoSnapSettle    `json:"settle,omitempty"`
}

// videoSnapRequest carries the request facts the charge derives from: the
// shared size key, the second track's billed quantity n (= seconds), the
// token track's seconds and estimated tokens.
type videoSnapRequest struct {
	Size      string `json:"size"`
	N         int64  `json:"n,omitempty"`
	Seconds   int64  `json:"seconds,omitempty"`
	EstTokens int64  `json:"est_tokens,omitempty"`
}

// videoSnapSettle carries the settle facts of a token-track success row: the
// vendor-reported video tokens and any billing note (e.g. the
// usage-missing fallback).
type videoSnapSettle struct {
	CompletionTokens int64  `json:"completion_tokens"`
	Note             string `json:"note,omitempty"`
}

// videoTokenSnapshot renders the token-track video snapshot at submit time:
// the price in force plus the request facts the estimate derives from.
func videoTokenSnapshot(price pricing.Price, size string, seconds, estTokens int64) ([]byte, error) {
	if price.Unit != pricing.UnitToken || price.Token == nil {
		return nil, fmt.Errorf("pricing: token snapshot of a %s-track price", price.Unit)
	}
	s := videoSnapshot{Unit: price.Unit, Token: price.Token,
		Request: &videoSnapRequest{Size: size, Seconds: seconds, EstTokens: estTokens}}
	return json.Marshal(s)
}

// videoBillingTrack reads the billing facts back from the task row's
// submit-time snapshot: which track the task settles on and, for the token
// track, the price to settle with. A snapshot that does not parse settles on
// the second track — the shape every pre-25 task row carries.
func videoBillingTrack(task videotask.Task) (pricing.Unit, *pricing.TokenPrice) {
	var snap videoSnapshot
	if len(task.PriceSnapshot) == 0 {
		return pricing.UnitSecond, nil
	}
	if err := json.Unmarshal(task.PriceSnapshot, &snap); err != nil {
		return pricing.UnitSecond, nil
	}
	if snap.Unit == pricing.UnitToken {
		return pricing.UnitToken, snap.Token
	}
	return pricing.UnitSecond, nil
}

// videoSettleSnapshot re-renders the token-track snapshot with the settle
// facts: the vendor-reported tokens and any billing note. The usage row's
// snapshot must let an audit recompute the charge without the task row.
func videoSettleSnapshot(task videotask.Task, reported int64, note string) []byte {
	var snap videoSnapshot
	if err := json.Unmarshal(task.PriceSnapshot, &snap); err != nil {
		return nil // 快照坏了:用量行留任务行原快照,账面事实不丢
	}
	snap.Settle = &videoSnapSettle{CompletionTokens: reported, Note: note}
	out, err := json.Marshal(snap)
	if err != nil {
		return nil
	}
	return out
}

func strPtr(s string) *string { return &s }

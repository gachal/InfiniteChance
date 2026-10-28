// 配乐异步任务(60 号票):契约照 /v1/videos 的两面 —— POST
// /v1/audio/music({model, prompt})受理回任务句柄,GET
// /v1/audio/music/tasks/{id} 轮询;取消面不做(MPS 取消接口未查证,前端
// 配乐任务不出取消按钮)。对外状态收敛五态(归并规则在 musictask 包:
// MPS 的 WAIT→queued、RUN→running、DONE→succeeded、FAIL→failed,未知态
// 归并 failed)。计费按次(UnitCall,一任务一首):提交时按单价预扣,
// 渠道在厂商受理那一刻钉死;轮询把任务推进到终态时一次结清 —— 成功定格
// 实扣(差额为零不动流水),失败全额退款,终态迁移落一条用量日志。轮询
// 本身的暂时失败不改状态不动账。调度常规语义(不同于音色/台词合成的钉
// 渠道):受理时按优先级/权重选渠道、上游临时失败可换道(预扣原封带往
// 下一渠道)、受理后钉死。
//
// 计费时限(无取消面的兜底):受理后 musicBillingDeadline 内仍未交付的
// 任务,在任意一次轮询时按失败收尾(全额退款 + 留痕),不再向厂商查询
// —— 钱不能被一个未知结局的任务无限占用;画布 worker 的排空轮询会观察到
// 这个终态(见 canvastask runMusic)。
package relay

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/musictask"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/usage"
)

// musicBillingDeadline bounds one task's money hold: an active task older
// than this closes as failed on the next poll without dialing upstream.
// 音乐生成普遍分钟级;30 分钟是宽上限,MPS 侧任务到不了这个年纪还没终态
// 就当它不会交付了(产物 URL 本就 24h 过期,迟到的交付也无人可取)。
const musicBillingDeadline = 30 * time.Minute

// maxMusicLyricsRunes 是歌词上限(62 号票:与模型 TokenHub 直连口径同值,
// VOD 透传无额外限制,真 key 复核);网关提交面按同值预检,超限在拨号之前
// 400。canvas 侧 canvastask/handler.go 有同源副本,改一边要同步另一边。
const maxMusicLyricsRunes = 3500

// musicRequest is the submit body's gateway slice —— 60 号票两字段,62 号票
// 追加 instrumental(bool 指针,缺省 true —— 纯音乐,60 号票既有调用完全
// 兼容)与 lyrics(可选字符串,人声歌曲的歌词;instrumental:false 且不带
// lyrics = 模型自动写词)。没有尺寸/秒数/参考维度(配乐没有可表达的参数
// 面;参考翻唱是上游 cover 模式,显式 out of scope)。
type musicRequest struct {
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	Instrumental *bool  `json:"instrumental"`
	Lyrics       string `json:"lyrics"`
}

// musicTaskJSON is the external task object: submit answers it fresh from
// the store, poll answers the stored row. 产物地址挂 audio.url 嵌套对象
// (与 /v1/audio/speech 的形状同族,和视频的扁平 video_url 区分开)。
type musicTaskJSON struct {
	ID        string           `json:"id"`
	Status    musictask.Status `json:"status"`
	Model     string           `json:"model"`
	CreatedAt int64            `json:"created_at"`
	Audio     *musicAudioJSON  `json:"audio,omitempty"`
	Error     *musicTaskError  `json:"error,omitempty"`
}

type musicAudioJSON struct {
	URL string `json:"url"`
}

type musicTaskError struct {
	Message string `json:"message"`
}

func musicTaskBody(t musictask.Task) musicTaskJSON {
	body := musicTaskJSON{
		ID:        t.ID,
		Status:    t.Status,
		Model:     t.PublicModel,
		CreatedAt: t.CreatedAt.Unix(),
	}
	if t.Status == musictask.StatusSucceeded && t.AudioURL != "" {
		body.Audio = &musicAudioJSON{URL: t.AudioURL}
	}
	if t.Status == musictask.StatusFailed && t.Error != "" {
		body.Error = &musicTaskError{Message: t.Error}
	}
	return body
}

// CreateMusicGeneration relays POST /v1/audio/music: validate → schedule
// music-capable candidates → price(call 轨,按次)→ reserve → try channels
// in order → the first vendor acceptance pins the channel, stores the task
// queued and hands back its task id. A rejected submit is an ordinary
// failover step(预扣原封带往下一渠道);候选用尽按同步轨收尾。
func (h *Handlers) CreateMusicGeneration(c *gin.Context) {
	key, _ := apikey.KeyFrom(c)

	p := h.prepareMusic(c, key)
	if p == nil {
		return
	}

	ctx := c.Request.Context()
	// 账务与留痕使用脱离请求的 context:提交中途客户端断开,已预扣的
	// 钱也必须退干净(与聊天/生图/视频轨同一纪律)。
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

		upstream, err := h.adaptorFor(at.ch).MusicSubmit(ctx, at.ch, at.payload)
		if err == nil && upstream.OK {
			vendorID, perr := parseMusicSubmit(upstream.Body)
			if perr == nil {
				run.breaker.RecordSuccess(at.ch.ID)
				h.musicSubmitted(c, billing, p, at, vendorID, run.retried)
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

// prepareMusic is the submit prefix: validate, schedule music-capable
// candidates, require a call-track price and take the pre-deduction
// (单价 × 1 —— 一任务一首). On any rejection the client has already been
// answered an OpenAI error object and the result is nil.
func (h *Handlers) prepareMusic(c *gin.Context, key apikey.Key) *prepared {
	ctx := c.Request.Context()

	raw, err := c.GetRawData()
	if err != nil {
		refuseBody(c, err)
		return nil
	}
	var req musicRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return nil
	}
	if req.Model == "" {
		apierr.OpenAI(c, http.StatusBadRequest, CodeMissingModel, TypeInvalidRequestError, "You must provide a 'model' parameter.")
		return nil
	}
	// 风格描述按原文计费与转发(不 trim 内容,只拒绝空串):首尾空白
	// 也是要给上游看的描述。
	if n := utf8.RuneCountInString(req.Prompt); n < 1 {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "You must provide a 'prompt' parameter.")
		return nil
	} else if n > mpsMaxPromptRunes {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			"The 'prompt' parameter exceeds the "+strconv.Itoa(mpsMaxPromptRunes)+"-character limit.")
		return nil
	}
	// 62 号票歌曲面:歌词上限与纯音乐互斥。显式 instrumental:true 携带
	// 歌词是客户端错误 —— 显式 400 优于静默忽略(歌词会被上游丢弃);
	// 纯空白歌词按未携带处理。打到 MPS 侧模型(非 vod- 前缀)时歌词会被
	// 静默降级成纯音乐,是 62 号票文档写明的已知边界,不在网关裁。
	if n := utf8.RuneCountInString(req.Lyrics); n > maxMusicLyricsRunes {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			"The 'lyrics' parameter exceeds the "+strconv.Itoa(maxMusicLyricsRunes)+"-character limit.")
		return nil
	}
	if req.Instrumental != nil && *req.Instrumental && strings.TrimSpace(req.Lyrics) != "" {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			"'lyrics' cannot be combined with instrumental:true; omit 'lyrics' or set instrumental:false for a vocal song.")
		return nil
	}

	channels, err := h.listChannels(ctx)
	if err != nil {
		h.failInternal(c, err)
		return nil
	}
	candidates := weightedOrder(eligibleChannels(channels, req.Model, channel.CapMusic), h.randIntn)
	if len(candidates) == 0 {
		apierr.OpenAI(c, http.StatusNotFound, CodeModelNotFound, TypeInvalidRequestError,
			"The model '"+req.Model+"' does not exist or no music-capable channel serves it.")
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
	if price.Unit != pricing.UnitCall || price.Call == nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' is not priced for per-call music billing.")
		return nil
	}

	// 先对首个候选重写一次模型名,证明 body 是可重写的 JSON 对象;
	// 逐渠道的真正重写在 attemptFor 里做(各渠道上游名不同)。
	if _, err := rewriteModel(raw, candidates[0].ModelMap[req.Model]); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return nil
	}

	// 预扣:一次一首,size 空串、n=1;审计快照记下请求事实。
	reserved := price.Call.ChargeMicros("", 1)
	snapshot, err := price.CallSnapshot("", 1)
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

	return &prepared{
		publicModel: req.Model,
		raw:         raw,
		key:         key, price: price,
		reserved: reserved, snapshot: snapshot,
		candidates: candidates,
		source:     SourceFrom(c),
	}
}

// musicSubmitted pins the accepting channel: the task row is created queued
// with the billing facts from submit time and the client gets the public
// task object. retried is the failover history of the submits abandoned
// before this one was accepted. A failed insert means the reserve would
// leak — refund and answer 500(与视频受理同款兜底)。
func (h *Handlers) musicSubmitted(c *gin.Context, billing context.Context, p *prepared, at attempt, vendorTaskID string, retried []string) {
	id, err := musictask.NewID()
	var task musictask.Task
	if err == nil {
		// 建行走脱离请求的 context:厂商已受理,客户端断开也不能让任务
		// 变成查不到的孤儿。
		task, err = h.MusicTasks.Create(billing, musictask.Task{
			ID: id, KeyID: p.key.ID, ChannelID: at.ch.ID, ChannelName: at.ch.Name,
			PublicModel: p.publicModel, UpstreamModel: at.upstreamModel,
			UpstreamTaskID: vendorTaskID, Status: musictask.StatusQueued,
			ReservedMicros: p.reserved, PriceSnapshot: p.snapshot,
			Error: upstreamErrorSummary(retried, ""),
		})
	}
	if err != nil {
		h.adjustBalance(billing, p.key.ID, p.reserved, apikey.ReasonRefund)
		log.Printf("relay: store music task for key %d: %v", p.key.ID, err)
		apierr.OpenAI(c, http.StatusInternalServerError, "internal_error", TypeServerError, "The gateway hit an internal error.")
		return
	}
	c.JSON(http.StatusOK, musicTaskBody(task))
}

// GetMusicTask relays GET /v1/audio/music/tasks/{id} — the poll. Terminal
// tasks answer from the store without touching the vendor(产物 URL 本就
// 临时,网关的账本事实才是契约);active tasks first hit the billing
// deadline sweep (超时收尾,退款不留悬念),then query the pinned channel,
// merge the vendor's raw status into the external machine, and close the
// books exactly once on the transition to a terminal state — the store's
// guarded update decides the winner when polls race.
func (h *Handlers) GetMusicTask(c *gin.Context) {
	key, _ := apikey.KeyFrom(c)
	task, ok := h.ownedMusicTask(c, key, c.Param("id"))
	if !ok {
		return
	}
	if musictask.Terminal(task.Status) {
		c.JSON(http.StatusOK, musicTaskBody(task))
		return
	}

	ctx := c.Request.Context()
	billing := context.WithoutCancel(ctx)

	// 计费时限收尾:active 且超过时限的任务按失败关闭(全额退款 + 留痕),
	// 不再向厂商查询。守卫 expect=active 态,与真正推进到终态的并发轮询
	// 竞速,输家以库里现行为准。
	if time.Since(task.CreatedAt) > musicBillingDeadline {
		summary := musicErrorWithHistory(task,
			"music billing deadline ("+musicBillingDeadline.String()+") exceeded without delivery")
		h.failMusicTask(c, billing, task, musictask.Patch{
			Status: musictask.StatusFailed,
			ErrMsg: &summary,
		})
		return
	}

	ch, err := h.Channels.Get(ctx, task.ChannelID)
	if errors.Is(err, channel.ErrNotFound) {
		// 渠道被删:没人能代理这个任务了。任务原地不动(等计费时限收尾),
		// 对客户端如实说明。
		apierr.OpenAI(c, http.StatusBadGateway, CodeUpstreamError, CodeUpstreamError,
			"The channel serving this task no longer exists; it can no longer be polled. It will be closed and refunded at the billing deadline.")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}

	upstream, err := h.adaptorFor(ch).MusicQuery(ctx, ch, task.UpstreamTaskID)
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
	rawStatus, audioURL, vendorErr, perr := parseMusicQuery(upstream.Body)
	if perr != nil {
		// 响应体不可解析同样是暂态上游故障,不能据此推进状态。
		apierr.OpenAI(c, http.StatusBadGateway, CodeUpstreamError, CodeUpstreamError,
			"Upstream task query failed: "+perr.Error())
		return
	}

	merged := musictask.MergeStatus(rawStatus)
	switch merged {
	case musictask.StatusQueued, musictask.StatusRunning:
		// 推进或刷新:状态只进不退(running 不会被打回 queued),原始
		// 状态串恒记最新一次。状态迁移走脱离请求的 context:它和账务
		// 一体,客户端在决定性的一次轮询中途断开也不能把迁移弄丢。
		status := merged
		if task.Status == musictask.StatusRunning && merged == musictask.StatusQueued {
			status = musictask.StatusRunning
		}
		if status != task.Status || rawStatus != task.UpstreamStatus {
			fresh, _, err := h.MusicTasks.Update(billing, task.ID, []musictask.Status{task.Status}, musictask.Patch{
				Status: status, UpstreamStatus: &rawStatus,
			})
			if err != nil {
				h.failInternal(c, err)
				return
			}
			task = fresh
		}
		c.JSON(http.StatusOK, musicTaskBody(task))
	case musictask.StatusSucceeded:
		if audioURL == "" {
			// 成功却没产物:对外不能算成功 —— 仅成功计费的前提是拿得到
			// 产物,与视频「成功却没 URL」同一判法。
			h.failMusicTask(c, billing, task, musictask.Patch{
				Status: musictask.StatusFailed, UpstreamStatus: &rawStatus,
				ErrMsg: strPtr(musicErrorWithHistory(task,
					"upstream reported "+strconv.Quote(rawStatus)+" without an audio URL")),
			})
			return
		}
		// 按次轨:实扣即预扣,差额恒零(零差额不动账),落成功留痕。
		charge := task.ReservedMicros
		fresh, won, err := h.MusicTasks.Update(billing, task.ID, musictask.ActiveStatuses, musictask.Patch{
			Status: musictask.StatusSucceeded, UpstreamStatus: &rawStatus,
			AudioURL: &audioURL, ChargeMicros: &charge,
		})
		if err != nil {
			h.failInternal(c, err)
			return
		}
		if won {
			h.adjustBalance(billing, task.KeyID, task.ReservedMicros-charge, apikey.ReasonSettle)
			h.recordUsage(billing, h.musicUsageEntry(fresh, usage.StatusSuccess, charge, ""))
		}
		c.JSON(http.StatusOK, musicTaskBody(fresh))
	default:
		// failed(含未知态归并):摘要优先厂商给的失败原因,缺了就报
		// 原始状态串。
		summary := vendorErr
		if summary == "" {
			summary = "upstream reported task status " + strconv.Quote(rawStatus)
		}
		h.failMusicTask(c, billing, task, musictask.Patch{
			Status: musictask.StatusFailed, UpstreamStatus: &rawStatus,
			ErrMsg: strPtr(musicErrorWithHistory(task, summary)),
		})
	}
}

// failMusicTask drives an active task to failed on the poll path: the
// guarded update is the single writer, and the winner refunds the whole
// reserve and leaves the upstream_error trail. Update rides the detached
// billing context — the transition and the books are one unit. The client
// gets the fresh row either way(输了竞态就答现状)。
func (h *Handlers) failMusicTask(c *gin.Context, billing context.Context, task musictask.Task, p musictask.Patch) {
	fresh, won, err := h.MusicTasks.Update(billing, task.ID, musictask.ActiveStatuses, p)
	if err != nil {
		h.failInternal(c, err)
		return
	}
	if won {
		h.adjustBalance(billing, task.KeyID, task.ReservedMicros, apikey.ReasonRefund)
		h.recordUsage(billing, h.musicUsageEntry(fresh, usage.StatusUpstreamError, 0, ""))
	}
	c.JSON(http.StatusOK, musicTaskBody(fresh))
}

// musicUsageEntry builds the trail row for a task's terminal transition:
// the pinned channel facts live on the task row, the lifetime is wall-clock
// since submit, and the unit is the call track the task settled on. The
// row's upstream_error column carries the task's accumulated history —
// submit-time failover first, then the closing summary.
func (h *Handlers) musicUsageEntry(task musictask.Task, status string, charge int64, note string) usage.Log {
	entry := usage.Log{
		KeyID: task.KeyID, ChannelID: task.ChannelID, ChannelName: task.ChannelName,
		PublicModel: task.PublicModel, UpstreamModel: task.UpstreamModel,
		Unit:       string(pricing.UnitCall),
		DurationMS: time.Since(task.CreatedAt).Milliseconds(),
		Status:     status, ChargeMicros: charge, PriceSnapshot: task.PriceSnapshot,
		UpstreamError: task.Error,
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

// ownedMusicTask loads the task and checks the caller owns it; a missing or
// foreign id answers 404 task_not_found — existence is not leaked.
func (h *Handlers) ownedMusicTask(c *gin.Context, key apikey.Key, id string) (musictask.Task, bool) {
	task, err := h.MusicTasks.Get(c.Request.Context(), id)
	if errors.Is(err, musictask.ErrNotFound) {
		apierr.OpenAI(c, http.StatusNotFound, CodeTaskNotFound, TypeInvalidRequestError, "No such task: "+id)
		return musictask.Task{}, false
	}
	if err != nil {
		h.failInternal(c, err)
		return musictask.Task{}, false
	}
	if task.KeyID != key.ID {
		apierr.OpenAI(c, http.StatusNotFound, CodeTaskNotFound, TypeInvalidRequestError, "No such task: "+id)
		return musictask.Task{}, false
	}
	return task, true
}

// parseMusicSubmit extracts the vendor task handle from a normalized 2xx
// submit body ({"task_id": …}).
func parseMusicSubmit(body []byte) (string, error) {
	var r struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", errors.New("upstream submit body is not a JSON object")
	}
	if strings.TrimSpace(r.TaskID) == "" {
		return "", errors.New("upstream submit response carries no task_id")
	}
	return strings.TrimSpace(r.TaskID), nil
}

// parseMusicQuery reads the normalized poll body: the raw status string
// (the caller merges it), the deliverable URL when succeeded, and the
// vendor's failure message when failed. A body that does not parse is a
// transient upstream fault — the caller must not advance the task on it.
func parseMusicQuery(body []byte) (rawStatus, audioURL, failMessage string, err error) {
	var r struct {
		TaskStatus string `json:"task_status"`
		AudioURL   string `json:"audio_url"`
		Error      *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", "", "", errors.New("upstream task body is not a JSON object")
	}
	if r.Error != nil {
		failMessage = r.Error.Message
	}
	return r.TaskStatus, strings.TrimSpace(r.AudioURL), strings.TrimSpace(failMessage), nil
}

// musicErrorWithHistory keeps the task row's error column cumulative:
// submit-time failover first, then the deciding summary(与视频任务行同
// 一形状;那边的 helper 吃 videotask.Task,这里另起一个小镜像)。
func musicErrorWithHistory(task musictask.Task, summary string) string {
	if task.Error == "" {
		return summary
	}
	return task.Error + "; " + summary
}

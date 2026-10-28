package canvastask

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
	"unicode/utf8"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/objectstore"
	"github.com/gachal/InfiniteChance/internal/voice"
)

// Gateway is the worker's view of the relay surface: the synchronous image
// calls — text-to-image (10 号票) and image-to-image (21 号票) — the async
// video contract's three faces — submit, poll, cancel (12 号票) — the
// 音色闭环的四端点加合成模型目录 (57 号票,钉渠道不换道), the 配乐异步
// 两面加模型目录 (60 号票,常规调度换道、无取消面) and the 音效同步单面加
// 模型目录 (61 号票,常规调度换道). *Client satisfies it; tests substitute
// fakes.
type Gateway interface {
	GenerateImage(ctx context.Context, req ImageRequest) (ImageResult, error)
	EditImage(ctx context.Context, req EditRequest) (ImageResult, error)
	SubmitVideo(ctx context.Context, req VideoRequest) (VideoSubmitResult, error)
	PollVideo(ctx context.Context, taskID string) (VideoPoll, error)
	CancelVideo(ctx context.Context, taskID string) error
	CreateVoice(ctx context.Context, req VoiceCreateRequest) (VoiceCreateResult, error)
	QueryVoice(ctx context.Context, channelID int64, voiceID, source string) (string, error)
	DeleteVoice(ctx context.Context, channelID int64, voiceID, source string) error
	SynthesizeSpeech(ctx context.Context, req SpeechRequest) (SpeechResult, error)
	ListAudioModels(ctx context.Context) (AudioModelCatalog, error)
	SubmitMusic(ctx context.Context, req MusicRequest) (MusicSubmitResult, error)
	PollMusic(ctx context.Context, taskID string) (MusicPoll, error)
	ListMusicModels(ctx context.Context) (MusicModelCatalog, error)
	CreateSfx(ctx context.Context, req SfxRequest) (SfxResult, error)
	ListSfxModels(ctx context.Context) (SfxModelCatalog, error)
}

// ChannelLister is the slice of the channel store the worker needs to
// auto-pin a channel for voice-creation tasks (57 号票):canvas 服务与网关
// 共享同一数据库,这里只读 —— 选渠道的过滤维度归渠道表管,与网关调度
// 同一事实源。nil 时 kind=voice 任务缺 channel_id 即失败(可读原因)。
type ChannelLister interface {
	List(ctx context.Context) ([]channel.Channel, error)
}

// VoiceLedger is the slice of the voices store the worker needs: 台词合成
// (kind=tts)运行时读台账行取渠道与上游句柄。nil 时 kind=tts 任务失败
// (可读原因)。
type VoiceLedger interface {
	Get(ctx context.Context, id int64) (voice.Voice, error)
}

// Worker runs the canvas generation queue server-side (10 号票): claim one
// queued task → call the gateway with the service-level key → finalize the
// task and its asset. Because the loop lives in canvas/server, a generation
// outlives the browser that requested it; results are on the task row when
// the editor comes back.
type Worker struct {
	store   Store
	gateway Gateway
	// storage 落产物字节(14 号票):nil 时跳过转存,素材行只带厂商地址
	// —— 与 10 号票的旧行为一致,测试与未配存储的部署都靠这个 nil 分支。
	storage objectstore.Store
	// canvases 解析任务画布的 IP 归属(50 号票:产物经画布传导);nil 时
	// 素材行落 0(全局)—— 旧装配形态与不关心 IP 的测试都靠 nil 分支。
	canvases CanvasGetter
	// channels / voices 是音色闭环的只读伴 store(57 号票):渠道目录
	// 供 kind=voice 自动钉渠道,音色台账供 kind=tts 取句柄。nil 的语义见
	// 各自接口注释(生产装配恒非 nil)。
	channels ChannelLister
	voices   VoiceLedger

	concurrency  int
	pollInterval time.Duration
	taskTimeout  time.Duration
	// 视频异步任务的两个旋钮(12 号票):网关轮询节奏与单个任务的时限。
	// 视频生成普遍在分钟级,时限远宽于同步生图。
	videoPollInterval time.Duration
	videoTaskTimeout  time.Duration
	// 音色任务的旋钮(57 号票):上游音色部署普遍秒级到一两分钟,轮询
	// 2s、总时限 2min —— 超时句柄保留(上游继续部署,预扣不退),重试
	// 续查不重复创建。
	voicePollInterval time.Duration
	voiceTaskTimeout  time.Duration
	// 配乐任务的旋钮(60 号票):音乐生成普遍分钟级,轮询照视频 3s、
	// 总时限 15min。无取消面 —— 超时后任务按失败收尾,worker 继续在后台
	// 排空轮询,驱动网关侧计费时限收尾退款(见 runMusic)。
	musicPollInterval time.Duration
	musicTaskTimeout  time.Duration
	// 音效任务的时限旋钮(61 号票):网关侧在 adaptor 内同步等待,独立
	// 预算 3min;worker 时限默认 10min —— 覆盖网关预算(3min × 含换道的
	// 多渠道尝试)再留转存余量,超时按失败收尾(可重试)。
	sfxTaskTimeout time.Duration
	logger         *log.Logger
}

// Defaults for a bare NewWorker: two concurrent generations, a 1s queue
// scan, and a 3min ceiling on one image call (同步生图普遍在秒级到分钟级;
// 超时视为失败,任务行给出原因,可重试). Video jobs poll the gateway every
// 3s and get a 15min ceiling — minute-scale generations with headroom; the
// deadline cancels the gateway task so the pre-deduction is refunded.
const (
	defaultConcurrency      = 2
	defaultPollInterval     = time.Second
	defaultTaskTimeout      = 3 * time.Minute
	defaultVideoPoll        = 3 * time.Second
	defaultVideoTaskTimeout = 15 * time.Minute
	defaultVoicePoll        = 2 * time.Second
	defaultVoiceTaskTimeout = 2 * time.Minute
	defaultMusicPoll        = 3 * time.Second
	defaultMusicTaskTimeout = 15 * time.Minute
	defaultSfxTaskTimeout   = 10 * time.Minute
)

// NewWorker wires a worker with production defaults; the With… options
// override them (tests mostly).
func NewWorker(store Store, gateway Gateway, opts ...WorkerOption) *Worker {
	w := &Worker{
		store:             store,
		gateway:           gateway,
		concurrency:       defaultConcurrency,
		pollInterval:      defaultPollInterval,
		taskTimeout:       defaultTaskTimeout,
		videoPollInterval: defaultVideoPoll,
		videoTaskTimeout:  defaultVideoTaskTimeout,
		voicePollInterval: defaultVoicePoll,
		voiceTaskTimeout:  defaultVoiceTaskTimeout,
		musicPollInterval: defaultMusicPoll,
		musicTaskTimeout:  defaultMusicTaskTimeout,
		sfxTaskTimeout:    defaultSfxTaskTimeout,
		logger:            log.Default(),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// WorkerOption customizes one Worker knob.
type WorkerOption func(*Worker)

// WithConcurrency caps how many generations run at once.
func WithConcurrency(n int) WorkerOption {
	return func(w *Worker) {
		if n > 0 {
			w.concurrency = n
		}
	}
}

// WithPollInterval sets the idle scan cadence of the queue loop.
func WithPollInterval(d time.Duration) WorkerOption {
	return func(w *Worker) { w.pollInterval = d }
}

// WithTaskTimeout bounds one gateway call.
func WithTaskTimeout(d time.Duration) WorkerOption {
	return func(w *Worker) { w.taskTimeout = d }
}

// WithVideoPollInterval sets the cadence of the video task's gateway polls.
func WithVideoPollInterval(d time.Duration) WorkerOption {
	return func(w *Worker) { w.videoPollInterval = d }
}

// WithVideoTaskTimeout bounds one video task from submit to terminal state.
func WithVideoTaskTimeout(d time.Duration) WorkerOption {
	return func(w *Worker) { w.videoTaskTimeout = d }
}

// WithVoicePollInterval sets the cadence of the voice task's gateway polls
// (57 号票:2s —— 部署普遍秒级,节奏要跟得上)。
func WithVoicePollInterval(d time.Duration) WorkerOption {
	return func(w *Worker) { w.voicePollInterval = d }
}

// WithVoiceTaskTimeout bounds one voice task from submit (or handle resume)
// to terminal state (57 号票:2min 上限,超时句柄保留)。
func WithVoiceTaskTimeout(d time.Duration) WorkerOption {
	return func(w *Worker) { w.voiceTaskTimeout = d }
}

// WithMusicPollInterval sets the cadence of the music task's gateway polls
// (60 号票:3s,与视频同拍)。
func WithMusicPollInterval(d time.Duration) WorkerOption {
	return func(w *Worker) { w.musicPollInterval = d }
}

// WithMusicTaskTimeout bounds one music task from submit to terminal state
// (60 号票:15min 上限;超时先失败收尾,再后台排空驱动网关退款)。
func WithMusicTaskTimeout(d time.Duration) WorkerOption {
	return func(w *Worker) { w.musicTaskTimeout = d }
}

// WithSfxTaskTimeout bounds one sfx task's synchronous gateway call plus the
// artifact transfer (61 号票:默认 10min —— 网关侧同步预算 3min 之上留
// 转存余量;超时按失败收尾,可重试)。
func WithSfxTaskTimeout(d time.Duration) WorkerOption {
	return func(w *Worker) { w.sfxTaskTimeout = d }
}

// WithChannels lets the worker auto-pin a channel for voice-creation tasks
// (57 号票,同库只读);nil 时 kind=voice 任务缺 channel_id 即失败。
func WithChannels(g ChannelLister) WorkerOption {
	return func(w *Worker) {
		if g != nil {
			w.channels = g
		}
	}
}

// WithVoiceLedger lets the worker read the voices rows for speech tasks
// (57 号票,同库只读);nil 时 kind=tts 任务失败。
func WithVoiceLedger(g VoiceLedger) WorkerOption {
	return func(w *Worker) {
		if g != nil {
			w.voices = g
		}
	}
}

// WithStorage routes generated artifacts into object storage before the
// task finalizes (14 号票); nil keeps the legacy vendor-URL-only rows.
func WithStorage(s objectstore.Store) WorkerOption {
	return func(w *Worker) { w.storage = s }
}

// WithCanvases lets the worker carry each task's canvas IP into its asset
// row (50 号票的归属传导);nil keeps rows at 0(全局)。
func WithCanvases(g CanvasGetter) WorkerOption {
	return func(w *Worker) {
		if g != nil {
			w.canvases = g
		}
	}
}

// canvasIPID resolves the task canvas's owning IP. 画布已删落 0(不阻塞,
// 素材行落全局);其他查询错误同样落 0 但留日志 —— IP 归属是过滤/展示
// 维度,不能让它拦住一次成功的生成。
func (w *Worker) canvasIPID(ctx context.Context, t Task) int64 {
	if w.canvases == nil {
		return 0
	}
	c, err := w.canvases.Get(ctx, t.CanvasID)
	if err != nil {
		if !errors.Is(err, canvas.ErrNotFound) {
			w.logger.Printf("canvastask: resolve ip of canvas %d for %s: %v", t.CanvasID, t.ID, err)
		}
		return 0
	}
	return c.IPID
}

// WithLogger routes worker logging (nil keeps the default logger).
func WithLogger(l *log.Logger) WorkerOption {
	return func(w *Worker) {
		if l != nil {
			w.logger = l
		}
	}
}

// maxErrorRunes keeps a failure reason well inside the TEXT error column.
const maxErrorRunes = 500

// Run drives the queue until ctx is canceled. Each free concurrency slot
// claims the next queued task and runs it in its own goroutine; when the
// queue is empty the loop sleeps a poll interval.
func (w *Worker) Run(ctx context.Context) {
	slots := make(chan struct{}, w.concurrency)
	for {
		// 空位先占住再认领:没有空位就不从队列拿任务,任务留在 queued。
		select {
		case <-ctx.Done():
			return
		case slots <- struct{}{}:
		}

		task, err := w.store.Claim(ctx)
		if errors.Is(err, ErrNotFound) {
			<-slots
			if !w.sleep(ctx) {
				return
			}
			continue
		}
		if err != nil {
			<-slots
			w.logger.Printf("canvastask: claim: %v", err)
			if !w.sleep(ctx) {
				return
			}
			continue
		}

		go func(t Task) {
			defer func() { <-slots }()
			w.runOne(ctx, t)
		}(task)
	}
}

// sleep waits one poll interval, reporting whether the loop should go on.
func (w *Worker) sleep(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(w.pollInterval):
		return true
	}
}

// runOne carries one claimed task through the gateway and closes it out,
// dispatching on the task kind: the synchronous image call, the async
// video contract's submit-and-poll loop, or the 音色闭环的两支 (57 号票
// —— voice 的部署轮询与 tts 的同步合成).
func (w *Worker) runOne(parent context.Context, t Task) {
	switch t.Kind {
	case KindVideo:
		w.runVideo(parent, t)
		return
	case KindVoice:
		w.runVoice(parent, t)
		return
	case KindTts:
		w.runTts(parent, t)
		return
	case KindMusic:
		w.runMusic(parent, t)
		return
	case KindSfx:
		w.runSfx(parent, t)
		return
	}

	ctx, cancel := context.WithTimeout(parent, w.taskTimeout)
	defer cancel()

	w.logger.Printf("canvastask: running %s (canvas %d, node %s, attempt %d)",
		t.ID, t.CanvasID, t.NodeID, t.Attempts)
	// 图生图分流(21 号票):带参考图的任务走 edits multipart,其余与
	// 文生图同一 generations 调用;产物/记账/失败路径两种形态完全同构。
	// Background(37 号票)两种形态同语义:非空即上送(行上只可能是
	// "transparent",createInput 已校验),空串不落线。
	source := fmt.Sprintf("canvas=%d task=%s node=%s", t.CanvasID, t.ID, t.NodeID)
	var result ImageResult
	var err error
	if len(t.ImageRefs) > 0 {
		result, err = w.gateway.EditImage(ctx, EditRequest{
			Model:      t.Model,
			Prompt:     t.Prompt,
			Size:       t.Size,
			Background: t.Background,
			Images:     t.ImageRefs,
			Source:     source,
		})
	} else {
		result, err = w.gateway.GenerateImage(ctx, ImageRequest{
			Model:      t.Model,
			Prompt:     t.Prompt,
			Size:       t.Size,
			Background: t.Background,
			Source:     source,
		})
	}
	bookkeeping := context.WithoutCancel(parent)
	if err != nil {
		w.fail(bookkeeping, t.ID, err.Error())
		return
	}
	a, aerr := w.archive(bookkeeping, t, result.URL)
	if aerr != nil {
		w.fail(bookkeeping, t.ID, aerr.Error())
		return
	}
	finalized, won, err := w.store.FinalizeSuccess(bookkeeping, t.ID, a)
	if err != nil {
		w.logger.Printf("canvastask: finalize success %s: %v", t.ID, err)
		return
	}
	if !won {
		// 终态被抢先(理论上只有重启恢复可能撞上):不重复落素材。
		w.logger.Printf("canvastask: finalize success %s lost the race (status now %s)", t.ID, finalized.Status)
		return
	}
	w.logger.Printf("canvastask: task %s succeeded, asset %d", t.ID, finalized.AssetID)
}

// archive 产物转存(14 号票):storage 未配置时返回只带厂商地址的素材
// 事实(旧行为);否则把字节落进对象存储并带上 object_key 三件套。转存
// 失败以 error 上抛,由调用方按任务失败收尾 —— 原因留在任务行上,可重
// 试,不悄悄留下只有临时厂商地址(约 24h 过期)的素材。
func (w *Worker) archive(ctx context.Context, t Task, url string) (asset.Asset, error) {
	a := asset.Asset{
		Kind:     t.Kind,
		CanvasID: t.CanvasID,
		TaskID:   t.ID,
		Model:    t.Model,
		Prompt:   t.Prompt,
		URL:      url,
		IPID:     w.canvasIPID(ctx, t), // 50 号票:归属随画布;画布已删落 0
	}
	if w.storage == nil {
		return a, nil
	}
	stored, err := asset.Transfer(ctx, w.storage, nil, t.CanvasID, t.ID, t.Kind, url)
	if err != nil {
		return asset.Asset{}, err
	}
	a.ObjectKey = stored.Key
	a.ContentType = stored.ContentType
	a.SizeBytes = stored.SizeBytes
	a.ContentHash = stored.ContentHash // 48 号票:转存链只落列不复用
	return a, nil
}

// fail closes a task failed with a truncated reason (the error column is
// TEXT; a vendor stack trace is not a reason).
func (w *Worker) fail(ctx context.Context, id, reason string) {
	if utf8.RuneCountInString(reason) > maxErrorRunes {
		reason = string([]rune(reason)[:maxErrorRunes])
	}
	if _, _, ferr := w.store.FinalizeFailure(ctx, id, reason); ferr != nil {
		w.logger.Printf("canvastask: finalize failure %s: %v", id, ferr)
	}
}

// videoRequestFor translates one video task row into the gateway submit
// (24 号票):结构化参考按角色落位 —— first_frame → image、last_frame →
// last_image、reference_* 按 kind 进 references;ratio/size/seconds 直传
// (seconds 0 = 自动,client 侧不落线)。12 号票的单串 ImageRef 旧形态在
// 没有结构化参考时兜底为首帧,在途旧任务升级后照跑。
func videoRequestFor(t Task) VideoRequest {
	req := VideoRequest{
		Model:   t.Model,
		Prompt:  t.Prompt,
		Seconds: t.Seconds,
		Size:    t.Size,
		Ratio:   t.Ratio,
		Image:   t.ImageRef,
		Source:  fmt.Sprintf("canvas=%d task=%s node=%s", t.CanvasID, t.ID, t.NodeID),
	}
	for _, r := range t.VideoRefs {
		switch r.Role {
		case RoleFirstFrame:
			req.Image = r.URL
		case RoleLastFrame:
			req.LastImage = r.URL
		default:
			req.References = append(req.References, VideoRefRequest{URL: r.URL, Kind: r.Kind})
		}
	}
	return req
}

// runVideo drives one image-to-video task through the gateway's async
// contract (12 号票): submit → persist the remote handle → poll until the
// gateway reaches a terminal state or the task deadline hits. The user's
// cancel runs on the HTTP handler — it cancels the gateway task (releasing
// the quota hold) and closes the row; the poll loop simply observes whatever
// the gateway then answers, and its guarded finalizers lose cleanly when the
// handler got there first.
func (w *Worker) runVideo(parent context.Context, t Task) {
	ctx, cancel := context.WithTimeout(parent, w.videoTaskTimeout)
	defer cancel()

	bookkeeping := context.WithoutCancel(parent)
	// 重试或重启恢复的任务带着上一次的远端任务:先取消它 —— 失败任务上
	// 它已终态(取消只是对账),崩溃遗留的任务则由此释放预扣。
	if t.RemoteTaskID != "" {
		if err := w.gateway.CancelVideo(bookkeeping, t.RemoteTaskID); err != nil {
			w.logger.Printf("canvastask: cancel stale remote %s for %s: %v", t.RemoteTaskID, t.ID, err)
		}
	}

	w.logger.Printf("canvastask: running %s (canvas %d, node %s, kind video, attempt %d)",
		t.ID, t.CanvasID, t.NodeID, t.Attempts)
	ref, err := w.gateway.SubmitVideo(ctx, videoRequestFor(t))
	if err != nil {
		w.fail(bookkeeping, t.ID, err.Error())
		return
	}

	// 远端句柄落行,取消与恢复才找得到它。行已不在 running(提交在途时
	// 被取消)则当场取消刚受理的网关任务,预扣由网关原路退回。落行本身
	// 的 DB 错误不拦轮询:句柄仍在进程手里,丢进程的极端情形由重启恢复
	// 兜底(与 10 号票同一责任边界)。
	if _, ok, aerr := w.store.AttachRemote(bookkeeping, t.ID, ref.TaskID); aerr != nil {
		w.logger.Printf("canvastask: attach remote %s to %s: %v", ref.TaskID, t.ID, aerr)
	} else if !ok {
		if cerr := w.gateway.CancelVideo(bookkeeping, ref.TaskID); cerr != nil {
			w.logger.Printf("canvastask: cancel race-lost remote %s for %s: %v", ref.TaskID, t.ID, cerr)
		}
		return
	}

	for {
		select {
		case <-ctx.Done():
			// 时限到:取消网关任务让预扣退回,任务行按失败收尾并给出原因
			// (可重试)。取消走脱离时限的 context —— 决定性的收尾不因
			// deadline 到点而丢失。
			if cerr := w.gateway.CancelVideo(bookkeeping, ref.TaskID); cerr != nil {
				w.logger.Printf("canvastask: cancel on timeout for %s (remote %s): %v", t.ID, ref.TaskID, cerr)
			}
			w.fail(bookkeeping, t.ID, fmt.Sprintf("生成超时(上限 %s),已取消网关任务", w.videoTaskTimeout))
			return
		case <-time.After(w.videoPollInterval):
		}

		// 先看本地行:用户取消时 handler 会先取消网关任务再关行,但那次
		// RPC 可能失败(只留日志)。行已不在 running 而网关任务还在跑,
		// 厂商若先完成,网关会照常结算 —— 就地补一次取消,预扣原路退回。
		if row, gerr := w.store.Get(ctx, t.ID); gerr == nil && Terminal(row.Status) {
			if cerr := w.gateway.CancelVideo(bookkeeping, ref.TaskID); cerr != nil {
				w.logger.Printf("canvastask: cancel abandoned remote %s for %s: %v", ref.TaskID, t.ID, cerr)
			}
			return
		}

		poll, err := w.gateway.PollVideo(ctx, ref.TaskID)
		if err != nil {
			// 一次轮询失败不是任务失败:网关的轮询是代理语义,暂态故障
			// 稍后再试;总时限兜底。
			w.logger.Printf("canvastask: poll %s (remote %s): %v", t.ID, ref.TaskID, err)
			continue
		}
		switch poll.Status {
		case string(StatusQueued), string(StatusRunning):
			continue
		case string(StatusSucceeded):
			if poll.VideoURL == "" {
				w.fail(bookkeeping, t.ID, "网关报告任务成功但没有视频地址")
				return
			}
			a, aerr := w.archive(bookkeeping, t, poll.VideoURL)
			if aerr != nil {
				w.fail(bookkeeping, t.ID, aerr.Error())
				return
			}
			finalized, won, err := w.store.FinalizeVideoSuccess(bookkeeping, t.ID, a)
			if err != nil {
				w.logger.Printf("canvastask: finalize success %s: %v", t.ID, err)
				return
			}
			if !won {
				w.logger.Printf("canvastask: finalize success %s lost the race (status now %s)", t.ID, finalized.Status)
				return
			}
			w.logger.Printf("canvastask: task %s succeeded, asset %d", t.ID, finalized.AssetID)
			return
		case string(StatusCanceled):
			if _, _, ferr := w.store.FinalizeCanceled(bookkeeping, t.ID); ferr != nil {
				w.logger.Printf("canvastask: finalize canceled %s: %v", t.ID, ferr)
			}
			return
		default: // failed(网关已把未知态归并 failed)
			reason := poll.Error
			if reason == "" {
				reason = "上游报告任务失败"
			}
			w.fail(bookkeeping, t.ID, reason)
			return
		}
	}
}

// nodeTaskSource 是节点任务的 X-InfiniteChance-Source 值(图片/视频的
// canvas=%d task=%s node=%s 同族,kind 进来源串供审计分辨;音色闭环两支
// 与配乐共用)。
func nodeTaskSource(t Task) string {
	return fmt.Sprintf("canvas=%d task=%s node=%s kind=%s", t.CanvasID, t.ID, t.NodeID, t.Kind)
}

// pickVoiceChannel auto-pins the channel for a voice-creation task whose
// spec carries no channel (57 号票):启用 + audio 能力 + 同时映射
// voice-enrollment 与目标合成模型 的渠道里取优先级最高者(平局按 id 升序
// —— 与网关调度「优先级分层」同一语义,确定性排序让重试与观察可复现)。
// 渠道表只读:过滤维度归渠道表管,选出来的 channel_id 落 voice_spec,
// 重试/query/后续合成复用同一渠道。
func (w *Worker) pickVoiceChannel(ctx context.Context, targetModel string) (int64, error) {
	if w.channels == nil {
		return 0, errors.New("画布服务未装配渠道目录,无法为音色创建选择渠道")
	}
	channels, err := w.channels.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("读取渠道目录失败: %w", err)
	}
	var best *channel.Channel
	for i := range channels {
		ch := &channels[i]
		if !ch.Enabled || !ch.HasCapability(channel.CapAudio) {
			continue
		}
		if ch.ModelMap[enrollmentModel] == "" || ch.ModelMap[targetModel] == "" {
			continue
		}
		if best == nil || ch.Priority > best.Priority || (ch.Priority == best.Priority && ch.ID < best.ID) {
			best = ch
		}
	}
	if best == nil {
		return 0, fmt.Errorf("没有启用且具备 audio 能力、同时映射 voice-enrollment 与 %s 的渠道", targetModel)
	}
	return best.ID, nil
}

// runVoice drives one voice-creation task through the gateway's pinned
// audio contract (57 号票): 选渠道 → create(收到 voice_id 先落行)→
// 试听转存 → 轮询 query 至 OK → 台账行 + 试听素材 + 任务终态同事务。
// remote 句柄纪律同视频:重启恢复/重试带句柄先 query 校验(OK 直接
// finalize;UNDEPLOYED 丢句柄重建;不存在/被拒重建);轮询超时句柄保留
// (上游继续部署,预扣不退),重试续查不重复创建;UNDEPLOYED → failed
// (上游审核未通过,丢弃句柄)。
func (w *Worker) runVoice(parent context.Context, t Task) {
	ctx, cancel := context.WithTimeout(parent, w.voiceTaskTimeout)
	defer cancel()
	bookkeeping := context.WithoutCancel(parent)

	spec := t.VoiceSpec
	source := nodeTaskSource(t)
	if spec.TargetModel == "" || spec.Name == "" || spec.PreviewText == "" {
		w.fail(bookkeeping, t.ID, "任务行缺少音色创建事实(target_model/name/preview_text)")
		return
	}

	// 渠道钉定:spec 没带(首次运行)时自动选,选到先落行再创建 ——
	// 重启恢复与重试必须回到同一渠道,音色是渠道工作空间的所有物。
	channelID := spec.ChannelID
	if channelID <= 0 {
		picked, err := w.pickVoiceChannel(ctx, spec.TargetModel)
		if err != nil {
			w.fail(bookkeeping, t.ID, err.Error())
			return
		}
		if _, ok, aerr := w.store.AttachVoiceChannel(bookkeeping, t.ID, picked); aerr != nil {
			// 落行失败不拦创建(句柄仍在进程手里);极端情形由恢复路径兜底。
			w.logger.Printf("canvastask: attach channel %d to %s: %v", picked, t.ID, aerr)
		} else if !ok {
			w.fail(bookkeeping, t.ID, "任务行已不在运行态,音色创建中止")
			return
		} else {
			channelID = picked
		}
	}

	var preview *asset.Asset // 试听素材(试听转存成功才有;nil = 台账落无预览形态)

	handle := spec.RemoteVoiceID
	if handle != "" {
		status, err := w.gateway.QueryVoice(ctx, channelID, handle, source)
		switch {
		case err == nil && status == voice.StatusOK:
			// 上次运行已部署完成:直接收尾(试听素材已随崩溃丢失,台账行
			// 落无预览形态 —— 素材行是转存产物,不是台账的成立条件)。
			w.finalizeVoice(bookkeeping, t, spec, channelID, handle, nil)
			return
		case err == nil && status == voice.StatusUndeployed:
			// 上游审核未通过:句柄作废,就地重建新音色(重试口径)。
			w.logger.Printf("canvastask: voice %s of %s undeployed, rebuilding", handle, t.ID)
			if _, _, aerr := w.store.AttachRemoteVoice(bookkeeping, t.ID, ""); aerr != nil {
				w.logger.Printf("canvastask: clear stale voice handle on %s: %v", t.ID, aerr)
			}
			handle = ""
		case err == nil:
			// 仍在部署或其他态:进轮询循环续查。
		default:
			var ue *voice.UpstreamError
			if errors.As(err, &ue) && ue.Status < 500 {
				// 网关裁决的永久拒绝(句柄不存在/渠道没了):句柄作废重建。
				w.logger.Printf("canvastask: voice %s of %s gone upstream (%d), rebuilding: %s", handle, t.ID, ue.Status, ue.Message)
				if _, _, aerr := w.store.AttachRemoteVoice(bookkeeping, t.ID, ""); aerr != nil {
					w.logger.Printf("canvastask: clear stale voice handle on %s: %v", t.ID, aerr)
				}
				handle = ""
			} else {
				// 暂态(传输失败/熔断中):进轮询循环,循环里继续 query。
				w.logger.Printf("canvastask: query %s (voice %s): %v", t.ID, handle, err)
			}
		}
	}

	if handle == "" {
		w.logger.Printf("canvastask: running %s (canvas %d, node %s, kind voice, attempt %d)",
			t.ID, t.CanvasID, t.NodeID, t.Attempts)
		res, err := w.gateway.CreateVoice(ctx, VoiceCreateRequest{
			TargetModel: spec.TargetModel,
			Mode:        spec.Mode,
			ChannelID:   channelID,
			VoicePrompt: spec.VoicePrompt,
			Audio:       spec.AudioURL,
			PreviewText: spec.PreviewText,
			Source:      source,
		})
		if err != nil {
			w.fail(bookkeeping, t.ID, err.Error())
			return
		}
		// 句柄先落行(视频 AttachRemote 同款):重启恢复与重试才知道该
		// query 谁。voice 任务不可取消,行不在 running 的竞态窗口几乎不
		// 存在,落行失败只留日志。
		if _, _, aerr := w.store.AttachRemoteVoice(bookkeeping, t.ID, res.VoiceID); aerr != nil {
			w.logger.Printf("canvastask: attach remote voice %s to %s: %v", res.VoiceID, t.ID, aerr)
		}
		handle = res.VoiceID
		// 试听转存(best-effort):失败不拦部署轮询 —— 台账行是闭环的
		// 主体,试听素材缺失只是节点上少一个播放器。
		if p, err := w.archiveAudio(bookkeeping, t, previewDataURI(res), spec.PreviewText); err != nil {
			w.logger.Printf("canvastask: transfer voice preview of %s: %v", t.ID, err)
		} else if p != nil {
			preview = p
		}
	}

	for {
		select {
		case <-ctx.Done():
			// 时限到:句柄保留(上游继续部署,预扣不退),失败原因指明可
			// 重试续查 —— 与视频「超时即取消」不同,音色没有可撤销的上游
			// 提交,保留句柄是唯一不浪费这次扣费的选择。
			w.fail(bookkeeping, t.ID, fmt.Sprintf("音色部署轮询超时(上限 %s),上游句柄已保留,重试将继续查询", w.voiceTaskTimeout))
			return
		case <-time.After(w.voicePollInterval):
		}

		status, err := w.gateway.QueryVoice(ctx, channelID, handle, source)
		if err != nil {
			// 一次轮询失败不是任务失败:暂态故障稍后再试;总时限兜底。
			// 网关裁决的 4xx(渠道被删/句柄被旁路删除)才就地收尾。
			var ue *voice.UpstreamError
			if errors.As(err, &ue) && ue.Status < 500 {
				w.fail(bookkeeping, t.ID, err.Error())
				return
			}
			w.logger.Printf("canvastask: poll %s (voice %s): %v", t.ID, handle, err)
			continue
		}
		switch status {
		case voice.StatusDeploying:
			continue
		case voice.StatusOK:
			w.finalizeVoice(bookkeeping, t, spec, channelID, handle, preview)
			return
		case voice.StatusUndeployed:
			// 上游审核未通过:丢弃句柄再失败,重试走全新创建。
			if _, _, aerr := w.store.AttachRemoteVoice(bookkeeping, t.ID, ""); aerr != nil {
				w.logger.Printf("canvastask: drop voice handle on %s: %v", t.ID, aerr)
			}
			w.fail(bookkeeping, t.ID, "上游审核未通过,该音色不可用(已丢弃句柄,重试将重新创建)")
			return
		default:
			w.fail(bookkeeping, t.ID, "上游报告未知音色状态 "+status)
			return
		}
	}
}

// finalizeVoice assembles the ledger row and closes the task through the
// store's one-transaction finalizer.
func (w *Worker) finalizeVoice(ctx context.Context, t Task, spec VoiceSpec, channelID int64, remoteVoiceID string, preview *asset.Asset) {
	in := voice.CreateInput{
		VoiceID:        remoteVoiceID,
		ChannelID:      channelID,
		TargetModel:    spec.TargetModel,
		Name:           spec.Name,
		SourceMode:     spec.Mode,
		SourcePrompt:   spec.VoicePrompt,
		SampleAssetID:  spec.SampleAssetID,
		Status:         voice.StatusOK,
	}
	if spec.Mode == voice.SourceModeClone {
		// clone 的出处是样本地址(design 存音色描述,同列两语义)。
		in.SourcePrompt = spec.AudioURL
	}
	finalized, won, err := w.store.FinalizeVoiceSuccess(ctx, t.ID, in, preview)
	if err != nil {
		w.logger.Printf("canvastask: finalize voice success %s: %v", t.ID, err)
		return
	}
	if !won {
		w.logger.Printf("canvastask: finalize voice success %s lost the race (status now %s)", t.ID, finalized.Status)
		return
	}
	w.logger.Printf("canvastask: task %s succeeded, voice %q, preview asset %d", t.ID, remoteVoiceID, finalized.AssetID)
}

// previewDataURI wraps the create response's Base64 preview as a data: URI
// — asset.Transfer decodes those in place, so the preview lands in our own
// storage without ever touching the wire twice. 无试听(上游没给)时零值
// 交给调用方跳过。
func previewDataURI(res VoiceCreateResult) string {
	if res.PreviewBase64 == "" {
		return ""
	}
	format := res.PreviewFormat
	if format == "" {
		format = "wav"
	}
	return "data:audio/" + format + ";base64," + res.PreviewBase64
}

// runTts carries one speech task through the gateway's pinned-character
// contract (57 号票): 运行时读台账行(校验 status=OK,取钉定渠道与上游
// 句柄 —— 提交到运行之间被删/审核翻转的任务在这里自然失败)→ 同步合成
// → 产物转存 kind=audio 素材行 → FinalizeTtsSuccess 同事务收尾。
func (w *Worker) runTts(parent context.Context, t Task) {
	ctx, cancel := context.WithTimeout(parent, w.taskTimeout)
	defer cancel()
	bookkeeping := context.WithoutCancel(parent)

	spec := t.VoiceSpec
	if spec.LedgerID <= 0 {
		w.fail(bookkeeping, t.ID, "任务行缺少音色台账引用(ledger_id)")
		return
	}
	if t.Prompt == "" {
		w.fail(bookkeeping, t.ID, "任务行缺少台词文本")
		return
	}
	if w.voices == nil {
		w.fail(bookkeeping, t.ID, "画布服务未装配音色台账,无法合成台词")
		return
	}
	v, err := w.voices.Get(ctx, spec.LedgerID)
	if err != nil {
		if errors.Is(err, voice.ErrVoiceNotFound) {
			w.fail(bookkeeping, t.ID, "音色不存在或已删除,无法合成台词")
			return
		}
		w.fail(bookkeeping, t.ID, fmt.Sprintf("读取音色台账失败: %v", err))
		return
	}
	if v.Status != voice.StatusOK {
		w.fail(bookkeeping, t.ID, "音色「"+v.Name+"」当前状态 "+v.Status+",尚不可合成台词")
		return
	}

	w.logger.Printf("canvastask: running %s (canvas %d, node %s, kind tts, attempt %d)",
		t.ID, t.CanvasID, t.NodeID, t.Attempts)
	res, err := w.gateway.SynthesizeSpeech(ctx, SpeechRequest{
		Model:       v.TargetModel,
		Voice:       v.VoiceID,
		Text:        t.Prompt,
		Instruction: spec.Instruction,
		ChannelID:   v.ChannelID,
		Source:      nodeTaskSource(t),
	})
	if err != nil {
		w.fail(bookkeeping, t.ID, err.Error())
		return
	}
	a, aerr := w.archiveAudio(bookkeeping, t, res.AudioURL, t.Prompt)
	if aerr != nil {
		w.fail(bookkeeping, t.ID, aerr.Error())
		return
	}
	finalized, won, err := w.store.FinalizeTtsSuccess(bookkeeping, t.ID, *a, res.Chars)
	if err != nil {
		w.logger.Printf("canvastask: finalize tts success %s: %v", t.ID, err)
		return
	}
	if !won {
		w.logger.Printf("canvastask: finalize tts success %s lost the race (status now %s)", t.ID, finalized.Status)
		return
	}
	w.logger.Printf("canvastask: task %s succeeded, asset %d", t.ID, finalized.AssetID)
}

// archiveAudio is the audio twin of archive (57 号票):产物一律 kind=audio
// 素材行,对象键按 canvases/ 档的 audio 扩展名归档(Transfer 按响应
// Content-Type 定扩展,wav/m4a/mp3 各归其位)。storage 未配置时返回只带
// 厂商临时地址的素材事实(约 24h 过期,旧行为兜底)。
func (w *Worker) archiveAudio(ctx context.Context, t Task, url, prompt string) (*asset.Asset, error) {
	if url == "" {
		return nil, nil
	}
	a := &asset.Asset{
		Kind:     asset.KindAudio,
		CanvasID: t.CanvasID,
		TaskID:   t.ID,
		Model:    t.Model,
		Prompt:   prompt,
		URL:      url,
		IPID:     w.canvasIPID(ctx, t), // 50 号票:归属随画布;画布已删落 0
	}
	if w.storage == nil {
		return a, nil
	}
	stored, err := asset.Transfer(ctx, w.storage, nil, t.CanvasID, t.ID, asset.KindAudio, url)
	if err != nil {
		return nil, err
	}
	a.ObjectKey = stored.Key
	a.ContentType = stored.ContentType
	a.SizeBytes = stored.SizeBytes
	a.ContentHash = stored.ContentHash // 48 号票:转存链只落列不复用
	return a, nil
}

// musicMaxConsecutivePollErrors 是配乐轮询的连续失败容忍:超过即按失败
// 收尾(网关持续不可达时不能无限占用 worker 槽位)。3s 轮询下 20 次
// ≈ 1 分钟,远宽于一次网络抖动。
const musicMaxConsecutivePollErrors = 20

// runMusic drives one 配乐 task through the gateway's async two-face
// contract (60 号票): submit → persist the remote handle → poll until the
// gateway reaches a terminal state. 无取消面 —— 超时语义分两层:网关侧
// musicBillingDeadline(30min)是账务的最终裁决,active 超龄任务在任意
// 一次轮询时按失败收尾退款;worker 侧 musicTaskTimeout(15min)只裁
// 「轮询持续失败/网关不可达」,健康的任务即便过了 15min 也继续等网关的
// 终态(音乐生成普遍分钟级,真跑满 15min 极罕见;由网关时限兜底收尾,
// worker 的续期轮询驱动它)。行上重试/重启恢复带句柄时不再重复提交 ——
// 旧句柄继续轮询(与音色句柄续查同哲学:提交即计费,重复提交是双倍
// 账);句柄已终态失败才就地重新提交。
func (w *Worker) runMusic(parent context.Context, t Task) {
	ctx, cancel := context.WithTimeout(parent, w.musicTaskTimeout)
	defer cancel()
	bookkeeping := context.WithoutCancel(parent)

	w.logger.Printf("canvastask: running %s (canvas %d, node %s, kind music, attempt %d)",
		t.ID, t.CanvasID, t.NodeID, t.Attempts)
	source := nodeTaskSource(t)

	handle := t.RemoteTaskID
	if handle != "" {
		// 恢复/重试:先验旧句柄。成功 → 直接走收尾(崩溃前已交付);仍在
		// 途或查询暂态失败 → 续查(waitMusic 的失败容忍兜底)—— 提交即
		// 计费,查询没成功就重提是双倍账;只有查询成功且句柄已终态失败/
		// 取消才重走全新提交(重试口径)。
		poll, err := w.gateway.PollMusic(ctx, handle)
		switch {
		case err == nil && poll.Status == string(StatusSucceeded):
			w.finishMusic(bookkeeping, t, poll)
			return
		case err == nil && Terminal(Status(poll.Status)):
			// 终态失败/取消:句柄作废,重走全新提交(重试口径)。
			w.logger.Printf("canvastask: stale music remote %s of %s terminal (%s), resubmitting",
				handle, t.ID, poll.Status)
		default:
			w.waitMusic(ctx, bookkeeping, t, handle, source)
			return
		}
	}

	// 歌曲事实(62 号票)随行重提:instrumental 与 lyrics 落任务列,恢复/
	// 重试的全新提交原样携带 —— instrumental=false 不落库会把人声歌曲重提
	// 成纯音乐。纯音乐行(60 号票缺省)显式带 instrumental=true,与网关
	// 缺省同义。
	instrumental := t.Instrumental
	ref, err := w.gateway.SubmitMusic(ctx, MusicRequest{
		Model:        t.Model,
		Prompt:       t.Prompt,
		Source:       source,
		Lyrics:       t.Lyrics,
		Instrumental: &instrumental,
	})
	if err != nil {
		w.fail(bookkeeping, t.ID, err.Error())
		return
	}

	// 远端句柄落行,恢复与重试才找得到它。落行失败不拦轮询:句柄仍在
	// 进程手里,丢进程的极端情形由重启恢复兜底;行已不在 running(提交
	// 在途时被收尾)则放弃 —— 网关侧任务由计费时限收尾退款。
	if _, ok, aerr := w.store.AttachRemote(bookkeeping, t.ID, ref.TaskID); aerr != nil {
		w.logger.Printf("canvastask: attach remote %s to %s: %v", ref.TaskID, t.ID, aerr)
	} else if !ok {
		w.logger.Printf("canvastask: task %s left running before remote attach; gateway task %s will be closed at the billing deadline", t.ID, ref.TaskID)
		return
	}

	w.waitMusic(ctx, bookkeeping, t, ref.TaskID, source)
}

// waitMusic is runMusic's poll loop: poll the gateway until a terminal
// state, renewing once past the worker deadline for as long as the gateway
// itself stays reachable (它的计费时限是终态的保证)。连续轮询失败超过
// 容忍即就地失败(网关不可达时不能无限占用槽位);超时且最后一拍健康
// 则续期一轮 —— 网关的计费时限(30min)必然在两轮时限内给出终态。
func (w *Worker) waitMusic(ctx context.Context, bookkeeping context.Context, t Task, handle, source string) {
	for phase := 0; ; phase++ {
		if phase > 0 {
			// 续期轮:上一轮 deadline 到点但网关仍健康可达 —— 新开一个
			// 时限继续等它的终态(由网关计费时限兜底,不会无限续)。
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(bookkeeping, w.musicTaskTimeout)
			defer cancel()
			w.logger.Printf("canvastask: music %s (remote %s) still active past deadline; waiting for the gateway's terminal state", t.ID, handle)
		}
		consecutiveErrors := 0
	watch:
		for {
			select {
			case <-ctx.Done():
				if consecutiveErrors > 0 {
					w.fail(bookkeeping, t.ID,
						fmt.Sprintf("网关轮询持续失败(连续 %d 次),已停止等待;网关侧预扣最迟由计费时限收尾退款", consecutiveErrors))
					return
				}
				// 最后一拍健康:网关还在跑。外层 phase 循环续期一轮。
				break watch
			case <-time.After(w.musicPollInterval):
			}

			poll, err := w.gateway.PollMusic(ctx, handle)
			if err != nil {
				consecutiveErrors++
				w.logger.Printf("canvastask: poll music %s (remote %s): %v", t.ID, handle, err)
				if consecutiveErrors >= musicMaxConsecutivePollErrors {
					w.fail(bookkeeping, t.ID,
						fmt.Sprintf("网关轮询连续失败 %d 次: %v", consecutiveErrors, err))
					return
				}
				continue
			}
			consecutiveErrors = 0

			switch poll.Status {
			case string(StatusQueued), string(StatusRunning):
				continue
			case string(StatusCanceled):
				if _, _, ferr := w.store.FinalizeCanceled(bookkeeping, t.ID); ferr != nil {
					w.logger.Printf("canvastask: finalize canceled %s: %v", t.ID, ferr)
				}
				return
			case string(StatusSucceeded):
				w.finishMusic(bookkeeping, t, poll)
				return
			default: // failed(网关已把未知态归并 failed)
				reason := poll.Error
				if reason == "" {
					reason = "上游报告任务失败"
				}
				w.fail(bookkeeping, t.ID, reason)
				return
			}
		}
	}
}

// finishMusic archives the delivered artifact and closes the task through
// the store's one-transaction finalizer. 排队晚到的成功(超时收尾后仍可能
// 被续期轮询观察到)与竞态输家都只留日志 —— 素材行随终态同事务,行不在
// running 就一起回滚,不落孤儿素材。
func (w *Worker) finishMusic(ctx context.Context, t Task, poll MusicPoll) {
	if poll.AudioURL == "" {
		w.fail(ctx, t.ID, "网关报告任务成功但没有音频地址")
		return
	}
	a, aerr := w.archiveAudio(ctx, t, poll.AudioURL, t.Prompt)
	if aerr != nil {
		w.fail(ctx, t.ID, aerr.Error())
		return
	}
	finalized, won, err := w.store.FinalizeMusicSuccess(ctx, t.ID, *a)
	if err != nil {
		w.logger.Printf("canvastask: finalize music success %s: %v", t.ID, err)
		return
	}
	if !won {
		w.logger.Printf("canvastask: finalize music success %s lost the race (status now %s)", t.ID, finalized.Status)
		return
	}
	w.logger.Printf("canvastask: task %s succeeded, asset %d", t.ID, finalized.AssetID)
}

// runSfx carries one 音效 task through the gateway's synchronous contract
// (61 号票):同步 CreateSfx → 产物转存 kind=audio 素材行 → 复用
// FinalizeMusicSuccess 收尾(音效与配乐同构:事实都在任务行的
// prompt/model/seconds 列,voice_spec 一列不动,前端按 source 判别回填)。
// 恢复语义照 runTts 现状,不另发明:同步任务没有远端句柄可续,重启回队
// (RequeueRunning)后按全新调用重跑 —— 双计费窗口 = 上一次网关调用已结清
// (second 轨预扣即实结或失败退款)但进程在收尾前崩溃,重试是一次全新的
// 预扣-结清。窗口有界:网关同步面在 adaptor 预算(3min,含换道候选)内
// 必然给出终态,挂死的请求按 504 全额退款,不存在无限占用;这与 tts 的
// 同款取舍(重跑优于丢产物),注释留痕不改变行为。
func (w *Worker) runSfx(parent context.Context, t Task) {
	ctx, cancel := context.WithTimeout(parent, w.sfxTaskTimeout)
	defer cancel()
	bookkeeping := context.WithoutCancel(parent)

	if t.Prompt == "" {
		w.fail(bookkeeping, t.ID, "任务行缺少音效描述")
		return
	}
	if t.Seconds < sfxMinSeconds || t.Seconds > sfxMaxSeconds {
		w.fail(bookkeeping, t.ID, fmt.Sprintf("任务行的秒数 %d 不在 %d–%d 域", t.Seconds, sfxMinSeconds, sfxMaxSeconds))
		return
	}

	w.logger.Printf("canvastask: running %s (canvas %d, node %s, kind sfx, attempt %d)",
		t.ID, t.CanvasID, t.NodeID, t.Attempts)
	res, err := w.gateway.CreateSfx(ctx, SfxRequest{
		Model:   t.Model,
		Text:    t.Prompt,
		Seconds: t.Seconds,
		Source:  nodeTaskSource(t),
	})
	if err != nil {
		w.fail(bookkeeping, t.ID, err.Error())
		return
	}
	a, aerr := w.archiveAudio(bookkeeping, t, res.AudioURL, t.Prompt)
	if aerr != nil {
		w.fail(bookkeeping, t.ID, aerr.Error())
		return
	}
	finalized, won, err := w.store.FinalizeMusicSuccess(bookkeeping, t.ID, *a)
	if err != nil {
		w.logger.Printf("canvastask: finalize sfx success %s: %v", t.ID, err)
		return
	}
	if !won {
		w.logger.Printf("canvastask: finalize sfx success %s lost the race (status now %s)", t.ID, finalized.Status)
		return
	}
	w.logger.Printf("canvastask: task %s succeeded, asset %d", t.ID, finalized.AssetID)
}

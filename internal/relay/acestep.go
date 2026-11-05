// acestep adaptor(80 号票):自托管 ACE-Step 1.5 的官方 acestep-api 异步
// REST 面(同曲换词通路二落,Mureka 通路搁置休眠)。与 OpenAI 兼容面不同
// 源 —— 可选 Bearer 鉴权(服务端 ACESTEP_API_KEY 非空才开启)、异步任务:
// 下载参考字节后 POST /release_task 以 multipart 提交 cover 任务(参考
// 音频文件字段 src_audio + 全文新词 lyrics),POST /query_result 批量轮询,
// 产物地址从 result 双层 JSON 的 file 相对路径拼 BaseURL 而来(/v1/audio
// 下载面,LAN 可达)。本 adaptor 只实现 cover 语义:audio_ref 非空 = 同曲
// 换词请求,歌词必填(cover 任务词是身份必需);audio_ref 为空的普通音乐
// 请求 = 请求级 400 指路普通音乐公开名。三个形状 sentinel 与上传链失败
// sentinel 都由 CreateMusicGeneration 归为请求级失败(不换道、不记熔断
// 账、预扣原退、0 usage 行,79 号票 mureka 同构)。
//
// ── 契约面(全部钉定,[官方] = docs/en/API.md @ ace-step/ACE-Step-1.5,
//
//	2026-11-05 调研;契约笔记 .scratch/infinite-chance/research/
//	acestep-api-contract.md 为 adaptor 地基)──
//
//	[官方] 提交  POST {base}/release_task —— JSON / multipart / urlencoded
//	       皆收。本 adaptor 走 **multipart**(契约笔记裁决:src_audio_path
//	       文档原文「requires absolute path on server」,远程 URL 直传未获
//	       文档支持;multipart 的 src_audio 文件字段是一等上传形态,与
//	       mureka 上传链同构,不算 wrapper。URL 直传留真机验证点,可行再
//	       省下载另票)。
//	[官方] 轮询  POST {base}/query_result —— body {"task_id_list":[…]};
//	       信封 {data:[…], code, error, timestamp, extra},data 是数组。
//	[官方] 状态  status 是 **整数**:0 = queued/running,1 = succeeded,
//	       2 = failed。归一时 0 → running(进行中两档合一,状态只进不退)。
//	[官方] 产物  result 是 **JSON 字符串**(双层编码,须二次 unmarshal)
//	       成数组,元素 file = "/v1/audio?path=…" **相对路径**,完整产物
//	       URL = {BaseURL}{file}。
//	[官方] 错误  FastAPI 形 {"detail":…};429 = 队列满(上游临时失败)、
//	       415 = Content-Type 不支持(同样临时态)→ 合成可换道 502;
//	       400 = 参数错(网关 sentinel 已前置拦截)→ 透传请求级。
//
// 票面定案 4:cover 提交体不送 model/vocal_language/instrumental/时长
// —— model_map 只作调度单元不映射上游档名(cover 是否收 model 留真机
// 验证),vocal_language 走上游 CoT 自侦测,cover 恒人声且时长跟随参考。
// 聊天/生图/视频/音频四方法与音效是显式不支持桩:acestep 渠道只声明
// music 能力,调度过滤下到不了这些方法(错误体形状经 ErrorSummary 宽解析
// 即可,无需 mureka 式专项注释)。
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// ── 常量面:路径与探测共用(channel.AcestepHealthPath),其余只归本
//
//	文件。──
const (
	// [官方] cover 任务提交路径(multipart)。
	acestepReleasePath = "/release_task"
	// [官方] 批量查询路径(JSON body)。
	acestepQueryPath = "/query_result"
	// acestepUploadFilename 是 multipart 的 src_audio 文件字段文件名(上游
	// 按扩展名认容器的可能性存在;实际格式以参考地址的产物为准,mp3 为主
	// —— murekaUploadFilename 同款判断)。
	acestepUploadFilename = "reference.mp3"
	// acestepRefDownloadBudget bounds the download of the resolved reference
	// address inside an independent budget —— 与 murekaRefDownloadBudget 同
	// 因:大音频下载可能远超 upstreamTimeout,vod sfx 的 3min 独立预算先例。
	// 下载与提交两跳沿用各自预算;提交后不再有上传中间环节(acestep 的
	// multipart 是「下载 + 提交」两跳,比 mureka 少一跳上传)。
	acestepRefDownloadBudget = 3 * time.Minute
)

// ── 请求级 sentinel(80 号票;文案即客户端文案,handler 侧
//
//	run.requestRefused = 400 invalid_request_error、预扣原退、不换道、
//	不记熔断账、0 usage 行,79 号票 mureka 同构)。──
var (
	// errAcestepRequiresLyrics:audio_ref 非空而歌词空 —— cover 无自动
	// 写词,词是身份必需(80 号票定案 6)。
	errAcestepRequiresLyrics = errors.New("acestep cover 仅受理同曲换词:歌词必填(cover 任务词是身份必需)")
	// errAcestepRequiresAudioRef:audio_ref 为空的普通音乐请求 —— 参考
	// 是身份所在,普通音乐请走其他公开名。
	errAcestepRequiresAudioRef = errors.New("acestep cover 仅受理同曲换词(参考音频是身份必需);普通音乐请用其他公开名")
	// errAcestepUploadFailed:下载链任何失败按请求级失败处理(79 号票
	// 定案 6 同款:参考是身份所在,不静默降级)。动态文案由
	// fmt.Errorf("%w: 环节: 原因") 携带,errors.Is 判 root。
	errAcestepUploadFailed = errors.New("acestep cover 参考音频下载失败")
	// errAcestepInvalidShape:参数形状非法(cover_strength 越界等 ——
	// 网关 prepareMusic 已前置校验,这里是纵深防御:请求体在网关与
	// adaptor 之间被改写过的未来路径也拦得住)。动态文案同款包裹。
	errAcestepInvalidShape = errors.New("acestep cover 参数形状非法")
)

// acestepMusicBody is the gateway submit body's adaptor slice。AudioRef 到
// 这里恒是已解析的上游可拉取地址(prepareMusic 的 resolveAudioRef 改写
// 过);CoverStrength 网关已按 [0,1] 前置校验,adaptor 层只作防御性复查。
type acestepMusicBody struct {
	Model         string   `json:"model"`
	Prompt        string   `json:"prompt"`
	Lyrics        string   `json:"lyrics"`
	AudioRef      string   `json:"audio_ref"`
	CoverStrength *float64 `json:"cover_strength"`
}

// acestepAdaptor implements Adaptor for acestep channels. Normalize*/聊天/
// 生图/视频/音频/音效方法全部继承 openAIAdaptor 并显式覆盖为不支持桩
// (该类型渠道只声明 music 能力,调度不会把别的请求落上来);ErrorSummary
// 沿用宽解析(FastAPI 的 {"detail":…} 无 error.message,回落原文截断)。
type acestepAdaptor struct {
	openAIAdaptor
}

// newAcestepAdaptor returns the production adaptor — stateless apart from
// the pooled transport, one instance serves every channel of the type.
func newAcestepAdaptor() *acestepAdaptor {
	return &acestepAdaptor{openAIAdaptor: openAIAdaptor{Client: &http.Client{Transport: upstreamTransport}}}
}

// acestepErrorBody renders one OpenAI-shaped error body(murekaErrorBody
// 同款形状,只进 usage 摘要与客户端错误)。
func acestepErrorBody(message string) []byte {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{"message": message, "param": nil, "code": "upstream_error"},
	})
	return body
}

// acestepCall builds and executes one acestep-api HTTP call (POST JSON /
// GET),Bearer-authed only when the channel carries a key(鉴权是服务端可选
// 开关:无 key 渠道是合法形态,发空 Bearer 反而可能在服务端开鉴权时误报
// 凭据失败 —— tester.testAcestep 同款口径),body-capped like every
// upstream call(murekaCall 同款)。
func (a *acestepAdaptor) acestepCall(ctx context.Context, ch channel.Channel, method, path string, payload []byte) (*UpstreamResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, ch.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if ch.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+ch.APIKey)
	}

	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody))
	if err != nil {
		return nil, err
	}
	return &UpstreamResponse{
		Status: resp.StatusCode,
		Body:   raw,
		OK:     resp.StatusCode >= 200 && resp.StatusCode < 300,
	}, nil
}

// downloadReference fetches the resolved reference address inside the
// independent download budget(acestepRefDownloadBudget)。体积上限显式
// 失败而非静默截断:参考是身份所在,截断会让提交字节短缺还不报错。上游
// 自报体积超 maxUpstreamBody(32MiB,对齐全体上游调用的读上限)即拒绝
// —— 上游本会拒收,提前在下载环节报「参考音频过大」(mureka 同款判定)。
func (a *acestepAdaptor) downloadReference(ctx context.Context, ref string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, acestepRefDownloadBudget)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("reference answered HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxUpstreamBody {
		return nil, fmt.Errorf("reference audio too large (exceeds the %d MiB fetch cap; Content-Length %d)", maxUpstreamBody>>20, resp.ContentLength)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxUpstreamBody {
		return nil, fmt.Errorf("reference audio too large (exceeds the %d MiB fetch cap)", maxUpstreamBody>>20)
	}
	if len(raw) == 0 {
		return nil, errors.New("reference is empty")
	}
	return raw, nil
}

// MusicSubmit runs the cover 语义:形状 sentinel → 下载参考字节 → multipart
// 提交 /release_task,归一 {"task_id"}。与 mureka 的两跳上传链(下载 →
// 上传 → 提交)相比少一跳:multipart 直接把参考字节当文件字段带走,
// 「上传」与「提交」合一。
func (a *acestepAdaptor) MusicSubmit(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	var req acestepMusicBody
	if err := json.Unmarshal(payload, &req); err != nil {
		// 契约体不可解析是请求侧问题:400 形拒绝,不换道(与 vod/minimax 同款)。
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: acestepErrorBody("acestep: request body is not a valid JSON object")}, nil
	}
	ref := strings.TrimSpace(req.AudioRef)
	// 三个形状 sentinel(见 var 块注释):先判参考再判词 —— 没有 reference
	// 的请求根本不是同曲换词,文案要指路普通音乐公开名;cover_strength
	// 越界是参数形状非法(网关已前置校验,这里纵深防御)。
	if ref == "" {
		return nil, errAcestepRequiresAudioRef
	}
	lyrics := strings.TrimSpace(req.Lyrics)
	if lyrics == "" {
		return nil, errAcestepRequiresLyrics
	}
	if req.CoverStrength != nil && coverStrengthOutOfRange(*req.CoverStrength) {
		return nil, fmt.Errorf("%w: cover_strength 需在 0 到 1 之间", errAcestepInvalidShape)
	}

	// 下载链:参考字节独立预算;任何失败都按请求级失败处理
	// (errAcestepUploadFailed 包裹,见 var 块注释)。
	audio, err := a.downloadReference(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("%w: 下载参考音频失败: %v", errAcestepUploadFailed, err)
	}

	upstream, err := a.releaseCoverTask(ctx, ch, lyrics, req.Prompt, req.CoverStrength, audio)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	// 信封 {data:[{task_id,…}], code, error} 取 task_id(解析从宽:id
	// 回退,mureka 同款)。
	var r struct {
		Data []struct {
			TaskID string `json:"task_id"`
			ID     string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(upstream.Body, &r)
	taskID := ""
	if len(r.Data) > 0 {
		taskID = strings.TrimSpace(r.Data[0].TaskID)
		if taskID == "" {
			taskID = strings.TrimSpace(r.Data[0].ID)
		}
	}
	if taskID == "" {
		// 2xx 无任务 id 的防御:合成 502 形,与上游失败同路可换道不建行
		// (minimax/mureka 同款)。
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: acestepErrorBody("acestep: 2xx submit carries no task id")}, nil
	}
	renamed, _ := json.Marshal(map[string]string{"task_id": taskID})
	return &UpstreamResponse{Status: upstream.Status, Body: renamed, OK: true}, nil
}

// releaseCoverTask posts one cover task to /release_task as multipart
// (契约笔记裁决,见文件头注释):task_type=cover、src_audio 文件字段、
// lyrics 全文新词、prompt 非空才送、audio_cover_strength 请求携带才送
// (%g 浮点文本)。票面定案 4 的禁送键(model/vocal_language/instrumental/
// 时长)在这里由「不写即不送」保证。
func (a *acestepAdaptor) releaseCoverTask(ctx context.Context, ch channel.Channel, lyrics, prompt string, strength *float64, audio []byte) (*UpstreamResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("task_type", "cover"); err != nil {
		return nil, err
	}
	if err := w.WriteField("lyrics", lyrics); err != nil {
		return nil, err
	}
	// prompt 选填(80 号票定案 6 沿 79):非空透传(2000 上限网关已管),
	// 空则不带该字段。
	if p := strings.TrimSpace(prompt); p != "" {
		if err := w.WriteField("prompt", p); err != nil {
			return nil, err
		}
	}
	// 旋律保持强度(80 号票定案 5):请求携带才送,%g 浮点文本;缺省不传
	// = 走上游缺省(上游缺省 1.0,契约笔记注记)。
	if strength != nil {
		if err := w.WriteField("audio_cover_strength", strconv.FormatFloat(*strength, 'g', -1, 64)); err != nil {
			return nil, err
		}
	}
	part, err := w.CreateFormFile("src_audio", acestepUploadFilename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.BaseURL+acestepReleasePath, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if ch.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+ch.APIKey)
	}

	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody))
	if err != nil {
		return nil, err
	}
	out := &UpstreamResponse{
		Status: resp.StatusCode,
		Body:   raw,
		OK:     resp.StatusCode >= 200 && resp.StatusCode < 300,
	}
	// 错误映射(契约笔记「错误形状」节):429 = 队列满、415 = Content-Type
	// 不支持 —— 都是上游临时态,合成可换道 502(带原文摘要);其余状态
	// 原样透传 —— 400 参数错归请求级(retryable 判否不换道),5xx 天然
	// 可换道。
	if !out.OK && (out.Status == http.StatusTooManyRequests || out.Status == http.StatusUnsupportedMediaType) {
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: acestepErrorBody(fmt.Sprintf("acestep: release_task answered HTTP %d: %s", out.Status, a.ErrorSummary(raw)))}, nil
	}
	return out, nil
}

// acestepMusicStatus translates one raw integer status onto the gateway 词表
// ([官方] 0/1/2):0 = queued/running 两档合一 → running(状态只进不退,
// 提交后网关任务行自 queued 起步,归 running 不回退);1 = succeeded;
// 2 = failed。未知整数值不猜 —— 调用方按暂态 502 处理(轮询不改状态
// 不动账;62 号票 VOD 的 WAITING 曾因直透被 MergeStatus default 误杀成
// failed,翻译层宁可暂态也不误杀)。
func acestepMusicStatus(status int) (string, bool) {
	switch status {
	case 0:
		return "running", true
	case 1:
		return "succeeded", true
	case 2:
		return "failed", true
	default:
		return "", false
	}
}

// acestepEnvelopeError leniently extracts one failure message from the
// envelope's error field(形状未钉定:字符串 / {"message":…} / {"detail":…}
// 都可能 —— FastAPI 的 HTTP 错误体用 detail,信封内错误形状真机验证点)。
func acestepEnvelopeError(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		return strings.TrimSpace(asString)
	}
	var asObject struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(raw, &asObject) == nil {
		if s := strings.TrimSpace(asObject.Message); s != "" {
			return s
		}
		return strings.TrimSpace(asObject.Detail)
	}
	return truncate(trimmed, 256)
}

// acestepTranslateQuery normalizes one /query_result envelope body into the
// gateway's internal task face so the relay's parseMusicQuery keeps a single
// contract(79 号票分层:murekaTranslateQuery 同构 —— adaptor 内归一,
// music.go 的 parseMusicQuery 保持单一出口契约)。result 是 JSON 字符串须
// 二次 unmarshal;succeeded 的产物地址取元素 file 相对路径拼 BaseURL。
// A body without any known shape (data 空 / 元素缺 status / status 越界 /
// result 解不动) is a transient upstream fault — the caller must not
// advance the task on it.
func (a *acestepAdaptor) acestepTranslateQuery(ch channel.Channel, body []byte) ([]byte, error) {
	var r struct {
		Data []struct {
			TaskID string          `json:"task_id"`
			Status *int            `json:"status"`
			Result string          `json:"result"`
			Error  json.RawMessage `json:"error"`
		} `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("upstream task body is not a JSON object: %w", err)
	}
	if len(r.Data) == 0 {
		return nil, errors.New("upstream task envelope carries no data entry")
	}
	elem := r.Data[0]
	if elem.Status == nil {
		return nil, errors.New("upstream task entry carries no status")
	}
	word, ok := acestepMusicStatus(*elem.Status)
	if !ok {
		return nil, fmt.Errorf("upstream task status %d is outside the [官方] 0/1/2 enum", *elem.Status)
	}
	out := map[string]any{"task_status": word}
	if word == "succeeded" {
		// result 是 JSON 字符串(双层编码):先落字符串再解数组,取第一个
		// 非空 file 相对路径拼 BaseURL 成完整产物地址(契约笔记「产物
		// 地址」节)。解不动 = 上游异常,暂态 502;解动了却没有 file =
		// succeeded 照旧透出(audio_url 空),网关既有「成功却没产物」
		// 规则裁决(不可计成功)。
		var items []struct {
			File string `json:"file"`
		}
		if err := json.Unmarshal([]byte(elem.Result), &items); err != nil {
			return nil, fmt.Errorf("upstream result string is not a JSON array: %v", err)
		}
		for _, item := range items {
			if f := strings.TrimSpace(item.File); f != "" {
				out["audio_url"] = strings.TrimRight(ch.BaseURL, "/") + f
				break
			}
		}
	}
	// 失败原因:优先元素内 error,回落信封 error(形状未钉定,宽解析)。
	failMsg := acestepEnvelopeError(elem.Error)
	if failMsg == "" {
		failMsg = acestepEnvelopeError(r.Error)
	}
	if failMsg != "" {
		out["error"] = map[string]string{"message": failMsg}
	}
	return json.Marshal(out)
}

// MusicQuery polls one acestep task via the batch face. The vendor envelope
// is normalized into the gateway's internal task face
// (acestepTranslateQuery); only then does the relay's merge-and-settle
// machinery see it.
func (a *acestepAdaptor) MusicQuery(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	payload, err := json.Marshal(map[string]any{"task_id_list": []string{upstreamTaskID}})
	if err != nil {
		return nil, err
	}
	upstream, err := a.acestepCall(ctx, ch, http.MethodPost, acestepQueryPath, payload)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	normalized, err := a.acestepTranslateQuery(ch, upstream.Body)
	if err != nil {
		// 查询响应不可解析 = 暂态上游故障:502 形归一,轮询不改状态不动账。
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: acestepErrorBody("acestep: " + err.Error())}, nil
	}
	return &UpstreamResponse{Status: upstream.Status, Body: normalized, OK: true}, nil
}

// 聊天/生图/视频/音频/音效面不归这个 adaptor:acestep 渠道只声明 music
// 能力,这些方法只作防御 —— 返回错误让 relay 按上游失败处理(调度过滤下
// 到不了这里);将来接 ACE-Step 的对话外能力时把桩换成透传即可(mureka
// 同款形状)。
func (a *acestepAdaptor) ChatCompletions(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: acestep adaptor does not relay chat completions")
}

func (a *acestepAdaptor) ChatCompletionsStream(ctx context.Context, ch channel.Channel, publicModel string, payload []byte) (*UpstreamStream, error) {
	return nil, errors.New("relay: acestep adaptor does not relay chat completions")
}

func (a *acestepAdaptor) ImagesGenerations(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: acestep adaptor does not relay images")
}

func (a *acestepAdaptor) ImagesEdits(ctx context.Context, ch channel.Channel, contentType string, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: acestep adaptor does not relay images")
}

func (a *acestepAdaptor) VideosSubmit(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: acestep adaptor does not relay videos")
}

func (a *acestepAdaptor) VideosQuery(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	return nil, errors.New("relay: acestep adaptor does not relay videos")
}

func (a *acestepAdaptor) VideosCancel(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	return nil, errors.New("relay: acestep adaptor does not relay videos")
}

func (a *acestepAdaptor) VoicesCreate(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: acestep adaptor does not relay audio voices")
}

func (a *acestepAdaptor) VoicesQuery(ctx context.Context, ch channel.Channel, upstreamModel, voiceID string) (*UpstreamResponse, error) {
	return nil, errors.New("relay: acestep adaptor does not relay audio voices")
}

func (a *acestepAdaptor) VoicesDelete(ctx context.Context, ch channel.Channel, upstreamModel, voiceID string) (*UpstreamResponse, error) {
	return nil, errors.New("relay: acestep adaptor does not relay audio voices")
}

func (a *acestepAdaptor) SpeechSynthesis(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: acestep adaptor does not relay speech synthesis")
}

func (a *acestepAdaptor) SfxSynthesis(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: acestep adaptor does not relay sfx synthesis")
}

// coverStrengthOutOfRange reports whether one client-supplied cover_strength
// is outside the closed [0,1] domain(80 号票定案 5:负数、>1、NaN/Inf 都
// 拒)。JSON 面的 NaN/Inf 字面量会被编码层先拒(NaN 非法、1e999 上溢
// unmarshal 报错),这里同时守护 adaptor 纵深防御与未来内部调用方。
func coverStrengthOutOfRange(v float64) bool {
	return math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1
}

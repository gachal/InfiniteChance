// mureka adaptor(79 号票):Mureka 原生音乐 API 的 Song Edit(同曲换词)
// 面。与 OpenAI 兼容面不同源 —— Bearer 鉴权、异步任务:参考音频先经
// /v1/files/upload 换文件 id,再 POST /v1/song/region-edit 提交换词编辑,
// GET /v1/song/query/{task_id} 轮询。本 adaptor 只实现 Edit 语义(79 号票
// 定案 2:最小面;Reference/Remix 等模式留增量):audio_ref 非空 = 同曲
// 换词请求,歌词必填(Edit 无自动写词,词是身份必需);audio_ref 为空的
// 普通音乐请求 = 请求级 400 指路普通音乐公开名。两个形状 sentinel 与
// 上传链失败 sentinel 都由 CreateMusicGeneration 归为请求级失败(不换道、
// 不记熔断账、预扣原退、0 用量行,78 号票 errAudioRefUnsupported 同构)。
//
// ── 路径与参数面(集中存证;[官方] = platform.mureka.ai 文档,2026-11-05
//
//	   调研;其余均为真 key 验证点,票面验证点 3)──
//
//		[官方] 提交  POST {base}/v1/song/region-edit —— "Edit a song based on
//		       the specified time range and input lyrics",异步任务,Bearer。
//		[官方] 轮询  GET  {base}/v1/song/query/{task_id}
//		[官方] 上传  POST {base}/v1/files/upload —— multipart/form-data(非
//		       JSON);file(二进制)与 url(地址)互斥;purpose 必填枚举
//		       (reference/melody/instrumental/voice/audio/remix/soundtrack/
//		       lyrics-video);音频 mp3/m4a ≤10MB。应答为「uploaded File
//		       object」,id 字段名官方页未渲染(解析从宽:id/file_id)。
//		参数字段名官方 schema 未公开渲染,按 fal 托管镜像 mureka/api/song/
//		edit 的参数形状兜底提交:audio_id / lyrics(必填)/ edit_start /
//		edit_end(毫秒,fal 标必填;fal 侧区域 ≥3s)。轮询的状态词表与产物
//		URL 字段同样未钉定 —— 状态按常见同义词收敛(murekaMusicStatus),
//		产物地址逐候选字段尝试(murekaTranslateQuery)。
//
// 错误体形状未实证(非 OpenAI 形的可能性存在):ErrorSummary 沿用
// openAIAdaptor 的「取 error.message、否则原文截断」宽解析,真 key 后按
// 实证形状再定是否照 dashscope 覆写。聊天/生图/视频/音频四方法与音效是
// 显式不支持桩:mureka 渠道只声明 music 能力,调度过滤下到不了这些方法。
package relay

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// ── 常量面:路径与探测共用(channel.MurekaQueryPathPrefix),其余只归
//
//	本文件;每个未钉定值都标了真 key 验证点。──
const (
	// [官方] region-edit 提交路径。
	murekaRegionEditPath = "/v1/song/region-edit"
	// [官方] 参考音频上传路径(multipart,见文件头注释块)。
	murekaUploadPath = "/v1/files/upload"
	// murekaUploadPurpose 是上传的 purpose 取值:[官方] 枚举里没有单列
	// 「edit」,region-edit 基底取哪个未钉定(候选 audio 通用音频 10MB /
	// remix 整曲 10–480s),先取通用 audio —— 真 key 验证点。
	murekaUploadPurpose = "audio"
	// murekaUploadFilename 是 multipart 的 file 字段文件名(上游按扩展名
	// 认容器的可能性存在;实际格式以参考地址的产物为准,mp3 为主)。
	murekaUploadFilename = "reference.mp3"
	// murekaRefDownloadBudget bounds the download of the resolved reference
	// address inside an independent budget —— 大音频(≤10MB 官方上限,但
	// 网关不预限参考体积)下载可能远超 upstreamTimeout;自设预算先例是
	// vod sfx 的 3min 独立预算(vod.go:862-868)。上传与提交两跳沿用
	// upstreamTimeout。
	murekaRefDownloadBudget = 3 * time.Minute
)

// ── 请求级 sentinel(79 号票;文案即客户端文案,handler 侧
//
//	run.requestRefused = 400 invalid_request_error、预扣原退、不换道、
//	不记熔断账、0 usage 行)。──
var (
	// errMurekaEditRequiresLyrics:audio_ref 非空而歌词空 —— Edit 无自动
	// 写词,词是身份必需(79 号票定案 5)。
	errMurekaEditRequiresLyrics = errors.New("mureka-song-edit 仅受理同曲换词:歌词必填(Edit 无自动写词,词是身份必需)")
	// errMurekaEditRequiresAudioRef:audio_ref 为空的普通音乐请求 —— 参考
	// 是身份所在,普通音乐请走其他公开名。
	errMurekaEditRequiresAudioRef = errors.New("mureka-song-edit 仅受理同曲换词(参考音频是身份必需);普通音乐请用其他公开名")
	// errMurekaUploadFailed:上传链任何失败(下载/上传/嗅探时长)按请求级
	// 失败处理(79 号票定案 6:参考是身份所在,不静默降级;勘察笔记 10 §2
	// 的「上传失败改可换道」建议不采纳,以票面为准)。动态文案由
	// fmt.Errorf("%w: 环节: 原因") 携带,errors.Is 判 root。
	errMurekaUploadFailed = errors.New("mureka-song-edit 参考音频上传失败")
)

// murekaMusicBody is the gateway submit body's adaptor slice(model 已换
// 上游名;instrumental 不参与 Edit 语义 —— 歌词非空即人声,互斥校验在
// prepareMusic)。AudioRef 到这里恒是已解析的上游可拉取地址(prepareMusic
// 的 resolveAudioRef 改写过)。
type murekaMusicBody struct {
	Model    string `json:"model"`
	Prompt   string `json:"prompt"`
	Lyrics   string `json:"lyrics"`
	AudioRef string `json:"audio_ref"`
}

// murekaAdaptor implements Adaptor for mureka channels. Normalize*/聊天/
// 生图/视频/音频/音效方法全部继承 openAIAdaptor 并显式覆盖为不支持桩
// (该类型渠道只声明 music 能力,调度不会把别的请求落上来);ErrorSummary
// 沿用宽解析(错误体形状未实证,见文件头注释)。
type murekaAdaptor struct {
	openAIAdaptor
}

// newMurekaAdaptor returns the production adaptor — stateless apart from
// the pooled transport, one instance serves every channel of the type.
func newMurekaAdaptor() *murekaAdaptor {
	return &murekaAdaptor{openAIAdaptor: openAIAdaptor{Client: &http.Client{Transport: upstreamTransport}}}
}

// murekaErrorBody renders one OpenAI-shaped error body(vodErrorBody 同款
// 形状,只进 usage 摘要与客户端错误)。
func murekaErrorBody(message string) []byte {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{"message": message, "param": nil, "code": "upstream_error"},
	})
	return body
}

// murekaCall builds and executes one Mureka HTTP call (POST JSON / GET),
// Bearer-authed, body-capped like every upstream call(minimaxCall 同款)。
func (a *murekaAdaptor) murekaCall(ctx context.Context, ch channel.Channel, method, path string, payload []byte) (*UpstreamResponse, error) {
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
	req.Header.Set("Authorization", "Bearer "+ch.APIKey)

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
// independent download budget(murekaRefDownloadBudget;注释见常量)。
func (a *murekaAdaptor) downloadReference(ctx context.Context, ref string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, murekaRefDownloadBudget)
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("reference is empty")
	}
	return raw, nil
}

// uploadReference posts the reference bytes to /v1/files/upload
// (multipart/form-data,purpose + file)and returns the file id. 字段形状
// 见文件头注释块([官方];应答 id 字段名未渲染,解析从宽)。
func (a *murekaAdaptor) uploadReference(ctx context.Context, ch channel.Channel, audio []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("purpose", murekaUploadPurpose); err != nil {
		return "", err
	}
	part, err := w.CreateFormFile("file", murekaUploadFilename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.BaseURL+murekaUploadPath, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+ch.APIKey)

	resp, err := a.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("upload answered HTTP %d: %s", resp.StatusCode, a.ErrorSummary(raw))
	}
	var r struct {
		ID     string `json:"id"`
		FileID string `json:"file_id"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", errors.New("upload response is not a JSON object")
	}
	if id := strings.TrimSpace(r.ID); id != "" {
		return id, nil
	}
	if id := strings.TrimSpace(r.FileID); id != "" {
		return id, nil
	}
	return "", errors.New("upload response carries no file id")
}

// MusicSubmit runs the Edit 语义:形状 sentinel → 下载参考字节 → multipart
// 上传取文件 id → 嗅探全曲选区 → 提交 region-edit,归一 {"task_id"}。
func (a *murekaAdaptor) MusicSubmit(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	var req murekaMusicBody
	if err := json.Unmarshal(payload, &req); err != nil {
		// 契约体不可解析是请求侧问题:400 形拒绝,不换道(与 vod/minimax 同款)。
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: murekaErrorBody("mureka: request body is not a valid JSON object")}, nil
	}
	ref := strings.TrimSpace(req.AudioRef)
	// 两个形状 sentinel(见 var 块注释):先判参考再判词 —— 没有 reference
	// 的请求根本不是同曲换词,文案要指路普通音乐公开名。
	if ref == "" {
		return nil, errMurekaEditRequiresAudioRef
	}
	lyrics := strings.TrimSpace(req.Lyrics)
	if lyrics == "" {
		return nil, errMurekaEditRequiresLyrics
	}

	// 上传链:下载 → 上传 → 嗅探选区;任何一环失败都按请求级失败处理
	// (errMurekaUploadFailed 包裹,见 var 块注释)。
	audio, err := a.downloadReference(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("%w: 下载参考音频失败: %v", errMurekaUploadFailed, err)
	}
	fileID, err := a.uploadReference(ctx, ch, audio)
	if err != nil {
		return nil, fmt.Errorf("%w: 上传参考音频失败: %v", errMurekaUploadFailed, err)
	}
	startMS, endMS, err := murekaEditSelection(audio)
	if err != nil {
		return nil, fmt.Errorf("%w: 嗅探参考音频时长失败: %v", errMurekaUploadFailed, err)
	}

	body := map[string]any{
		"audio_id":   fileID,
		"lyrics":     lyrics,
		"edit_start": startMS,
		"edit_end":   endMS,
	}
	// prompt 选填(79 号票定案 5):非空透传(2000 上限网关已管),空则
	// 不带该字段;payload.Model 已是上游映射名,但 Edit 端点未证实收模型
	// 档字段(useapi 兼容层实证拒 model 参数),不透传 —— 真 key 后若官方
	// 收档名在此加键。
	if p := strings.TrimSpace(req.Prompt); p != "" {
		body["prompt"] = p
	}
	translated, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	upstream, err := a.murekaCall(ctx, ch, http.MethodPost, murekaRegionEditPath, translated)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	var r struct {
		TaskID string `json:"task_id"`
		ID     string `json:"id"`
	}
	_ = json.Unmarshal(upstream.Body, &r)
	taskID := strings.TrimSpace(r.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(r.ID)
	}
	if taskID == "" {
		// 2xx 无任务 id 的防御:合成 502 形,与上游失败同路可换道不建行
		// (minimax 同款)。
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: murekaErrorBody("mureka: 2xx submit carries no task id")}, nil
	}
	renamed, _ := json.Marshal(map[string]string{"task_id": taskID})
	return &UpstreamResponse{Status: upstream.Status, Body: renamed, OK: true}, nil
}

// murekaEditSelection pins the whole-song edit region(79 号票定案 3:一次
// 画布提交 = 一次全曲 Edit):edit_start=0,edit_end=嗅探的参考音频时长
// (毫秒)。fal 托管镜像标 edit_start/edit_end 必填(毫秒),官方是否允许
// 省略 edit_end 未钉定 —— 真 key 后若可省略,改这里省略即可(独立小函数
// 便于改)。嗅探误差即编辑区误差:mp3 按逐帧计(时长与帧码率无关,精确),
// m4a 按 mvhd box 精确读;解不出的音频按上传链 sentinel 文案报(真 key
// 验证点:官方若给参考时长查询面,可改走查询免嗅探)。
func murekaEditSelection(audio []byte) (int64, int64, error) {
	endMS, ok := sniffAudioDurationMS(audio)
	if !ok {
		return 0, 0, errors.New("unrecognized audio bytes (mp3/m4a)")
	}
	return 0, endMS, nil
}

// sniffAudioDurationMS best-effort reads one audio blob's duration in
// milliseconds: mp3 走逐帧计,mp4/m4a 走 mvhd;两种都解不出 ok=false ——
// stdlib 无音频解码,这里只认形状,不引入新依赖(79 号票纪律)。
func sniffAudioDurationMS(b []byte) (int64, bool) {
	if ms, ok := mp3DurationMS(b); ok {
		return ms, true
	}
	if ms, ok := mp4DurationMS(b); ok {
		return ms, true
	}
	return 0, false
}

// mp3DurationMS walks MPEG Layer III frames and counts samples —— 每帧样本
// 数对 CBR/VBR 恒定(1152@MPEG1 / 576@MPEG2、2.5),时长与帧码率无关;
// padding 逐帧判。先剥 ID3v2 头,再锚首帧,后续帧任一解不动即放弃(伪
// 同步/脏数据不猜)。返回整毫秒(整帧精度)。
func mp3DurationMS(b []byte) (int64, bool) {
	i := 0
	if len(b) >= 10 && string(b[:3]) == "ID3" {
		// ID3v2 头:10 字节 + 同步安全(synchsafe)标签长度。
		tag := int64(b[6]&0x7f)<<21 | int64(b[7]&0x7f)<<14 | int64(b[8]&0x7f)<<7 | int64(b[9]&0x7f)
		i = 10 + int(tag)
		if i < 0 || i+4 > len(b) {
			return 0, false
		}
	}
	// 锚首帧帧头(有 ID3 时它在标签后不远;无标签时从头找)。只认
	// 0xFF Ex 同步 + Layer III。
	for i+4 <= len(b) && !(b[i] == 0xFF && b[i+1]&0xE0 == 0xE0 && b[i+1]&0x06 == 0x02) {
		i++
	}
	if i+4 > len(b) {
		return 0, false
	}
	samplesPerFrame, bitrates, sampleRates, ok := mp3FrameTables(b[i], b[i+1])
	if !ok {
		return 0, false
	}
	sampleRate := sampleRates[(b[i+2]>>2)&0x03]
	if sampleRate == 0 || bitrates[b[i+2]>>4] == 0 {
		return 0, false
	}
	frames := int64(0)
	for i+4 <= len(b) {
		if b[i] != 0xFF || b[i+1]&0xE0 != 0xE0 || b[i+1]&0x06 != 0x02 {
			return 0, false // 非同步字节或非 Layer III:流不干净,不猜
		}
		spf, bitrates, rates, ok := mp3FrameTables(b[i], b[i+1])
		if !ok || spf != samplesPerFrame {
			return 0, false // 版本漂移:流不干净,不猜
		}
		br := bitrates[b[i+2]>>4]
		sr := rates[(b[i+2]>>2)&0x03]
		if br == 0 || sr != sampleRate {
			return 0, false // 采样率漂移或坏码率:不猜
		}
		pad := int64((b[i+2] >> 1) & 0x01)
		size := int64(spf/8)*int64(br)*1000/int64(sr) + pad
		if size < 24 {
			return 0, false
		}
		frames++
		i += int(size)
	}
	if frames == 0 {
		return 0, false
	}
	return frames * int64(samplesPerFrame) * 1000 / int64(sampleRate), true
}

// mp3FrameTables resolves (samplesPerFrame, bitrate kbps 表, samplerate 表)
// from one frame header's version/layer bits;只认 Layer III,版本
// 00=MPEG2.5 / 10=MPEG2 / 11=MPEG1(01 保留)。
func mp3FrameTables(_ byte, second byte) (int, [16]int, [4]int, bool) {
	version := (second >> 3) & 0x03
	switch version {
	case 3: // MPEG1 Layer III
		return 1152,
			[16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
			[4]int{44100, 48000, 32000, 0}, true
	case 2, 0: // MPEG2 / MPEG2.5 Layer III
		rates := [4]int{22050, 24000, 16000, 0} // MPEG2
		if version == 0 {
			rates = [4]int{11025, 12000, 8000, 0} // MPEG2.5
		}
		return 576,
			[16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
			rates, true
	default:
		return 0, [16]int{}, [4]int{}, false
	}
}

// mp4DurationMS reads the mvhd box's timescale+duration(mp4/m4a 容器;
// mvhd 常在前部,扫描窗口 1MB 防御整曲大文件)。
func mp4DurationMS(b []byte) (int64, bool) {
	limit := len(b)
	if limit > 1<<20 {
		limit = 1 << 20
	}
	idx := bytes.Index(b[:limit], []byte("mvhd"))
	if idx < 0 {
		return 0, false
	}
	p := idx + 4 // version(1) + flags(3) 从这里起
	version := b[p]
	switch version {
	case 1:
		if p+32 > len(b) {
			return 0, false
		}
		timescale := int64(binary.BigEndian.Uint32(b[p+20 : p+24]))
		duration := int64(binary.BigEndian.Uint64(b[p+24 : p+32]))
		if timescale <= 0 {
			return 0, false
		}
		return duration * 1000 / timescale, true
	default: // version 0
		if p+20 > len(b) {
			return 0, false
		}
		timescale := int64(binary.BigEndian.Uint32(b[p+12 : p+16]))
		duration := int64(binary.BigEndian.Uint32(b[p+16 : p+20]))
		if timescale <= 0 {
			return 0, false
		}
		return duration * 1000 / timescale, true
	}
}

// murekaMusicStatus translates one Mureka raw status word onto the gateway
// 词表(queued/running/succeeded/failed/canceled 族,MergeStatus 认得的
// 词):官方状态枚举未钉定(票 79 验证点 3),先按常见异步任务同义词收敛
// —— 防在途词被 MergeStatus 的 default 规则误杀成 failed(62 号票 VOD 的
// WAITING 曾因直透被误杀,vodMusicStatus 翻译层同因)。未知词原样透出,
// 由词表 default 兜底;真 key 后按官方枚举回填词表。
func murekaMusicStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "created", "submitted", "pending", "waiting", "queued", "in_queue":
		return "queued"
	case "generating", "in_progress", "processing", "running", "streaming":
		return "running"
	case "succeeded", "success", "complete", "completed", "finished", "done", "ok":
		return "succeeded"
	case "failed", "error", "rejected":
		return "failed"
	case "canceled", "cancelled":
		return "canceled"
	default:
		return status
	}
}

// murekaTranslateQuery normalizes one Mureka task body into the gateway's
// internal task face so the relay's parseMusicQuery keeps a single contract.
// 字段名未钉定(见文件头注释块):状态尽力取 status/task_status,产物地址
// 逐候选字段尝试(嵌套 audio.url → 扁平 audio_url → files[0].url),失败
// 原因尽力取 error.message/fail_reason。A body without any known shape is
// a transient upstream fault — the caller must not advance the task on it.
func murekaTranslateQuery(body []byte) ([]byte, error) {
	var r struct {
		Status     string `json:"status"`
		TaskStatus string `json:"task_status"`
		Audio      *struct {
			URL string `json:"url"`
		} `json:"audio"`
		AudioURL string `json:"audio_url"`
		Files    []struct {
			URL string `json:"url"`
		} `json:"files"`
		FailReason string `json:"fail_reason"`
		Error      *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("upstream task body is not a JSON object: %w", err)
	}
	status := strings.TrimSpace(r.Status)
	if status == "" {
		status = strings.TrimSpace(r.TaskStatus)
	}
	if status == "" {
		return nil, errors.New("upstream task body carries no status")
	}
	audioURL := ""
	if r.Audio != nil {
		audioURL = strings.TrimSpace(r.Audio.URL)
	}
	if audioURL == "" {
		audioURL = strings.TrimSpace(r.AudioURL)
	}
	if audioURL == "" {
		for _, f := range r.Files {
			if u := strings.TrimSpace(f.URL); u != "" {
				audioURL = u
				break
			}
		}
	}
	failMsg := ""
	if r.Error != nil {
		failMsg = strings.TrimSpace(r.Error.Message)
	}
	if failMsg == "" {
		failMsg = strings.TrimSpace(r.FailReason)
	}
	out := map[string]any{
		"task_status": murekaMusicStatus(status),
	}
	if audioURL != "" {
		out["audio_url"] = audioURL
	}
	if failMsg != "" {
		out["error"] = map[string]string{"message": failMsg}
	}
	return json.Marshal(out)
}

// MusicQuery polls one Mureka task. The vendor body is normalized into the
// gateway's internal task face (murekaTranslateQuery); only then does the
// relay's merge-and-settle machinery see it.
func (a *murekaAdaptor) MusicQuery(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	upstream, err := a.murekaCall(ctx, ch, http.MethodGet,
		channel.MurekaQueryPathPrefix+url.PathEscape(upstreamTaskID), nil)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	normalized, err := murekaTranslateQuery(upstream.Body)
	if err != nil {
		// 查询响应不可解析 = 暂态上游故障:502 形归一,轮询不改状态不动账。
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: murekaErrorBody("mureka: " + err.Error())}, nil
	}
	return &UpstreamResponse{Status: upstream.Status, Body: normalized, OK: true}, nil
}

// 聊天/生图/视频/音频/音效面不归这个 adaptor:mureka 渠道只声明 music
// 能力,这些方法只作防御 —— 返回错误让 relay 按上游失败处理(调度过滤下
// 到不了这里);将来接 Mureka 对话外能力时把桩换成透传即可(minimax 同款
// 形状)。
func (a *murekaAdaptor) ChatCompletions(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: mureka adaptor does not relay chat completions")
}

func (a *murekaAdaptor) ChatCompletionsStream(ctx context.Context, ch channel.Channel, publicModel string, payload []byte) (*UpstreamStream, error) {
	return nil, errors.New("relay: mureka adaptor does not relay chat completions")
}

func (a *murekaAdaptor) ImagesGenerations(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: mureka adaptor does not relay images")
}

func (a *murekaAdaptor) ImagesEdits(ctx context.Context, ch channel.Channel, contentType string, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: mureka adaptor does not relay images")
}

func (a *murekaAdaptor) VideosSubmit(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: mureka adaptor does not relay videos")
}

func (a *murekaAdaptor) VideosQuery(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	return nil, errors.New("relay: mureka adaptor does not relay videos")
}

func (a *murekaAdaptor) VideosCancel(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	return nil, errors.New("relay: mureka adaptor does not relay videos")
}

func (a *murekaAdaptor) VoicesCreate(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: mureka adaptor does not relay audio voices")
}

func (a *murekaAdaptor) VoicesQuery(ctx context.Context, ch channel.Channel, upstreamModel, voiceID string) (*UpstreamResponse, error) {
	return nil, errors.New("relay: mureka adaptor does not relay audio voices")
}

func (a *murekaAdaptor) VoicesDelete(ctx context.Context, ch channel.Channel, upstreamModel, voiceID string) (*UpstreamResponse, error) {
	return nil, errors.New("relay: mureka adaptor does not relay audio voices")
}

func (a *murekaAdaptor) SpeechSynthesis(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: mureka adaptor does not relay speech synthesis")
}

func (a *murekaAdaptor) SfxSynthesis(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: mureka adaptor does not relay sfx synthesis")
}

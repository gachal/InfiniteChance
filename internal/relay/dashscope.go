// dashscope adaptor(46 号票):阿里云百炼的 DashScope 统一异步视频契约
// ——百炼上架的第三方视频模型(MiniMax-H3、PixVerse、万相…)共用这一张
// 任务面。也不是 OpenAI 兼容面:Bearer 鉴权、提交必带 X-DashScope-Async
// 头(POST {base}/api/v1/services/aigc/video-generation/video-synthesis,
// 体是 model / input{prompt,media[]}/ parameters 三段分离,media 条目是
// 扁平 {type,url}:first_frame/last_frame/image_url/feature/driving_audio)、
// 轮询 GET {base}/api/v1/tasks/{id}(output.task_status 大写枚举,产物
// output.video_url,失败原因 output.code/message)。与 Ark/minimax 的分节
// 形状不同构,不复用其渲染;但状态词汇走同一个五态机 —— MergeStatus 本就
// 大小写不敏感(PENDING/RUNNING/SUCCEEDED/CANCELED 天然归并),UNKNOWN
// (任务不存在或超 24h)落未知态归 failed,语义正确。DashScope 没有公开
// 的任务取消 API,取消方法返回合成 2xx:网关本地取消照常兑付(退全款),
// 厂商侧任务自然耗尽。本文件把翻译关在 adaptor 内:提交体从网关视频契约
// 重组成 DashScope 形状,查询响应归一成网关任务面契约,relay 骨架零改动。
// 聊天/生图方法是显式不支持桩:MVP 该类型渠道只声明 videos 能力,调度过
// 滤下到不了;将来接百炼同面模型时把桩换成透传即可。
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// dashscopeAdaptor implements Adaptor for dashscope channels. Normalize* 复用
// openAIAdaptor 基类(本 adaptor 不服务同步面,Normalize 到不了);
// ErrorSummary 覆写:DashScope 错误体是顶层 {"code","message"}(提交 4xx)
// 或 {"output":{"code","message"}}(任务失败),不是 OpenAI error object 形。
type dashscopeAdaptor struct {
	openAIAdaptor
}

// newDashScopeAdaptor returns the production adaptor — stateless apart from
// the pooled transport, one instance serves every channel of the type.
func newDashScopeAdaptor() *dashscopeAdaptor {
	return &dashscopeAdaptor{openAIAdaptor: openAIAdaptor{Client: &http.Client{Transport: upstreamTransport}}}
}

// dashscopeSubmitPath / dashscopeQueryPrefix are the versioned task-face
// paths appended to the channel's BaseURL (工作空间域名根,不含路径).
const (
	dashscopeSubmitPath  = "/api/v1/services/aigc/video-generation/video-synthesis"
	dashscopeQueryPrefix = "/api/v1/tasks/"
)

// dashscopeReferenceType maps one gateway reference kind onto its DashScope
// media type(官方枚举:参考图 image_url、参考视频 feature、参考音频
// driving_audio;首/尾帧不从这里走,由提交体字段直译)。
func dashscopeReferenceType(kind string) string {
	switch strings.TrimSpace(kind) {
	case refKindVideo:
		return "feature"
	case refKindAudio:
		return "driving_audio"
	default:
		return "image_url"
	}
}

// dashscopeTranslateSubmit rebuilds the gateway's video body (videoRequest,
// 同包契约切片;model 已换上游名,校验都在 prepareVideo)as one DashScope
// submit body:prompt 进 input.prompt(必填,prepareVideo 已强制非空);
// image/last_image 进 media 的 first_frame/last_frame 条目;references 按
// 各自 kind 进 image_url/feature/driving_audio 条目(扁平 {type,url});
// seconds → parameters.duration(官方必填,缺省补 defaultVideoSeconds 与
// 计费预扣同值,账实一致);size → parameters.resolution(官方必选
// 768P/2K,不传交上游 400)、ratio → parameters.ratio(档位串直传不校验
// 枚举);watermark 不传 = 官方默认 false。图生视频与多模态参考互斥、
// 文生视频 ratio 必填且不能 adaptive 等上游约束由阿里云裁决,4xx 透回。
func dashscopeTranslateSubmit(payload []byte) ([]byte, error) {
	var req videoRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("request body is not a valid JSON object: %w", err)
	}
	media := make([]map[string]string, 0, 2+len(req.References))
	if u := strings.TrimSpace(req.Image); u != "" {
		media = append(media, map[string]string{"type": "first_frame", "url": u})
	}
	if u := strings.TrimSpace(req.LastImage); u != "" {
		media = append(media, map[string]string{"type": "last_frame", "url": u})
	}
	for _, r := range req.References {
		media = append(media, map[string]string{"type": dashscopeReferenceType(r.Kind), "url": r.URL})
	}
	input := map[string]any{}
	if p := strings.TrimSpace(req.Prompt); p != "" {
		input["prompt"] = p
	}
	if len(media) > 0 {
		input["media"] = media
	}
	duration := defaultVideoSeconds
	if req.Seconds != nil {
		duration = *req.Seconds
	}
	parameters := map[string]any{"duration": duration}
	if s := strings.TrimSpace(req.Size); s != "" {
		parameters["resolution"] = s
	}
	if r := strings.TrimSpace(req.Ratio); r != "" {
		parameters["ratio"] = r
	}
	return json.Marshal(map[string]any{
		"model":      req.Model,
		"input":      input,
		"parameters": parameters,
	})
}

// dashscopeTranslateQuery normalizes one DashScope task body into the
// gateway's internal task face so the relay's parseVideoQuery keeps a single
// contract: output.task_status 原样透传(五态机大小写不敏感地归并,UNKNOWN
// 走未知态归 failed)、output.video_url → video_url、output.code/message
// 兜底取 message → error.message;DashScope 的 usage 没有 completion_tokens,
// 不随行(second 轨结算用不到)。A body without an output object or status
// is a transient upstream fault — the caller must not advance the task on it.
func dashscopeTranslateQuery(body []byte) ([]byte, error) {
	var r struct {
		Output *struct {
			TaskID     string `json:"task_id"`
			TaskStatus string `json:"task_status"`
			VideoURL   string `json:"video_url"`
			Code       string `json:"code"`
			Message    string `json:"message"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("upstream task body is not a JSON object: %w", err)
	}
	if r.Output == nil || strings.TrimSpace(r.Output.TaskStatus) == "" {
		return nil, errors.New("dashscope task body carries no output.task_status")
	}
	out := map[string]any{
		"id":          r.Output.TaskID,
		"task_status": strings.TrimSpace(r.Output.TaskStatus),
	}
	if u := strings.TrimSpace(r.Output.VideoURL); u != "" {
		out["video_url"] = u
	}
	msg := strings.TrimSpace(r.Output.Message)
	if msg == "" {
		msg = strings.TrimSpace(r.Output.Code)
	}
	if msg != "" {
		out["error"] = map[string]string{"message": msg}
	}
	return json.Marshal(out)
}

// dashscopeCall builds and executes one DashScope HTTP call, Bearer-authed,
// body-capped like every upstream call; async marks the submit's required
// X-DashScope-Async header(缺了官方报 "current user api does not support
// synchronous calls").
func (a *dashscopeAdaptor) dashscopeCall(ctx context.Context, ch channel.Channel, method, path string, payload []byte, async bool) (*UpstreamResponse, error) {
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
	if async {
		req.Header.Set("X-DashScope-Async", "enable")
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

// VideosSubmit translates and submits one DashScope task. A 2xx answer
// carries output.task_id — re-rendered as {"task_id": …} so the relay's
// parseVideoSubmit keeps a single contract; an empty id is still rejected
// defensively(与 ark/minimax 同款 502 形,不换道不建行).
func (a *dashscopeAdaptor) VideosSubmit(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	translated, err := dashscopeTranslateSubmit(payload)
	if err != nil {
		// 契约体不可解析是请求侧问题:400 形拒绝,不换道(与 vod/ark 同款)。
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: vodErrorBody("dashscope: " + err.Error())}, nil
	}
	upstream, err := a.dashscopeCall(ctx, ch, http.MethodPost, dashscopeSubmitPath, translated, true)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	var r struct {
		Output *struct {
			TaskID string `json:"task_id"`
		} `json:"output"`
	}
	if err := json.Unmarshal(upstream.Body, &r); err != nil || r.Output == nil ||
		strings.TrimSpace(r.Output.TaskID) == "" {
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: vodErrorBody("dashscope: 2xx submit carries no task id")}, nil
	}
	renamed, _ := json.Marshal(map[string]string{"task_id": strings.TrimSpace(r.Output.TaskID)})
	return &UpstreamResponse{Status: upstream.Status, Body: renamed, OK: true}, nil
}

// VideosQuery polls one DashScope task. The vendor body is normalized into
// the gateway's internal task face (dashscopeTranslateQuery); only then does
// the relay's merge-and-settle machinery see it.
func (a *dashscopeAdaptor) VideosQuery(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	upstream, err := a.dashscopeCall(ctx, ch, http.MethodGet, dashscopeQueryPrefix+url.PathEscape(upstreamTaskID), nil, false)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	normalized, err := dashscopeTranslateQuery(upstream.Body)
	if err != nil {
		// 查询响应不可解析 = 暂态上游故障:502 形归一,轮询不改状态不动账。
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: vodErrorBody("dashscope: " + err.Error())}, nil
	}
	return &UpstreamResponse{Status: upstream.Status, Body: normalized, OK: true}, nil
}

// VideosCancel is a deliberate no-op: DashScope 没有公开的任务取消 API
// (46 号票),厂商侧任务自然耗尽。合成 2xx 让 relay 的「非 2xx 只进日志、
// 本地取消照常」路径静默通过 —— 对外取消不扣费的语义由网关兑付,与
// ark/minimax 的厂商侧结果只进日志同一纪律。
func (a *dashscopeAdaptor) VideosCancel(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	return &UpstreamResponse{Status: http.StatusOK, OK: true,
		Body: []byte(`{"note":"dashscope: no public task cancel API; cancelled locally only"}`)}, nil
}

// ErrorSummary 提取 DashScope 形错误:任务失败的 output.{code,message}、
// 提交 4xx 的顶层 {code,message},都兜底取 code;两者皆非才落基类
// (OpenAI 形 → 截原文)。
func (a *dashscopeAdaptor) ErrorSummary(body []byte) string {
	var e struct {
		Output *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"output"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &e); err == nil {
		if e.Output != nil && (strings.TrimSpace(e.Output.Message) != "" || strings.TrimSpace(e.Output.Code) != "") {
			msg := strings.TrimSpace(e.Output.Message)
			if msg == "" {
				msg = e.Output.Code
			}
			return truncate(msg, 512)
		}
		if strings.TrimSpace(e.Message) != "" || strings.TrimSpace(e.Code) != "" {
			msg := strings.TrimSpace(e.Message)
			if msg == "" {
				msg = e.Code
			}
			return truncate(msg, 512)
		}
	}
	return a.openAIAdaptor.ErrorSummary(body)
}

// 聊天/生图面不归这个 adaptor:MVP 仅 videos 能力,这些方法只作防御 ——
// 返回错误让 relay 按上游失败处理(调度过滤下到不了这里);接百炼同面
// 模型时在 adaptor 内把桩换成透传即可。
func (a *dashscopeAdaptor) ChatCompletions(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: dashscope adaptor does not relay chat completions")
}

func (a *dashscopeAdaptor) ChatCompletionsStream(ctx context.Context, ch channel.Channel, publicModel string, payload []byte) (*UpstreamStream, error) {
	return nil, errors.New("relay: dashscope adaptor does not relay chat completions")
}

func (a *dashscopeAdaptor) ImagesGenerations(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: dashscope adaptor does not relay images")
}

func (a *dashscopeAdaptor) ImagesEdits(ctx context.Context, ch channel.Channel, contentType string, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: dashscope adaptor does not relay images")
}

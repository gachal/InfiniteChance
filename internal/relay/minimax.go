// minimax adaptor(45 号票):MiniMax 原生视频 API(MiniMax-H3 / H3-Max)
// 也不是 OpenAI 兼容面 —— Bearer 鉴权、异步任务(POST {base}/v2/
// video_generation 提交、GET {base}/v2/query/video_generation/{id} 轮询、
// DELETE {base}/v2/video_generation/{id} 取消)。与 Ark(ark.go)的分节形状
// 几乎同构:content[] 承载 text 与带 role 的 image_url/video_url/audio_url
// 分节(角色枚举 first_frame/last_frame/reference_* 与网关契约同款),顶层
// resolution/duration/ratio;差别在查询响应包一层 {"task":{…}}、产物在
// content.url(不是 Ark 的 content.video_url)。本文件把翻译关在 adaptor
// 内:提交体从网关视频契约重组成 MiniMax 形状,查询响应归一成网关任务面
// 契约(task_status/video_url/error/usage.completion_tokens),relay 骨架零
// 改动。聊天/生图方法是显式不支持桩:MVP 该类型渠道只声明 videos 能力,
// 调度过滤下到不了;将来接 MiniMax 对话/生图时把桩换成透传即可。
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

// minimaxAdaptor implements Adaptor for minimax channels. ErrorSummary 与
// Normalize* 复用 openAIAdaptor(MiniMax 错误体是 OpenAI 形;本 adaptor
// 不服务同步面,Normalize 到不了)。
type minimaxAdaptor struct {
	openAIAdaptor
}

// newMiniMaxAdaptor returns the production adaptor — stateless apart from
// the pooled transport, one instance serves every channel of the type.
func newMiniMaxAdaptor() *minimaxAdaptor {
	return &minimaxAdaptor{openAIAdaptor: openAIAdaptor{Client: &http.Client{Transport: upstreamTransport}}}
}

// minimaxTranslateSubmit rebuilds the gateway's video body (videoRequest,
// 同包契约切片;model 已换上游名,校验都在 prepareVideo)as one MiniMax
// submit body: seconds → duration(缺省补 defaultVideoSeconds —— MiniMax
// 必填该字段,而网关的 seconds 缺省只进计费与任务行、不改写透传的请求
// 体)、size → resolution、ratio → ratio(档位串数值直传不校验枚举,官方
// 档 480P/768P/2K 以官方串为键,不归一);prompt 进 text 分节(MiniMax
// 要求 content 含非空 text,网关 prepareVideo 已强制 prompt 非空);
// image/last_image 进首尾帧分节;references 按各自 kind 进参考分节 ——
// 分节与角色枚举与 Ark 同构,复用 arkMediaPart。文生视频 ratio 必填且
// 不能 adaptive 等上游约束由 MiniMax 裁决,4xx 透回。
func minimaxTranslateSubmit(payload []byte) ([]byte, error) {
	var req videoRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("request body is not a valid JSON object: %w", err)
	}
	content := make([]map[string]any, 0, 2+len(req.References))
	if p := strings.TrimSpace(req.Prompt); p != "" {
		content = append(content, map[string]any{"type": "text", "text": p})
	}
	if u := strings.TrimSpace(req.Image); u != "" {
		content = append(content, arkMediaPart("image_url", "first_frame", u))
	}
	if u := strings.TrimSpace(req.LastImage); u != "" {
		content = append(content, arkMediaPart("image_url", "last_frame", u))
	}
	for _, r := range req.References {
		typ, role := arkReferenceRole(r.Kind)
		content = append(content, arkMediaPart(typ, role, r.URL))
	}
	body := map[string]any{
		"model":   req.Model,
		"content": content,
	}
	if req.Seconds != nil {
		body["duration"] = *req.Seconds
	} else {
		// 「自动时长」(不传 seconds)在 adaptor 补齐缺省秒数:预扣已按
		// 同值计费(prepareVideo 的 defaultVideoSeconds),账实一致。
		body["duration"] = defaultVideoSeconds
	}
	if s := strings.TrimSpace(req.Size); s != "" {
		body["resolution"] = s
	}
	if r := strings.TrimSpace(req.Ratio); r != "" {
		body["ratio"] = r
	}
	return json.Marshal(body)
}

// minimaxTranslateQuery normalizes one MiniMax task body into the gateway's
// internal task face so the relay's parseVideoQuery keeps a single contract.
// MiniMax 的查询响应比 Ark 多包一层 {"task":{…}},产物在 content.url;
// status 枚举 queued/running/succeeded/failed/cancelled(与五态机词汇对齐,
// cancelled 双 L 由 MergeStatus 一并归并);usage.completion_tokens 随行
// (second 轨结算用不到,归一面契约照带)。A body that does not parse is a
// transient upstream fault — the caller must not advance the task on it.
func minimaxTranslateQuery(body []byte) ([]byte, error) {
	var r struct {
		Task struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Content *struct {
				URL string `json:"url"`
			} `json:"content"`
			Error *struct {
				Code    string `json:"code"`
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
			Usage *struct {
				CompletionTokens int64 `json:"completion_tokens"`
			} `json:"usage"`
		} `json:"task"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("upstream task body is not a JSON object: %w", err)
	}
	out := map[string]any{
		"id":          r.Task.ID,
		"task_status": r.Task.Status,
	}
	if r.Task.Content != nil {
		out["video_url"] = strings.TrimSpace(r.Task.Content.URL)
	}
	if r.Task.Error != nil {
		msg := r.Task.Error.Message
		if strings.TrimSpace(msg) == "" {
			msg = r.Task.Error.Code
		}
		if strings.TrimSpace(msg) == "" {
			msg = r.Task.Error.Type
		}
		out["error"] = map[string]string{"message": msg}
	}
	if r.Task.Usage != nil {
		out["usage"] = map[string]int64{"completion_tokens": r.Task.Usage.CompletionTokens}
	}
	return json.Marshal(out)
}

// minimaxCall builds and executes one MiniMax HTTP call (POST JSON / GET /
// DELETE), Bearer-authed, body-capped like every upstream call.
func (a *minimaxAdaptor) minimaxCall(ctx context.Context, ch channel.Channel, method, path string, payload []byte) (*UpstreamResponse, error) {
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

// VideosSubmit translates and submits one MiniMax task. A 2xx answer already
// carries {"task_id": …} in the gateway's face; an empty id is still rejected
// defensively (与 Ark 同款 502 形,不换道不建行).
func (a *minimaxAdaptor) VideosSubmit(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	translated, err := minimaxTranslateSubmit(payload)
	if err != nil {
		// 契约体不可解析是请求侧问题:400 形拒绝,不换道(与 vod/ark 同款)。
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: vodErrorBody("minimax: " + err.Error())}, nil
	}
	upstream, err := a.minimaxCall(ctx, ch, http.MethodPost, "/v2/video_generation", translated)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	var r struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(upstream.Body, &r); err != nil || strings.TrimSpace(r.TaskID) == "" {
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: vodErrorBody("minimax: 2xx submit carries no task id")}, nil
	}
	renamed, _ := json.Marshal(map[string]string{"task_id": strings.TrimSpace(r.TaskID)})
	return &UpstreamResponse{Status: upstream.Status, Body: renamed, OK: true}, nil
}

// VideosQuery polls one MiniMax task. The vendor body is normalized into the
// gateway's internal task face (minimaxTranslateQuery); only then does the
// relay's merge-and-settle machinery see it.
func (a *minimaxAdaptor) VideosQuery(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	upstream, err := a.minimaxCall(ctx, ch, http.MethodGet, "/v2/query/video_generation/"+url.PathEscape(upstreamTaskID), nil)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	normalized, err := minimaxTranslateQuery(upstream.Body)
	if err != nil {
		// 查询响应不可解析 = 暂态上游故障:502 形归一,轮询不改状态不动账。
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: vodErrorBody("minimax: " + err.Error())}, nil
	}
	return &UpstreamResponse{Status: upstream.Status, Body: normalized, OK: true}, nil
}

// VideosCancel asks MiniMax to stop one task: DELETE /v2/video_generation/
// {id} — queued 任务取消(不收费)、succeeded/failed 删除记录;running 不可
// 取消。非 2xx 不致命:网关照常本地取消不扣费,厂商侧结果只进日志(语义由
// relay 兑付,与 Ark 同款)。
func (a *minimaxAdaptor) VideosCancel(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	return a.minimaxCall(ctx, ch, http.MethodDelete, "/v2/video_generation/"+url.PathEscape(upstreamTaskID), nil)
}

// 聊天/生图面不归这个 adaptor:MVP 仅 videos 能力,这些方法只作防御 ——
// 返回错误让 relay 按上游失败处理(调度过滤下到不了这里);接 MiniMax
// 对话/生图时在 adaptor 内把桩换成透传即可。
func (a *minimaxAdaptor) ChatCompletions(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: minimax adaptor does not relay chat completions")
}

func (a *minimaxAdaptor) ChatCompletionsStream(ctx context.Context, ch channel.Channel, publicModel string, payload []byte) (*UpstreamStream, error) {
	return nil, errors.New("relay: minimax adaptor does not relay chat completions")
}

func (a *minimaxAdaptor) ImagesGenerations(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: minimax adaptor does not relay images")
}

func (a *minimaxAdaptor) ImagesEdits(ctx context.Context, ch channel.Channel, contentType string, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: minimax adaptor does not relay images")
}

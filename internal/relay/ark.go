// volcengine-ark adaptor(25 号票):火山方舟 Seedance 生视频不是 OpenAI
// 兼容面 —— Bearer 鉴权、异步任务(POST {base}/contents/generations/tasks
// 提交、GET …/tasks/{id} 轮询、DELETE …/tasks/{id} 取消或删除),参数在
// content 分节(text 承载提示词,image_url/video_url/audio_url 分节带
// role 区分首帧/尾帧/参考)与顶层 resolution/ratio/duration,按
// usage.completion_tokens 计费。本文件把翻译关在 adaptor 内:提交体从
// 网关视频契约重组成 Ark 形状,查询响应归一成网关任务面契约
// (task_status/video_url/error/usage.completion_tokens),relay 骨架零改
// 动。聊天/生图方法返回不支持错误:MVP 该类型渠道只声明 videos 能力,
// 调度不会把别的请求落上来;将来接豆包对话/Seedream 生图时把桩换成
// 透传即可。
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

// arkAdaptor implements Adaptor for volcengine-ark channels. ErrorSummary 与
// Normalize* 复用 openAIAdaptor(Ark 错误体是 OpenAI 形 {"error":{"code",
// "message"}};本 adaptor 不服务同步面,Normalize 到不了)。
type arkAdaptor struct {
	openAIAdaptor
}

// newArkAdaptor returns the production adaptor — stateless apart from the
// pooled transport, one instance serves every channel of the type.
func newArkAdaptor() *arkAdaptor {
	return &arkAdaptor{openAIAdaptor: openAIAdaptor{Client: &http.Client{Transport: upstreamTransport}}}
}

// arkMediaPart renders one Ark content 分节:媒体 URL 在以其 type 命名的
// 子对象里({"type":"image_url","role":"first_frame","image_url":{"url":…}})。
// role 由网关字段语义决定;档位/互斥枚举不校验,交上游裁(上游 4xx 透回)。
func arkMediaPart(typ, role, rawURL string) map[string]any {
	return map[string]any{
		"type": typ,
		"role": role,
		typ:    map[string]string{"url": strings.TrimSpace(rawURL)},
	}
}

// arkReferenceRole maps one gateway reference kind onto its Ark content
// 分节 type 与 role.
func arkReferenceRole(kind string) (typ, role string) {
	switch strings.TrimSpace(kind) {
	case refKindVideo:
		return "video_url", "reference_video"
	case refKindAudio:
		return "audio_url", "reference_audio"
	default:
		return "image_url", "reference_image"
	}
}

// arkTranslateSubmit rebuilds the gateway's video body (videoRequest,同包
// 契约切片;model 已换上游名,校验都在 prepareVideo)as one Ark submit
// body: seconds → duration、size → resolution、ratio → ratio(档位串数值
// 直传不校验枚举);prompt 进 text 分节;image/last_image 进首尾帧分节;
// references 按各自 kind 进参考分节。首帧/参考模式互斥等上游约束由
// Ark 裁决,4xx 透回。
func arkTranslateSubmit(payload []byte) ([]byte, error) {
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
	}
	if s := strings.TrimSpace(req.Size); s != "" {
		body["resolution"] = s
	}
	if r := strings.TrimSpace(req.Ratio); r != "" {
		body["ratio"] = r
	}
	return json.Marshal(body)
}

// arkTranslateQuery normalizes one Ark task body into the gateway's internal
// task face so the relay's parseVideoQuery keeps a single contract: status →
// task_status, content.video_url → video_url, error(code/message)→
// error.message, usage.completion_tokens rides along verbatim. A body that
// does not parse is a transient upstream fault — the caller must not advance
// the task on it.
func arkTranslateQuery(body []byte) ([]byte, error) {
	var r struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Content *struct {
			VideoURL string `json:"video_url"`
		} `json:"content"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Usage *struct {
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("upstream task body is not a JSON object: %w", err)
	}
	out := map[string]any{
		"id":          r.ID,
		"task_status": r.Status,
	}
	if r.Content != nil {
		out["video_url"] = strings.TrimSpace(r.Content.VideoURL)
	}
	if r.Error != nil {
		msg := r.Error.Message
		if strings.TrimSpace(msg) == "" {
			msg = r.Error.Code
		}
		out["error"] = map[string]string{"message": msg}
	}
	if r.Usage != nil {
		out["usage"] = map[string]int64{"completion_tokens": r.Usage.CompletionTokens}
	}
	return json.Marshal(out)
}

// arkCall builds and executes one Ark HTTP call (POST JSON / GET / DELETE),
// Bearer-authed, body-capped like every upstream call.
func (a *arkAdaptor) arkCall(ctx context.Context, ch channel.Channel, method, path string, payload []byte) (*UpstreamResponse, error) {
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

// VideosSubmit translates and submits one Ark task. A 2xx answer carries
// {"id": "cgt-…"} — it is re-rendered as {"task_id": …} so the relay's
// parseVideoSubmit keeps a single contract.
func (a *arkAdaptor) VideosSubmit(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	translated, err := arkTranslateSubmit(payload)
	if err != nil {
		// 契约体不可解析是请求侧问题:400 形拒绝,不换道(与 vod 同款)。
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: vodErrorBody("volcengine-ark: " + err.Error())}, nil
	}
	upstream, err := a.arkCall(ctx, ch, http.MethodPost, "/contents/generations/tasks", translated)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	var r struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(upstream.Body, &r); err != nil || strings.TrimSpace(r.ID) == "" {
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: vodErrorBody("volcengine-ark: 2xx submit carries no task id")}, nil
	}
	renamed, _ := json.Marshal(map[string]string{"task_id": strings.TrimSpace(r.ID)})
	return &UpstreamResponse{Status: upstream.Status, Body: renamed, OK: true}, nil
}

// VideosQuery polls one Ark task. The vendor body is normalized into the
// gateway's internal task face (arkTranslateQuery); only then does the
// relay's merge-and-settle machinery see it.
func (a *arkAdaptor) VideosQuery(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	upstream, err := a.arkCall(ctx, ch, http.MethodGet, "/contents/generations/tasks/"+url.PathEscape(upstreamTaskID), nil)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	normalized, err := arkTranslateQuery(upstream.Body)
	if err != nil {
		// 查询响应不可解析 = 暂态上游故障:502 形归一,轮询不改状态不动账。
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: vodErrorBody("volcengine-ark: " + err.Error())}, nil
	}
	return &UpstreamResponse{Status: upstream.Status, Body: normalized, OK: true}, nil
}

// VideosCancel asks Ark to stop one task: DELETE …/tasks/{id} — queued 任务
// 取消、succeeded 任务删除;running 不可取消。非 2xx 不致命:网关照常本地
// 取消不扣费,厂商侧结果只进日志(语义由 relay 兑付)。
func (a *arkAdaptor) VideosCancel(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	return a.arkCall(ctx, ch, http.MethodDelete, "/contents/generations/tasks/"+url.PathEscape(upstreamTaskID), nil)
}

// 聊天/生图面不归这个 adaptor:MVP 仅 videos 能力,这些方法只作防御 ——
// 返回错误让 relay 按上游失败处理(调度过滤下到不了这里);接豆包对话/
// Seedream 生图时在 adaptor 内把桩换成透传即可。
func (a *arkAdaptor) ChatCompletions(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: volcengine-ark adaptor does not relay chat completions")
}

func (a *arkAdaptor) ChatCompletionsStream(ctx context.Context, ch channel.Channel, publicModel string, payload []byte) (*UpstreamStream, error) {
	return nil, errors.New("relay: volcengine-ark adaptor does not relay chat completions")
}

func (a *arkAdaptor) ImagesGenerations(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: volcengine-ark adaptor does not relay images")
}

func (a *arkAdaptor) ImagesEdits(ctx context.Context, ch channel.Channel, contentType string, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: volcengine-ark adaptor does not relay images")
}

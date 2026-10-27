// tencent-mps adaptor(60 号票):腾讯云 MPS(媒资 AIGC)的聚合音乐生成
// 不是 OpenAI 兼容上游 —— TC3 签名(mps.tencentcloudapi.com,service=mps,
// Version 2019-06-12)、异步任务(CreateAigcAudioTask 提交 →
// DescribeAigcAudioTask 轮询)。一个接口聚合 MiniMaxMusic/EL/Mureka/GL 等
// 多家音乐模型:公开模型名映射到上游 "ModelName ModelVersion" 串(空格
// 连接,如 "MiniMaxMusic 3.0"、"Mureka instrumental_9.5",与 VOD adaptor
// 同一惯例);MPS 要求任务产物写进调用方指定的 COS 桶(bucket/region 在
// 渠道 config)。SceneType 恒 "music";AdditionalParameters 按底层模型
// 翻译 —— MiniMaxMusic 写死 {"is_instrumental":true}(纯音乐物种定案),
// Mureka 的纯音乐由 instrumental_* 版本名表达、GL/EL 不需要参数。对外仍
// 是 MusicSubmit/MusicQuery 两个窄方法,提交应答归一 {"task_id"}、轮询
// 应答归一 {"task_status","audio_url","error"},relay 骨架(调度/熔断/
// 换道/计费)零改动。
package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/tc3"
)

const (
	// mpsDefaultEndpoint is where Tencent Cloud serves the MPS API when a
	// channel leaves BaseURL empty(与 channel 包探测器的缺省同值,两处
	// 各自持有 —— relay 不引 channel 的私有常量)。
	mpsDefaultEndpoint = "https://mps.tencentcloudapi.com"

	mpsAPIVersion = "2019-06-12"
	mpsService    = "mps"

	// mpsSceneTypeMusic 固定音乐场景(MPS 的 SceneType 还认 "sfx",那是
	// 61 号票音效的潜在入口,本票不动)。
	mpsSceneTypeMusic = "music"

	// mpsMaxPromptRunes 是 MPS Prompt 的厂商上限(文档:最大支持 2000
	// 字符);网关提交面按同值预检,超限在拨号之前 400。
	mpsMaxPromptRunes = 2000
)

// mpsAdaptor implements Adaptor for tencent-mps channels. Normalize*/聊天/
// 生图/视频/音频方法全部继承 openAIAdaptor 的桩:该类型渠道只声明 music
// 能力,调度不会把别的请求落上来。
type mpsAdaptor struct {
	openAIAdaptor
}

func newMPSAdaptor() *mpsAdaptor {
	return &mpsAdaptor{openAIAdaptor: openAIAdaptor{Client: &http.Client{Transport: upstreamTransport}}}
}

// mpsChannel bundles one channel's MPS-facing view: where to call, with
// which TC3 credentials, and the output COS bucket MPS writes artifacts to.
type mpsChannel struct {
	endpoint  string
	secretID  string
	secretKey string
	region    string
	bucket    string
}

// mpsView resolves the channel into MPS credentials. ok=false means the
// channel is misconfigured (secrets or bucket never stored) — the caller
// answers a 401-shaped refusal: no failover, no breaker debt, 与上游密钥
// 失效同语义。
func mpsView(ch channel.Channel) (mpsChannel, bool) {
	v := mpsChannel{
		endpoint:  strings.TrimRight(ch.BaseURL, "/"),
		secretID:  ch.Config["secret_id"],
		secretKey: ch.Config["secret_key"],
		region:    ch.Config["region"],
		bucket:    ch.Config["bucket"],
	}
	if v.endpoint == "" {
		v.endpoint = mpsDefaultEndpoint
	}
	if v.secretID == "" || v.secretKey == "" || v.bucket == "" || v.region == "" {
		return v, false
	}
	return v, true
}

// mpsEnvelope is the outer shape of every Tencent Cloud API answer: HTTP
// is 200 even for business errors, which live under Response.Error.
type mpsEnvelope struct {
	Response struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
		TaskId string `json:"TaskId"`
	} `json:"Response"`
}

// mpsCall signs and POSTs one action. A 2xx answer with no Response.Error
// comes back OK=true with the full body; every failure shape (transport
// error aside) is normalized to an OpenAI-error-shaped body so the relay's
// ErrorSummary and the failover status mapping keep working unchanged.
func (a *mpsAdaptor) mpsCall(ctx context.Context, ch channel.Channel, action string, payload []byte) (*UpstreamResponse, error) {
	v, ok := mpsView(ch)
	if !ok {
		return mpsRefusal("tencent-mps 渠道缺少 secret_id/secret_key/bucket/region,请在管理端补齐凭据与输出桶"), nil
	}
	status, body, err := tc3.Call(ctx, a.Client, v.endpoint, action, mpsAPIVersion, mpsService,
		v.secretID, v.secretKey, v.region, payload)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return &UpstreamResponse{Status: status, Body: body}, nil
	}
	var env mpsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: mpsErrorBody(fmt.Sprintf("tencent mps %s: 响应体不是 JSON:%.200s", action, body))}, nil
	}
	if e := env.Response.Error; e != nil {
		return &UpstreamResponse{
			Status: vodErrorStatus(e.Code),
			Body:   mpsErrorBody(fmt.Sprintf("tencent mps %s: %s: %s", action, e.Code, e.Message)),
		}, nil
	}
	return &UpstreamResponse{Status: status, Body: body, OK: true}, nil
}

func mpsErrorBody(message string) []byte {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{"message": message, "param": nil, "code": "upstream_error"},
	})
	return body
}

func mpsRefusal(message string) *UpstreamResponse {
	return &UpstreamResponse{Status: http.StatusUnauthorized, Body: mpsErrorBody(message)}
}

// musicSubmitBody is the gateway submit body's adaptor slice — model 已经
// 换成上游 "ModelName ModelVersion" 串,prompt 是风格描述。
type musicSubmitBody struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// mpsAdditionalParameters renders the per-model fixed extras: 纯音乐物种
// 定案下 MiniMaxMusic 需要 is_instrumental 写死 true(MPS 文档的官方示例
// 参数);Mureka 走 instrumental_* 版本名、GL/EL 无需参数,一律不给。
func mpsAdditionalParameters(modelName string) (string, bool) {
	if strings.EqualFold(modelName, "MiniMaxMusic") {
		return `{"is_instrumental":true}`, true
	}
	return "", false
}

// MusicSubmit translates one {"model","prompt"} body into a submitted MPS
// CreateAigcAudioTask, returning the normalized {"task_id"} body. 上游名
// 的空格前段是 ModelName、后段是 ModelVersion(缺版本时省略 ModelVersion,
// MPS 取该模型的稳定版)。
func (a *mpsAdaptor) MusicSubmit(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	var req musicSubmitBody
	if err := json.Unmarshal(payload, &req); err != nil {
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: mpsErrorBody("tencent mps: request body is not a valid JSON object")}, nil
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: mpsErrorBody("tencent mps: 'prompt' is required for music generation")}, nil
	}
	if n := len([]rune(prompt)); n > mpsMaxPromptRunes {
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: mpsErrorBody(fmt.Sprintf("tencent mps: 'prompt' exceeds the %d-character limit", mpsMaxPromptRunes))}, nil
	}
	modelName, modelVersion, _ := strings.Cut(strings.TrimSpace(req.Model), " ")
	if modelName == "" {
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: mpsErrorBody("tencent mps: model mapping is empty")}, nil
	}

	v, ok := mpsView(ch)
	if !ok {
		return mpsRefusal("tencent-mps 渠道缺少 secret_id/secret_key/bucket/region,请在管理端补齐凭据与输出桶"), nil
	}
	mpsReq := map[string]any{
		"ModelName": modelName,
		"SceneType": mpsSceneTypeMusic,
		"Prompt":    prompt,
		"StoreCosParam": map[string]any{
			"CosBucketName":   v.bucket,
			"CosBucketRegion": v.region,
		},
	}
	if modelVersion != "" {
		mpsReq["ModelVersion"] = modelVersion
	}
	if extra, ok := mpsAdditionalParameters(modelName); ok {
		mpsReq["AdditionalParameters"] = extra
	}
	// 输出格式定死 mp3:产物转存的扩展名/嗅探不依赖 Describe 应答(上游
	// 不保证报格式),可预测的容器让「成功即转存」的链路无歧义。
	mpsReq["ExtraParameters"] = map[string]any{"OutputAudioFormat": "mp3"}

	payload, err := json.Marshal(mpsReq)
	if err != nil {
		return nil, err
	}
	upstream, err := a.mpsCall(ctx, ch, "CreateAigcAudioTask", payload)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	var env mpsEnvelope
	if err := json.Unmarshal(upstream.Body, &env); err != nil || env.Response.TaskId == "" {
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: mpsErrorBody("tencent mps CreateAigcAudioTask: 2xx 但无 TaskId")}, nil
	}
	body, _ := json.Marshal(map[string]any{"task_id": env.Response.TaskId})
	return &UpstreamResponse{Status: http.StatusOK, Body: body, OK: true}, nil
}

// MusicQuery relays DescribeAigcAudioTask, normalizing the vendor answer to
// the gateway poll shape: task_status is the RAW vendor string (WAIT/RUN/
// DONE/FAIL — the caller merges it), audio_url the first delivered artifact
// address (产物写进渠道指定的输出 COS 桶,地址由 MPS 回给), error the
// vendor's failure message.
func (a *mpsAdaptor) MusicQuery(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	payload, err := json.Marshal(map[string]any{"TaskId": upstreamTaskID})
	if err != nil {
		return nil, err
	}
	upstream, err := a.mpsCall(ctx, ch, "DescribeAigcAudioTask", payload)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	var env struct {
		Response struct {
			Status     string `json:"Status"`
			Message    string `json:"Message"`
			AudioInfos []struct {
				URL      string `json:"Url"`
				Duration int64  `json:"Duration"`
			} `json:"AudioInfos"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(upstream.Body, &env); err != nil {
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: mpsErrorBody("tencent mps DescribeAigcAudioTask: 响应体不是 JSON")}, nil
	}
	out := map[string]any{"task_status": env.Response.Status}
	if len(env.Response.AudioInfos) > 0 {
		out["audio_url"] = env.Response.AudioInfos[0].URL
	}
	if env.Response.Message != "" {
		out["error"] = map[string]any{"message": env.Response.Message}
	}
	body, _ := json.Marshal(out)
	return &UpstreamResponse{Status: http.StatusOK, Body: body, OK: true}, nil
}

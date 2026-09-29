// tencent-vod adaptor(20 号票):腾讯云点播 AIGC 聚合服务不是 OpenAI 兼容
// 上游 —— TC3 签名、X-TC-Action 头、异步任务(CreateAigcImageTask →
// DescribeTaskDetail 轮询)、产物是任务 Output.FileInfos[].FileUrl。本文件
// 把这些全部关在 adaptor 内:对外仍是 ImagesGenerations/ImagesEdits 两个
// 同步方法,请求翻译(OpenAI images 参数 → VOD OutputConfig)、等待
// (10 分钟预算 + 5 秒轮询)、响应转换(FileUrl → OpenAI data[].url)都在
// 这里完成,relay 骨架(调度/熔断/换道/计费)零改动。聊天/视频方法返回
// 不支持错误:该类型渠道只声明 images 能力,调度不会把别的请求落上来。
package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/tc3"
)

const (
	vodDefaultEndpoint = "https://vod.tencentcloudapi.com"
	vodAPIVersion      = "2018-07-17"
	vodService         = "vod"

	// vodPollInterval / vodTotalBudget bound one synchronous wait. GPT-Image
	// 2.5 Sunburst 是高精度慢档(社区参考实现的图片任务缺省就给 600s),
	// 所以预算独立于全局 buffered 上限放宽到 10 分钟;超时按临时失败
	// 收尾 —— 可换道、退款,但腾讯侧任务可能仍在跑(照计费,且默认
	// 1 并发会被占住),这是票里写明的已知泄漏。
	vodPollInterval = 5 * time.Second
	vodTotalBudget  = 10 * time.Minute

	// vodMaxOutputImages is the vendor-side OutputImageCount ceiling (OG 系
	// 1-8); beyond it the request is refused before submit as a client error.
	vodMaxOutputImages = 8

	// vodPollTolerance is how many consecutive transient poll failures
	// (network, non-2xx, unparseable body, InternalError) one wait absorbs
	// before giving up as a temporary failure.
	vodPollTolerance = 3
)

// vodTaskKey names the DescribeTaskDetail sub-object carrying one AIGC
// task (AigcImageTask, SceneAigcVideoTask, …).
var vodTaskKey = regexp.MustCompile(`^(Aigc|SceneAigc)\w*Task$`)

// vodAdaptor implements Adaptor for tencent-vod channels. Normalize* 与
// ErrorSummary 复用 openAIAdaptor(响应体在 adaptor 内已转成 OpenAI 形状)。
type vodAdaptor struct {
	openAIAdaptor

	// pollInterval / budget exist as fields so tests can tighten the wait;
	// production gets the ticketed constants from newVODAdaptor.
	pollInterval time.Duration
	budget       time.Duration
}

// newVODAdaptor returns the production adaptor with the ticketed wait budget.
func newVODAdaptor() *vodAdaptor {
	return &vodAdaptor{
		openAIAdaptor: openAIAdaptor{Client: &http.Client{Transport: upstreamTransport}},
		pollInterval:  vodPollInterval,
		budget:        vodTotalBudget,
	}
}

// vodChannel bundles one channel's VOD-facing view: where to call, with
// which TC3 credentials, and the optional SubAppId every request carries.
type vodChannel struct {
	endpoint  string
	secretID  string
	secretKey string
	region    string
	subAppID  int64
}

// vodView resolves the channel into VOD credentials. ok=false means the
// channel is misconfigured (secrets never stored) — the caller answers a
// 401-shaped refusal: no failover, no breaker debt, 与上游密钥失效同语义。
func vodView(ch channel.Channel) (vodChannel, bool) {
	v := vodChannel{
		endpoint:  strings.TrimRight(ch.BaseURL, "/"),
		secretID:  ch.Config["secret_id"],
		secretKey: ch.Config["secret_key"],
		region:    ch.Config["region"],
	}
	if v.endpoint == "" {
		v.endpoint = vodDefaultEndpoint
	}
	if v.secretID == "" || v.secretKey == "" {
		return v, false
	}
	if sub := strings.TrimSpace(ch.Config["sub_app_id"]); sub != "" {
		id, err := strconv.ParseInt(sub, 10, 64)
		if err != nil {
			return v, false
		}
		v.subAppID = id
	}
	return v, true
}

// vodEnvelope is the outer shape of every Tencent Cloud API answer: HTTP
// is 200 even for business errors, which live under Response.Error.
type vodEnvelope struct {
	Response struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
		TaskId     string          `json:"TaskId"`
		TaskDetail json.RawMessage `json:"TaskDetail"`
	} `json:"Response"`
}

// vodCall signs and POSTs one action. A 2xx answer with no Response.Error
// comes back OK=true with the full body; every failure shape (transport
// error aside) is normalized to an OpenAI-error-shaped body so the relay's
// ErrorSummary and the failover status mapping keep working unchanged.
func (a *vodAdaptor) vodCall(ctx context.Context, ch channel.Channel, action string, payload []byte) (*UpstreamResponse, error) {
	v, ok := vodView(ch)
	if !ok {
		return vodRefusal("tencent-vod 渠道缺少 secret_id/secret_key(或 sub_app_id 不是数字),请在管理端补齐凭据"), nil
	}
	status, body, err := tc3.Call(ctx, a.Client, v.endpoint, action, vodAPIVersion, vodService,
		v.secretID, v.secretKey, v.region, payload)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return &UpstreamResponse{Status: status, Body: body}, nil
	}
	var env vodEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: vodErrorBody(fmt.Sprintf("tencent vod %s: 响应体不是 JSON:%.200s", action, body))}, nil
	}
	if e := env.Response.Error; e != nil {
		return &UpstreamResponse{
			Status: vodErrorStatus(e.Code),
			Body:   vodErrorBody(fmt.Sprintf("tencent vod %s: %s: %s", action, e.Code, e.Message)),
		}, nil
	}
	return &UpstreamResponse{Status: status, Body: body, OK: true}, nil
}

// vodErrorStatus maps one Tencent error code onto the HTTP status the
// relay's failover semantics read: 4xx = client-side (no failover, no
// breaker debt), 429/5xx = temporary (failover). Unknown codes land on 502
// — safest default, a failover-able temporary failure.
func vodErrorStatus(code string) int {
	switch {
	case strings.HasPrefix(code, "AuthFailure"):
		return http.StatusUnauthorized
	case strings.HasPrefix(code, "InvalidParameter"),
		code == "MissingParameter",
		strings.HasPrefix(code, "UnauthorizedOperation"):
		return http.StatusBadRequest
	case strings.HasPrefix(code, "LimitExceeded"),
		strings.Contains(code, "Limit"),
		strings.Contains(code, "Concurrent"):
		return http.StatusTooManyRequests
	default:
		return http.StatusBadGateway
	}
}

func vodErrorBody(message string) []byte {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{"message": message, "param": nil, "code": "upstream_error"},
	})
	return body
}

func vodRefusal(message string) *UpstreamResponse {
	return &UpstreamResponse{Status: http.StatusUnauthorized, Body: vodErrorBody(message)}
}

// vodImagesRequest is the slice of the (model-rewritten) OpenAI images
// body the VOD translation needs; everything else is not expressible
// upstream and dropped by design (response_format 恒 url,quality 不翻译).
type vodImagesRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	N      *int64 `json:"n"`
	Size   string `json:"size"`
}

// vodSubmitTask builds and submits one CreateAigcImageTask, returning the
// vendor TaskId. modelName carries the ModelMap upstream string with the
// documented shape "ModelName ModelVersion" (空格连接,如 "OG image2.5_sunburst")。
func (a *vodAdaptor) vodSubmitTask(ctx context.Context, ch channel.Channel, modelName, prompt, size string, n int64, refs []map[string]string) (*UpstreamResponse, string, error) {
	if n < 1 || n > vodMaxOutputImages {
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: vodErrorBody(fmt.Sprintf("'n' must be between 1 and %d on tencent-vod image channels.", vodMaxOutputImages))}, "", nil
	}
	name, version, _ := strings.Cut(strings.TrimSpace(modelName), " ")
	resolution, aspect := vodSizeConfig(size)
	output := map[string]any{
		"StorageMode": "Temporary", // 持久化交由 14 号票转存链;Permanent 留待 VOD 侧超分诉求
		"Resolution":  resolution,
		"AspectRatio": aspect,
	}
	if n > 1 {
		output["OutputImageCount"] = n
	}
	req := map[string]any{
		"ModelName":    name,
		"ModelVersion": version,
		"Prompt":       prompt,
		"OutputConfig": output,
	}
	if v, ok := vodView(ch); ok && v.subAppID != 0 {
		req["SubAppId"] = v.subAppID
	}
	if len(refs) > 0 {
		req["FileInfos"] = refs
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, "", err
	}

	upstream, err := a.vodCall(ctx, ch, "CreateAigcImageTask", payload)
	if err != nil || !upstream.OK {
		return upstream, "", err
	}
	var env vodEnvelope
	if err := json.Unmarshal(upstream.Body, &env); err != nil || env.Response.TaskId == "" {
		return &UpstreamResponse{Status: http.StatusBadGateway,
			Body: vodErrorBody("tencent vod CreateAigcImageTask: 2xx 但无 TaskId")}, "", nil
	}
	return upstream, env.Response.TaskId, nil
}

// vodWaitTask polls DescribeTaskDetail to a terminal state inside the
// adaptor budget, then converts the delivered FileUrl list into one
// OpenAI-shaped images body — the sync wrapper 01 号票定下的形态.
func (a *vodAdaptor) vodWaitTask(ctx context.Context, ch channel.Channel, taskID string) (*UpstreamResponse, error) {
	deadline := time.Now().Add(a.budget)
	transient := 0
	for {
		poll := map[string]any{"TaskId": taskID}
		if v, ok := vodView(ch); ok && v.subAppID != 0 {
			poll["SubAppId"] = v.subAppID
		}
		payload, err := json.Marshal(poll)
		if err != nil {
			return nil, err
		}
		upstream, err := a.vodCall(ctx, ch, "DescribeTaskDetail", payload)
		switch {
		case err != nil || upstream == nil:
			// 网络层失败:可容忍的暂态。
		case !upstream.OK && (upstream.Status >= 500 || upstream.Status == 429):
			// 5xx/429(含 InternalError 的归一):可容忍的暂态。
		case !upstream.OK:
			// 400/401:参数坏了或签名这关都过不了,重试无意义。
			return upstream, nil
		default:
			status, urls, failMsg := vodTaskResult(upstream.Body)
			switch {
			case status == "SUCCESS" || status == "FINISH" || status == "DONE":
				if len(urls) == 0 {
					return &UpstreamResponse{Status: http.StatusBadGateway,
						Body: vodErrorBody("tencent vod task succeeded but delivered no image URLs")}, nil
				}
				body, _ := json.Marshal(map[string]any{
					"created": time.Now().Unix(),
					"data":    urlsToData(urls),
				})
				return &UpstreamResponse{Status: http.StatusOK, Body: body, OK: true}, nil
			case status == "FAIL" || status == "FAILED" || status == "ERROR":
				return &UpstreamResponse{Status: http.StatusBadGateway,
					Body: vodErrorBody("tencent vod task failed: " + failMsg)}, nil
			default:
				transient = 0 // 拿到合法的处理中状态,暂态计数清零
			}
		}

		if transient++; transient > vodPollTolerance {
			return &UpstreamResponse{Status: http.StatusBadGateway,
				Body: vodErrorBody("tencent vod polling failed repeatedly (transient upstream errors)")}, nil
		}
		if time.Now().After(deadline) {
			// 已知泄漏:腾讯侧任务可能仍在跑并计费(VOD 未查到取消接口);
			// 按临时失败收尾,预扣由 relay 退还。
			return &UpstreamResponse{Status: http.StatusGatewayTimeout,
				Body: vodErrorBody(fmt.Sprintf("tencent vod task %s did not finish within %s", taskID, a.budget))}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(a.pollInterval):
		}
	}
}

// vodTaskResult extracts the merged status, the delivered URLs and a
// failure summary from one DescribeTaskDetail 2xx body — the walk mirrors
// the reference client: the task lives in the (Aigc|SceneAigc)*Task
// sub-object, URLs only under Output (Input 子树必须忽略).
func vodTaskResult(body []byte) (status string, urls []string, failMsg string) {
	// Response 原样取字节再解:任务字段可能平铺在 Response 顶层
	// (Status/FileInfos),也可能嵌在 (Aigc|SceneAigc)*Task 子对象或
	// TaskDetail 里,声明死字段会丢形状。
	var envelope struct {
		Response json.RawMessage `json:"Response"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Response) == 0 {
		return "", nil, "响应体不是 JSON"
	}
	detail := envelope.Response
	var root map[string]any
	if err := json.Unmarshal(detail, &root); err != nil {
		return "", nil, "任务详情不是 JSON 对象"
	}
	if nested, ok := root["TaskDetail"].(map[string]any); ok {
		root = nested
	}
	task := root
	for key, val := range root {
		if sub, ok := val.(map[string]any); ok && vodTaskKey.MatchString(key) {
			task = sub
			break
		}
	}
	if s, ok := task["Status"].(string); ok {
		status = strings.ToUpper(s)
	}
	if status == "" {
		if s, ok := root["Status"].(string); ok {
			status = strings.ToUpper(s)
		}
	}
	if code, ok := task["ErrCode"].(string); ok && code != "" {
		failMsg = code
		if msg, ok := task["Message"].(string); ok && msg != "" {
			failMsg += ": " + msg
		}
	}
	if failMsg == "" {
		if msg, ok := task["Message"].(string); ok && msg != "" {
			failMsg = msg
		}
	}
	if output, ok := task["Output"].(map[string]any); ok {
		urls = collectURLs(output)
	} else {
		// 平铺形态(无 Output 包装):整个任务对象都是产物树,
		// Input 侧若有引用文件会混进来,由 Output 缺失本身排除
		// (参考实现同款回退)。
		urls = collectURLs(task)
	}
	return status, urls, failMsg
}

// collectURLs walks one Output subtree collecting FileUrl/Url strings.
func collectURLs(node any) []string {
	var urls []string
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if u, ok := v["FileUrl"].(string); ok && u != "" {
				urls = append(urls, u)
			}
			if u, ok := v["Url"].(string); ok && u != "" {
				urls = append(urls, u)
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(node)
	return urls
}

func urlsToData(urls []string) []map[string]string {
	data := make([]map[string]string, len(urls))
	for i, u := range urls {
		data[i] = map[string]string{"url": u}
	}
	return data
}

// vodSizeConfig maps one OpenAI size ("1024x1024"/"auto"/缺省) onto the
// VOD OutputConfig pair: 短边 ≥2048 → 2K 档,否则 1K;宽高比就近取枚举;
// 解析不了的尺寸(含 auto/缺省)落 1K + adaptive,由模型自行决定构图。
func vodSizeConfig(size string) (resolution, aspect string) {
	resolution, aspect = "1K", "adaptive"
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(size)), "x", 2)
	if len(parts) != 2 {
		return resolution, aspect
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return resolution, aspect
	}
	if min(w, h) >= 2048 {
		resolution = "2K"
	}
	aspect = nearestVODAspect(w, h)
	return resolution, aspect
}

// vodAspects is the vendor's AspectRatio enum with its exact ratios.
var vodAspects = []struct {
	name  string
	ratio float64
}{
	{"21:9", 21.0 / 9}, {"16:9", 16.0 / 9}, {"4:3", 4.0 / 3}, {"1:1", 1},
	{"3:4", 3.0 / 4}, {"9:16", 9.0 / 16},
}

// nearestVODAspect picks the enum entry closest in log-ratio distance.
func nearestVODAspect(w, h int) string {
	actual := float64(w) / float64(h)
	best, bestDist := "adaptive", math.MaxFloat64
	for _, a := range vodAspects {
		if d := math.Abs(math.Log(actual / a.ratio)); d < bestDist {
			best, bestDist = a.name, d
		}
	}
	return best
}

// ImagesGenerations translates one model-rewritten generations body into
// a submitted-and-awaited VOD task (text-to-image: no FileInfos).
func (a *vodAdaptor) ImagesGenerations(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	var req vodImagesRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: vodErrorBody("tencent vod: request body is not a valid JSON object")}, nil
	}
	n := int64(1)
	if req.N != nil {
		n = *req.N
	}
	upstream, taskID, err := a.vodSubmitTask(ctx, ch, req.Model, req.Prompt, req.Size, n, nil)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	return a.vodWaitTask(ctx, ch, taskID)
}

// ImagesEdits translates one rebuilt multipart edits form into a VOD
// image-to-image task: every uploaded file part becomes a Base64
// reference FileInfo (生图 FileInfos 仅 Type+Base64,无 Category/Usage),
// text fields ride the same translation as generations;mask 不翻译。
func (a *vodAdaptor) ImagesEdits(ctx context.Context, ch channel.Channel, contentType string, payload []byte) (*UpstreamResponse, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return &UpstreamResponse{Status: http.StatusBadRequest,
			Body: vodErrorBody("tencent vod: edits body must be a parseable multipart/form-data")}, nil
	}
	reader := multipart.NewReader(bytes.NewReader(payload), params["boundary"])

	var prompt, model, size string
	n := int64(1)
	var refs []map[string]string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &UpstreamResponse{Status: http.StatusBadRequest,
				Body: vodErrorBody("tencent vod: edits multipart body could not be parsed")}, nil
		}
		if part.FileName() == "" {
			switch part.FormName() {
			case "prompt":
				prompt = readPartText(part)
			case "model":
				model = readPartText(part)
			case "size":
				size = readPartText(part)
			case "n":
				if v, perr := strconv.ParseInt(strings.TrimSpace(readPartText(part)), 10, 64); perr == nil {
					n = v
				}
			}
			continue
		}
		data, rerr := io.ReadAll(io.LimitReader(part, maxUpstreamBody))
		part.Close()
		if rerr != nil {
			return &UpstreamResponse{Status: http.StatusBadRequest,
				Body: vodErrorBody("tencent vod: reference image part could not be read")}, nil
		}
		refs = append(refs, map[string]string{
			"Type":   "Base64",
			"Base64": base64.StdEncoding.EncodeToString(data),
		})
	}

	upstream, taskID, err := a.vodSubmitTask(ctx, ch, model, prompt, size, n, refs)
	if err != nil || !upstream.OK {
		return upstream, err
	}
	return a.vodWaitTask(ctx, ch, taskID)
}

func readPartText(part *multipart.Part) string {
	data, err := io.ReadAll(io.LimitReader(part, maxUpstreamBody))
	if err != nil {
		return ""
	}
	return string(data)
}

// 聊天/视频面不归这个 adaptor:该类型渠道只声明 images 能力,这些方法
// 只作防御 —— 返回错误让 relay 按上游失败处理(调度过滤下到不了这里)。
func (a *vodAdaptor) ChatCompletions(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: tencent-vod adaptor does not relay chat completions")
}

func (a *vodAdaptor) ChatCompletionsStream(ctx context.Context, ch channel.Channel, publicModel string, payload []byte) (*UpstreamStream, error) {
	return nil, errors.New("relay: tencent-vod adaptor does not relay chat completions")
}

func (a *vodAdaptor) VideosSubmit(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	return nil, errors.New("relay: tencent-vod adaptor does not relay video tasks")
}

func (a *vodAdaptor) VideosQuery(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	return nil, errors.New("relay: tencent-vod adaptor does not relay video tasks")
}

func (a *vodAdaptor) VideosCancel(ctx context.Context, ch channel.Channel, upstreamTaskID string) (*UpstreamResponse, error) {
	return nil, errors.New("relay: tencent-vod adaptor does not relay video tasks")
}

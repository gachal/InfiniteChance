package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gachal/InfiniteChance/internal/tc3"
)

// probeTimeout bounds one upstream connectivity probe.
const probeTimeout = 10 * time.Second

// maxErrorSnippet caps how much of an upstream error body is echoed back to
// the admin — enough to identify an auth failure, not a full dump.
const maxErrorSnippet = 512

// maxListingBytes caps the success body we read for the model count. Real
// /models listings run far past the error-snippet size, so the success path
// needs its own generous budget.
const maxListingBytes = 4 << 20

// probeClient is the shared probe client — one pool for all connectivity
// probes instead of a fresh client (and fresh TLS handshake) per click.
var probeClient = &http.Client{Timeout: probeTimeout}

// Tester probes a channel by calling GET {base_url}/models with the stored
// secret — the one free request every OpenAI-compatible vendor answers, so
// the admin gets a decidable ok/fail right after saving a channel.
type Tester struct {
	// Client is optional; nil means the shared probe client.
	Client *http.Client
}

// Result is the admin-facing probe verdict.
type Result struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Detail    string `json:"detail,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Test runs one probe. It never returns an error: every failure mode is a
// decidable Result, so the endpoint can answer 200 with ok=false. The probe
// itself is type-aware (20 号票): OpenAI-compatible upstreams answer the free
// GET {base_url}/models; a tencent-vod channel is TC3-signed and has no
// /models, so it gets the VOD probe below instead; volcengine-ark (25 号票)
// has no /models either and gets the Ark probe below.
func (t *Tester) Test(ctx context.Context, ch Channel) Result {
	if ch.Type == TypeTencentVod {
		return t.testTencentVod(ctx, ch)
	}
	if ch.Type == TypeVolcengineArk {
		return t.testVolcengineArk(ctx, ch)
	}
	if ch.APIKey == "" {
		return Result{Error: "渠道未配置密钥,请先保存密钥再测试"}
	}
	client := t.Client
	if client == nil {
		client = probeClient
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet,
		strings.TrimRight(ch.BaseURL, "/")+"/models", nil)
	if err != nil {
		return Result{Error: fmt.Sprintf("构造探测请求失败:%v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+ch.APIKey)
	req.Header.Set("User-Agent", "infinitechance-probe")

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return Result{LatencyMS: latency, Error: fmt.Sprintf("连接上游失败:%v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 == 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxListingBytes))
		detail := fmt.Sprintf("HTTP %d,连通正常", resp.StatusCode)
		if n, ok := countModels(body); ok {
			detail = fmt.Sprintf("HTTP %d,发现 %d 个模型", resp.StatusCode, n)
		}
		return Result{OK: true, LatencyMS: latency, Detail: detail}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippet))
	snippet := strings.TrimSpace(string(body))
	if snippet == "" {
		return Result{LatencyMS: latency,
			Error: fmt.Sprintf("上游返回 HTTP %d(无响应体)", resp.StatusCode)}
	}
	return Result{LatencyMS: latency,
		Error: fmt.Sprintf("上游返回 HTTP %d:%s", resp.StatusCode, snippet)}
}

// countModels best-effort parses the standard {"data":[...]} listing; a
// non-listing 2xx body still means reachable, just without a model count.
func countModels(body []byte) (int, bool) {
	var listing struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return 0, false
	}
	return len(listing.Data), true
}

// vodDefaultEndpoint is where Tencent Cloud serves the VOD API when a
// channel leaves BaseURL empty (mirror of the relay adaptor's default).
const vodDefaultEndpoint = "https://vod.tencentcloudapi.com"

// testTencentVod probes one tencent-vod channel with a TC3-signed
// DescribeTaskDetail for a task id that cannot exist. Tencent answers
// HTTP 200 with the business error inside Response.Error, so a
// task-not-found style answer proves the whole chain — network, endpoint,
// signature, SecretId/SecretKey, SubAppId — in one cheap call; an
// AuthFailure (bad key or drifted clock) or SubAppId complaint is the
// decidable failure the admin can act on.
func (t *Tester) testTencentVod(ctx context.Context, ch Channel) Result {
	secretID := ch.Config["secret_id"]
	secretKey := ch.Config["secret_key"]
	if secretID == "" || secretKey == "" {
		return Result{Error: "tencent-vod 渠道未配置 secret_id/secret_key,请先保存凭据再测试"}
	}
	endpoint := ch.BaseURL
	if endpoint == "" {
		endpoint = vodDefaultEndpoint
	}
	payload := []byte(`{"TaskId":"infinitechance-probe-not-a-task"}`)
	if sub := strings.TrimSpace(ch.Config["sub_app_id"]); sub != "" {
		if _, err := strconv.ParseInt(sub, 10, 64); err != nil {
			return Result{Error: "sub_app_id 必须是数字(腾讯云子应用 ID)"}
		}
		payload = []byte(`{"SubAppId":` + sub + `,"TaskId":"infinitechance-probe-not-a-task"}`)
	}

	client := t.Client
	if client == nil {
		client = probeClient
	}
	start := time.Now()
	status, body, err := tc3.Call(ctx, client, endpoint, "DescribeTaskDetail",
		"2018-07-17", "vod", secretID, secretKey, ch.Config["region"], payload)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return Result{LatencyMS: latency, Error: fmt.Sprintf("连接腾讯云失败:%v", err)}
	}

	var env struct {
		Response struct {
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return Result{LatencyMS: latency,
			Error: fmt.Sprintf("腾讯云应答不是 JSON(HTTP %d)", status)}
	}
	e := env.Response.Error
	if e == nil {
		// 理论上到不了:探测的 TaskId 必不存在。到了也算连通成功。
		return Result{OK: true, LatencyMS: latency,
			Detail: fmt.Sprintf("HTTP %d,凭据与网络正常", status)}
	}
	if strings.HasPrefix(e.Code, "AuthFailure") {
		return Result{LatencyMS: latency, Error: fmt.Sprintf(
			"凭据或签名失败(%s):%s(密钥错误,或本机时钟漂移超过 5 分钟)",
			e.Code, truncateRunes(e.Message, maxErrorSnippet))}
	}
	if strings.Contains(e.Code, "SubAppId") || strings.Contains(e.Message, "SubAppId") {
		return Result{LatencyMS: latency, Error: fmt.Sprintf(
			"SubAppId 不被接受(%s):%s", e.Code, truncateRunes(e.Message, maxErrorSnippet))}
	}
	// 任务不存在一类业务错:签名与凭据全部走通,连通即成功。
	return Result{OK: true, LatencyMS: latency,
		Detail: fmt.Sprintf("HTTP %d,凭据与网络正常(腾讯云应答 %s)", status, e.Code)}
}

// truncateRunes cuts s to at most n runes without splitting a character.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// testVolcengineArk probes one volcengine-ark channel with a GET for a task
// id that cannot exist (tencent-vod 假任务探测同款套路,25 号票): a
// business-level "task not found" answer (404 / NotFound 类 code) proves the
// whole chain — network, endpoint, Bearer key — in one cheap call; 401/403
// is the decidable credential failure. Anything else (429/5xx/坏形状) is
// reported verbatim — reachable but not provably healthy. 具体错误码以真
// key 实证为准(票内验证点),未实证前按 HTTP 状态分类。
func (t *Tester) testVolcengineArk(ctx context.Context, ch Channel) Result {
	if ch.APIKey == "" {
		return Result{Error: "渠道未配置密钥,请先保存密钥再测试"}
	}
	client := t.Client
	if client == nil {
		client = probeClient
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet,
		strings.TrimRight(ch.BaseURL, "/")+"/contents/generations/tasks/infinitechance-probe-not-a-task", nil)
	if err != nil {
		return Result{Error: fmt.Sprintf("构造探测请求失败:%v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+ch.APIKey)
	req.Header.Set("User-Agent", "infinitechance-probe")

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return Result{LatencyMS: latency, Error: fmt.Sprintf("连接上游失败:%v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippet))
	// Ark 错误体是 OpenAI 形 {"error":{"code","message"}},提取 code 佐证判定。
	code := ""
	var env struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error != nil {
		code = env.Error.Code
	}
	detail := func() string {
		if code != "" {
			return fmt.Sprintf("HTTP %d,凭据与网络正常(方舟应答 %s)", resp.StatusCode, code)
		}
		return fmt.Sprintf("HTTP %d,凭据与网络正常", resp.StatusCode)
	}
	snippet := strings.TrimSpace(string(body))

	switch {
	case resp.StatusCode/100 == 2:
		// 探测的 TaskId 必不存在;2xx 到不了,到了也算连通成功。
		return Result{OK: true, LatencyMS: latency, Detail: detail()}
	case resp.StatusCode == http.StatusNotFound:
		// 业务级「任务不存在」:凭据与连通全通。
		return Result{OK: true, LatencyMS: latency, Detail: detail()}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		reason := code
		if reason == "" {
			reason = snippet
		}
		return Result{LatencyMS: latency, Error: fmt.Sprintf(
			"凭据失败(HTTP %d):%s", resp.StatusCode, truncateRunes(reason, maxErrorSnippet))}
	default:
		if snippet == "" {
			return Result{LatencyMS: latency,
				Error: fmt.Sprintf("上游返回 HTTP %d(无响应体)", resp.StatusCode)}
		}
		return Result{LatencyMS: latency,
			Error: fmt.Sprintf("上游返回 HTTP %d:%s", resp.StatusCode, truncateRunes(snippet, maxErrorSnippet))}
	}
}

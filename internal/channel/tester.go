package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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

// Tester probes a channel by calling GET {base_url}/models with the stored
// secret — the one free request every OpenAI-compatible vendor answers, so
// the admin gets a decidable ok/fail right after saving a channel.
type Tester struct {
	// Client is optional; nil means a client with probeTimeout.
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
// decidable Result, so the endpoint can answer 200 with ok=false.
func (t *Tester) Test(ctx context.Context, ch Channel) Result {
	if ch.APIKey == "" {
		return Result{Error: "渠道未配置密钥,请先保存密钥再测试"}
	}
	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: probeTimeout}
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

package relay_test

// 10 号票(画布来源标记)的中转面测试:canvas/server 以服务级 key 调
// /v1/images/generations 时带上 X-InfiniteChance-Source,网关把它的值
// 落进用量日志的 source 列 —— 画布来源的花费在审计里与直连流量可分。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/relay"
)

func TestRelayImagesUsageCarriesSourceHeader(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, okImagesHandler(map[string]any{"url": "https://img.example/dog.png"}))
	env.seedImageChannel(t, "img", upstream.server.URL, "img-m", "upstream-img", 0,
		[]channel.Capability{channel.CapImages})
	_, full := env.seedKey(t, 1_000_000)
	env.seedImagePrice(t, "img-m", nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(imageBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+full)
	req.Header.Set("X-InfiniteChance-Source", "canvas=7 task=ct_abc node=image-1-1")
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body.String())
	}

	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	if got := rows[0].Source; got != "canvas=7 task=ct_abc node=image-1-1" {
		t.Errorf("usage source = %q, want the canvas origin mark", got)
	}
}

func TestRelayUsageWithoutSourceHeaderStaysEmpty(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, okImagesHandler(map[string]any{"url": "https://img.example/dog.png"}))
	env.seedImageChannel(t, "img", upstream.server.URL, "img-m", "upstream-img", 0,
		[]channel.Capability{channel.CapImages})
	_, full := env.seedKey(t, 1_000_000)
	env.seedImagePrice(t, "img-m", nil)

	if w := env.postImages(t, full, imageBody); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	rows := env.usageRows(t)
	if len(rows) != 1 || rows[0].Source != "" {
		t.Fatalf("usage rows = %+v, want one row with an empty source", rows)
	}
}

// 11 号票:提示词生成走聊天轨,同样以 source 标记入账 —— 画布来源的
// token 用量在 usage_logs 里与直连聊天可分、可对账。
func TestRelayChatUsageCarriesSourceHeaderAndTokens(t *testing.T) {
	env := newRelayEnv(t, nil)
	upstream := newFakeUpstream(t, okChatHandler("a neon cyberpunk city", 120, 340))
	env.seedChannel(t, upstream.server.URL, "public-m", "upstream-m")
	_, full := env.seedKey(t, 1_000_000)
	env.seedPrice(t, "public-m")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(testBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+full)
	req.Header.Set("X-InfiniteChance-Source", "canvas=7 node=prompt-1-1 gen=prompt")
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", w.Code, w.Body.String())
	}

	rows := env.usageRows(t)
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if got := row.Source; got != "canvas=7 node=prompt-1-1 gen=prompt" {
		t.Errorf("usage source = %q, want the canvas origin mark", got)
	}
	if row.Unit != "token" || row.PromptTokens != 120 || row.CompletionTokens != 340 {
		t.Errorf("usage = unit %s tokens %d/%d, want token track with the upstream counts",
			row.Unit, row.PromptTokens, row.CompletionTokens)
	}
}

func TestSourceFromSanitizesHeaderValue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 控制字符剔除、首尾空白剪掉。
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Request.Header.Set("X-InfiniteChance-Source", " canvas=1\x00task=ct_a ")
	if got, want := relay.SourceFrom(c), "canvas=1task=ct_a"; got != want {
		t.Errorf("SourceFrom = %q, want %q", got, want)
	}

	// 超长值截断到列宽以内,不放过长的垃圾进审计列。
	c.Request.Header.Set("X-InfiniteChance-Source", strings.Repeat("长", 500))
	if got := relay.SourceFrom(c); len([]rune(got)) != 255 {
		t.Errorf("SourceFrom rune count = %d, want capped at 255", len([]rune(got)))
	}

	// 缺头为空串。
	c.Request.Header.Del("X-InfiniteChance-Source")
	if got := relay.SourceFrom(c); got != "" {
		t.Errorf("SourceFrom without header = %q, want empty", got)
	}
}

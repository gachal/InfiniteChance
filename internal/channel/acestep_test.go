package channel_test

// 80 号票(acestep 渠道类型,ACE-Step 1.5 自托管 cover 同曲换词)的注册
// 面测试:Normalize 的类型规则(BaseURL 必填、api_key **可选** —— 鉴权是
// 服务端 ACESTEP_API_KEY 环境变量控制的开关,无 key 时 Bearer 头不发;
// 无 config 凭据要求)与类型化连通探测(GET {base}/health:2xx = 连通全
// 通,带 key 时 401 = 凭据失败,其余如实报 —— 见 tester.go testAcestep)。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
)

func TestNormalizeAcestepTypeRules(t *testing.T) {
	// 合法:BaseURL 是局域网 http 地址(自托管服务,端口 8001),可选带
	// key(带则原样 trim 落 api_key 列)。
	in := channel.Input{Name: "acestep 本机", Type: channel.TypeAcestep,
		BaseURL: " http://192.168.1.20:8001 ", APIKey: "  local-token  ",
		ModelMap:     map[string]string{"acestep-relyric": "acestep-v1.5"},
		Capabilities: []channel.Capability{channel.CapMusic}}
	norm, err := in.Normalize(true)
	if err != nil {
		t.Fatalf("valid acestep channel rejected: %v", err)
	}
	if norm.APIKey != "local-token" || norm.BaseURL != "http://192.168.1.20:8001" {
		t.Fatalf("normalized = %+v", norm)
	}

	// 类型进 SupportedTypes(唯一的全量类型表)。
	if !channel.SupportedType(channel.TypeAcestep) {
		t.Fatal("acestep not in SupportedTypes")
	}

	// BaseURL 必填:自托管服务的地址就是渠道的全部路由信息,空 = 渠道
	// 无处可拨。
	if _, err := (channel.Input{Name: "a", Type: channel.TypeAcestep, APIKey: "k"}).Normalize(true); err == nil {
		t.Fatal("acestep without BaseURL should fail")
	}
	// API key 可选:服务端鉴权是可选开关(ACESTEP_API_KEY 空 = 关闭),
	// 无 key 的渠道是合法形态 —— 与 mureka/minimax 的必填口径相反。
	if _, err := (channel.Input{Name: "a", Type: channel.TypeAcestep,
		BaseURL: "http://192.168.1.20:8001"}).Normalize(true); err != nil {
		t.Fatalf("acestep without api_key should pass: %v", err)
	}
}

func TestAcestepConnectivityProbeByVerdict(t *testing.T) {
	cases := []struct {
		name    string
		apiKey  string
		status  int
		answer  string
		wantOK  bool
		wantIn  string // detail 或 error 里应包含的片段
	}{
		{
			// [官方] /health 一等健康检查:2xx = 网络、端点、(可选)Bearer
			// 全链走通。
			name:   "2xx=连通成功",
			status: http.StatusOK,
			answer: `{"status":"ok"}`,
			wantOK: true,
			wantIn: "连通正常",
		},
		{
			name:   "无 key 2xx=连通成功",
			status: http.StatusOK,
			answer: `{"status":"ok"}`,
			wantOK: true,
			wantIn: "连通正常",
		},
		{
			// 带配置 key 时 401 = 凭据失败(服务端开了鉴权而 key 不对)。
			name:   "401=凭据失败",
			apiKey: "acestep-key",
			status: http.StatusUnauthorized,
			answer: `{"detail":"invalid api key"}`,
			wantOK: false,
			wantIn: "凭据失败",
		},
		{
			name:   "5xx=如实报错",
			status: http.StatusInternalServerError,
			answer: `{"detail":"model not loaded"}`,
			wantOK: false,
			wantIn: "500",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sawMethod, sawPath, sawAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sawMethod, sawPath, sawAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.answer))
			}))
			defer srv.Close()

			tester := &channel.Tester{Client: srv.Client()}
			ch := channel.Channel{Type: channel.TypeAcestep, BaseURL: srv.URL, APIKey: tc.apiKey}
			result := tester.Test(context.Background(), ch)
			if result.OK != tc.wantOK {
				t.Fatalf("ok = %v (detail=%q error=%q), want %v", result.OK, result.Detail, result.Error, tc.wantOK)
			}
			if sawMethod != http.MethodGet || !strings.HasSuffix(sawPath, "/health") {
				t.Fatalf("probe = %s %s, want GET {base}/health", sawMethod, sawPath)
			}
			if tc.apiKey != "" && sawAuth != "Bearer "+tc.apiKey {
				t.Fatalf("probe auth = %q, want the Bearer key", sawAuth)
			}
			if tc.apiKey == "" && sawAuth != "" {
				t.Fatalf("probe auth = %q, want no Authorization header without a key", sawAuth)
			}
			combined := result.Detail + result.Error
			if !strings.Contains(combined, tc.wantIn) {
				t.Fatalf("verdict %q missing %q", combined, tc.wantIn)
			}
		})
	}
}

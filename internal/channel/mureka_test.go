package channel_test

// 79 号票(mureka 渠道类型,Song Edit 同曲换词)的注册面测试:Normalize
// 的类型规则(裸域名 BaseURL 必填、api_key 必填、无 config 凭据 —— 照
// minimax 形状)与类型化连通探测(GET 假任务 ID:400/404 业务错 = 连通
// 成功,401/403 = 凭据失败,其余如实报)。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
)

func TestNormalizeMurekaTypeRules(t *testing.T) {
	// 合法:BaseURL 是裸域名(版本路径由 adaptor/tester 拼),单 Bearer
	// key 存 api_key 列。
	in := channel.Input{Name: "mureka", Type: channel.TypeMureka,
		BaseURL: "https://api.mureka.ai", APIKey: "  mureka-key-1  ",
		ModelMap:     map[string]string{"mureka-song-edit": "mureka-9.5"},
		Capabilities: []channel.Capability{channel.CapMusic}}
	norm, err := in.Normalize(true)
	if err != nil {
		t.Fatalf("valid mureka channel rejected: %v", err)
	}
	if norm.APIKey != "mureka-key-1" || norm.BaseURL != "https://api.mureka.ai" {
		t.Fatalf("normalized = %+v", norm)
	}

	// 类型进 SupportedTypes(唯一的全量类型表)。
	if !channel.SupportedType(channel.TypeMureka) {
		t.Fatal("mureka not in SupportedTypes")
	}

	// BaseURL 必填(裸域名口径,46 号票勘误:版本路径不进 BaseURL)。
	if _, err := (channel.Input{Name: "a", Type: channel.TypeMureka, APIKey: "k"}).Normalize(true); err == nil {
		t.Fatal("mureka without BaseURL should fail")
	}
	// 创建时密钥必填(单 key,不落 config)。
	if _, err := (channel.Input{Name: "a", Type: channel.TypeMureka,
		BaseURL: "https://api.mureka.ai"}).Normalize(true); err == nil {
		t.Fatal("mureka without api_key should fail on create")
	}
}

func TestMurekaConnectivityProbeByVerdict(t *testing.T) {
	cases := []struct {
		name   string
		status int
		answer string
		wantOK bool
		wantIn string // detail 或 error 里应包含的片段
	}{
		{
			// 探测的任务 ID 必不存在:业务级 400/404 证明 Bearer 全链走通。
			name:   "任务不存在=连通成功",
			status: http.StatusNotFound,
			answer: `{"error":{"message":"task not found"}}`,
			wantOK: true,
			wantIn: "凭据与网络正常",
		},
		{
			name:   "400 业务错=连通成功",
			status: http.StatusBadRequest,
			answer: `{"error":{"message":"invalid task id"}}`,
			wantOK: true,
			wantIn: "凭据与网络正常",
		},
		{
			name:   "401=凭据失败",
			status: http.StatusUnauthorized,
			answer: `{"error":{"message":"invalid api key"}}`,
			wantOK: false,
			wantIn: "凭据失败",
		},
		{
			name:   "403=凭据失败",
			status: http.StatusForbidden,
			answer: `{"error":{"message":"forbidden"}}`,
			wantOK: false,
			wantIn: "凭据失败",
		},
		{
			name:   "5xx=如实报错",
			status: http.StatusBadGateway,
			answer: `{"error":{"message":"upstream busy"}}`,
			wantOK: false,
			wantIn: "502",
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
			ch := channel.Channel{Type: channel.TypeMureka, BaseURL: srv.URL, APIKey: "mureka-key"}
			result := tester.Test(context.Background(), ch)
			if result.OK != tc.wantOK {
				t.Fatalf("ok = %v (detail=%q error=%q), want %v", result.OK, result.Detail, result.Error, tc.wantOK)
			}
			if sawMethod != http.MethodGet || !strings.Contains(sawPath, "/v1/song/query/") || !strings.Contains(sawPath, "infinitechance-probe-not-a-task") {
				t.Fatalf("probe = %s %s, want GET {base}/v1/song/query/infinitechance-probe-not-a-task", sawMethod, sawPath)
			}
			if sawAuth != "Bearer mureka-key" {
				t.Fatalf("probe auth = %q, want the Bearer key", sawAuth)
			}
			combined := result.Detail + result.Error
			if !strings.Contains(combined, tc.wantIn) {
				t.Fatalf("verdict %q missing %q", combined, tc.wantIn)
			}
		})
	}
}

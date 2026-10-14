package channel_test

// 46 号票(dashscope 渠道类型)的测试:Normalize 的类型规则(BaseURL 必填
// 为工作空间域名根、api_key 必填、config 凭据不适用)与类型化连通探测
// (GET 假任务 ID:404 业务错 / 2xx 含 UNKNOWN 体 = 连通成功,401 = 凭据
// 失败,其余如实报)。ark_test.go 同款表驱动。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
)

func TestNormalizeDashScopeTypeRules(t *testing.T) {
	// 合法:BaseURL 是工作空间域名根(不含路径),单 Bearer key 存 api_key 列。
	in := channel.Input{Name: "bailian", Type: channel.TypeDashScope,
		BaseURL: "https://ws123.cn-beijing.maas.aliyuncs.com", APIKey: "  sk-bailian-1  ",
		ModelMap: map[string]string{"minimax-h3": "MiniMax/MiniMax-H3"}}
	norm, err := in.Normalize(true)
	if err != nil {
		t.Fatalf("valid dashscope channel rejected: %v", err)
	}
	if norm.APIKey != "sk-bailian-1" || norm.BaseURL != "https://ws123.cn-beijing.maas.aliyuncs.com" {
		t.Fatalf("normalized = %+v", norm)
	}

	// BaseURL 必填(工作空间域名每人不同,不给缺省)。
	if _, err := (channel.Input{Name: "a", Type: channel.TypeDashScope, APIKey: "k"}).Normalize(true); err == nil {
		t.Fatal("dashscope without BaseURL should fail")
	}
	// 创建时密钥必填(不落在 config)。
	if _, err := (channel.Input{Name: "a", Type: channel.TypeDashScope,
		BaseURL: "https://ws123.cn-beijing.maas.aliyuncs.com"}).Normalize(true); err == nil {
		t.Fatal("dashscope without api_key should fail on create")
	}
	// 尾斜杠修剪(adaptor 拼 /api/v1 路径时不重斜杠)。
	trailing, err := (channel.Input{Name: "a", Type: channel.TypeDashScope,
		BaseURL: "https://ws123.cn-beijing.maas.aliyuncs.com/", APIKey: "k"}).Normalize(false)
	if err != nil || strings.HasSuffix(trailing.BaseURL, "/") {
		t.Fatalf("trailing slash not trimmed: %+v err=%v", trailing, err)
	}
}

func TestDashScopeConnectivityProbeByVerdict(t *testing.T) {
	cases := []struct {
		name   string
		status int
		answer string
		wantOK bool
		wantIn string // detail 或 error 里应包含的片段
	}{
		{
			name:   "任务不存在(404)=连通成功",
			status: http.StatusNotFound,
			answer: `{"code":"NotFoundTaskId","message":"Task not found","request_id":"req-1"}`,
			wantOK: true,
			wantIn: "凭据与网络正常",
		},
		{
			name:   "任务不存在(2xx+UNKNOWN 体)=连通成功",
			status: http.StatusOK,
			answer: `{"request_id":"req-2","output":{"task_id":"ghost","task_status":"UNKNOWN"}}`,
			wantOK: true,
			wantIn: "UNKNOWN",
		},
		{
			name:   "401=凭据失败",
			status: http.StatusUnauthorized,
			answer: `{"code":"InvalidApiKey","message":"Invalid API-key provided.","request_id":"req-3"}`,
			wantOK: false,
			wantIn: "凭据失败",
		},
		{
			name:   "5xx=如实报错",
			status: http.StatusBadGateway,
			answer: `{"code":"InternalError","message":"upstream busy","request_id":"req-4"}`,
			wantOK: false,
			wantIn: "502",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sawMethod, sawPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sawMethod, sawPath = r.Method, r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.answer))
			}))
			defer srv.Close()

			tester := &channel.Tester{Client: srv.Client()}
			ch := channel.Channel{Type: channel.TypeDashScope, BaseURL: srv.URL, APIKey: "sk-bailian"}
			result := tester.Test(context.Background(), ch)
			if result.OK != tc.wantOK {
				t.Fatalf("ok = %v (detail=%q error=%q), want %v", result.OK, result.Detail, result.Error, tc.wantOK)
			}
			if sawMethod != http.MethodGet || !strings.Contains(sawPath, "/api/v1/tasks/") {
				t.Fatalf("probe = %s %s, want GET {base}/api/v1/tasks/{id}", sawMethod, sawPath)
			}
			combined := result.Detail + result.Error
			if !strings.Contains(combined, tc.wantIn) {
				t.Fatalf("verdict %q missing %q", combined, tc.wantIn)
			}
		})
	}

	// 密钥缺失:直接可判定的失败,不拨号。
	tester := &channel.Tester{}
	ch := channel.Channel{Type: channel.TypeDashScope, BaseURL: "https://ws.example.com"}
	if result := tester.Test(context.Background(), ch); result.OK || !strings.Contains(result.Error, "密钥") {
		t.Fatalf("missing-key verdict = %+v", result)
	}
}

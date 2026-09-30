package channel_test

// 25 号票(volcengine-ark 渠道类型)的测试:Normalize 的类型规则(BaseURL
// 必填含版本路径、api_key 必填、config 凭据不适用)与类型化连通探测
// (GET 假任务 ID:404 业务错 = 连通成功,401 = 凭据失败,其余如实报)。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
)

func TestNormalizeVolcengineArkTypeRules(t *testing.T) {
	// 合法:BaseURL 含版本路径,单 Bearer key 存 api_key 列。
	in := channel.Input{Name: "ark", Type: channel.TypeVolcengineArk,
		BaseURL: "https://ark.cn-beijing.volcengine.com/api/v3", APIKey: "  ark-key-1  ",
		ModelMap: map[string]string{"seedance-2.5": "doubao-seedance-2-5-250929"}}
	norm, err := in.Normalize(true)
	if err != nil {
		t.Fatalf("valid ark channel rejected: %v", err)
	}
	if norm.APIKey != "ark-key-1" || norm.BaseURL != "https://ark.cn-beijing.volcengine.com/api/v3" {
		t.Fatalf("normalized = %+v", norm)
	}

	// BaseURL 必填(与 openai 同款:换 region 即换 BaseURL,不给缺省)。
	if _, err := (channel.Input{Name: "a", Type: channel.TypeVolcengineArk, APIKey: "k"}).Normalize(true); err == nil {
		t.Fatal("ark without BaseURL should fail")
	}
	// 创建时密钥必填(不落在 config)。
	if _, err := (channel.Input{Name: "a", Type: channel.TypeVolcengineArk,
		BaseURL: "https://ark.cn-beijing.volcengine.com/api/v3"}).Normalize(true); err == nil {
		t.Fatal("ark without api_key should fail on create")
	}
	// 尾斜杠修剪。
	trailing, err := (channel.Input{Name: "a", Type: channel.TypeVolcengineArk,
		BaseURL: "https://ark.cn-beijing.volcengine.com/api/v3/", APIKey: "k"}).Normalize(false)
	if err != nil || strings.HasSuffix(trailing.BaseURL, "/") {
		t.Fatalf("trailing slash not trimmed: %+v err=%v", trailing, err)
	}
}

func TestArkConnectivityProbeByVerdict(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		answer  string
		wantOK  bool
		wantIn  string // detail 或 error 里应包含的片段
	}{
		{
			name:   "任务不存在=连通成功",
			status: http.StatusNotFound,
			answer: `{"error":{"code":"ResourceNotFound","message":"task not found"}}`,
			wantOK: true,
			wantIn: "凭据与网络正常",
		},
		{
			name:   "401=凭据失败",
			status: http.StatusUnauthorized,
			answer: `{"error":{"code":"AuthenticationError","message":"invalid api key"}}`,
			wantOK: false,
			wantIn: "凭据失败",
		},
		{
			name:   "5xx=如实报错",
			status: http.StatusBadGateway,
			answer: `{"error":{"code":"InternalError","message":"upstream busy"}}`,
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
			ch := channel.Channel{Type: channel.TypeVolcengineArk, BaseURL: srv.URL, APIKey: "ark-key"}
			result := tester.Test(context.Background(), ch)
			if result.OK != tc.wantOK {
				t.Fatalf("ok = %v (detail=%q error=%q), want %v", result.OK, result.Detail, result.Error, tc.wantOK)
			}
			if sawMethod != http.MethodGet || !strings.Contains(sawPath, "/contents/generations/tasks/") {
				t.Fatalf("probe = %s %s, want GET {base}/contents/generations/tasks/{id}", sawMethod, sawPath)
			}
			combined := result.Detail + result.Error
			if !strings.Contains(combined, tc.wantIn) {
				t.Fatalf("verdict %q missing %q", combined, tc.wantIn)
			}
		})
	}

	// 密钥缺失:直接可判定的失败,不拨号。
	tester := &channel.Tester{}
	ch := channel.Channel{Type: channel.TypeVolcengineArk, BaseURL: "https://ark.example/api/v3"}
	if result := tester.Test(context.Background(), ch); result.OK || !strings.Contains(result.Error, "密钥") {
		t.Fatalf("missing-key verdict = %+v", result)
	}
}

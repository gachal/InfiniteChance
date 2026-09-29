package channel_test

// 20 号票(tencent-vod 渠道类型)的测试:Normalize 的类型感知(BaseURL
// 可空、config 凭据必填)、admin API 的 config 掩码回显与「留空保留」、
// 类型化连通探测、MySQL config 列往返。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
)

func vodChannelBody() map[string]any {
	return map[string]any{
		"name":     "tencent-vod-main",
		"type":     "tencent-vod",
		"base_url": "",
		"api_key":  "",
		"config": map[string]string{
			"secret_id":  "AKIDtest123456",
			"secret_key": "vodSecretKey789",
			"sub_app_id": "1500000000",
			"region":     "ap-guangzhou",
		},
		"model_map": map[string]string{"og-image-2.5": "OG image2.5_sunburst"},
		"priority":  5,
		"weight":    1,
		"enabled":   true,
	}
}

func TestNormalizeTencentVodTypeRules(t *testing.T) {
	// tencent-vod:BaseURL 可空,api_key 可空,凭据在 config。
	in := channel.Input{Name: "v", Type: channel.TypeTencentVod, BaseURL: "",
		Config: map[string]string{"secret_id": "AKIDx", "secret_key": "k", "sub_app_id": " 150 ", "region": " ap-guangzhou "}}
	norm, err := in.Normalize(true)
	if err != nil {
		t.Fatalf("tencent-vod create without BaseURL: %v", err)
	}
	if norm.Config["sub_app_id"] != "150" || norm.Config["region"] != "ap-guangzhou" {
		t.Fatalf("config values not trimmed: %v", norm.Config)
	}

	// 创建时凭据必须齐。
	noKey := channel.Input{Name: "v", Type: channel.TypeTencentVod, Config: map[string]string{"secret_id": "AKIDx"}}
	if _, err := noKey.Normalize(true); err == nil {
		t.Fatal("create without secret_key should fail")
	}

	// openai 类型维持原规则:BaseURL 必填、api_key 必填。
	blank := channel.Input{Name: "o", Type: channel.TypeOpenAI, APIKey: "sk-x"}
	if _, err := blank.Normalize(true); err == nil {
		t.Fatal("openai without BaseURL should fail")
	}

	// tencent-vod 显式填 BaseURL 时仍校验形状。
	bad := channel.Input{Name: "v", Type: channel.TypeTencentVod, BaseURL: "not-a-url",
		Config: map[string]string{"secret_id": "AKIDx", "secret_key": "k"}}
	if _, err := bad.Normalize(false); err == nil {
		t.Fatal("malformed BaseURL should fail on tencent-vod too")
	}

	// config 键为空拒收。
	emptyKey := channel.Input{Name: "v", Type: channel.TypeTencentVod,
		Config: map[string]string{"": "x", "secret_id": "AKIDx", "secret_key": "k"}}
	if _, err := emptyKey.Normalize(false); err == nil {
		t.Fatal("blank config key should fail")
	}
}

func TestIsSecretConfigKey(t *testing.T) {
	cases := map[string]bool{
		"secret_id": true, "SECRET_KEY": true, "access_key": true, "ApiKey": true,
		"sub_app_id": false, "region": false, "endpoint": false,
	}
	for key, want := range cases {
		if got := channel.IsSecretConfigKey(key); got != want {
			t.Errorf("IsSecretConfigKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestVodChannelConfigMaskedOnEcho(t *testing.T) {
	store := newFakeStore()
	srv := newChannelServer(store)

	w := doJSON(srv, http.MethodPost, "/admin/channels", vodChannelBody())
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		ID          int64             `json:"id"`
		Config      map[string]string `json:"config"`
		ConfigHints map[string]string `json:"config_hints"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 敏感键清空 + 尾 4 位提示;非敏感键原值回显。
	if created.Config["secret_id"] != "" || created.Config["secret_key"] != "" {
		t.Fatalf("secrets leaked: %v", created.Config)
	}
	if h := created.ConfigHints["secret_id"]; !strings.HasSuffix(h, "3456") {
		t.Fatalf("secret_id hint = %q", h)
	}
	if created.Config["sub_app_id"] != "1500000000" || created.Config["region"] != "ap-guangzhou" {
		t.Fatalf("non-secret config not echoed: %v", created.Config)
	}
	if strings.Contains(w.Body.String(), "vodSecretKey789") || strings.Contains(w.Body.String(), "AKIDtest123456") {
		t.Fatalf("secret value leaked in response: %s", w.Body.String())
	}

	// 更新:敏感键留空 = 保留已存值;非敏感键可改。
	body := vodChannelBody()
	body["config"] = map[string]string{
		"secret_id": "", "secret_key": "", "sub_app_id": "1500000001", "region": "",
	}
	w = doJSON(srv, http.MethodPut, "/admin/channels/1", body)
	if w.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", w.Code, w.Body.String())
	}
	stored, err := store.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Config["secret_id"] != "AKIDtest123456" || stored.Config["secret_key"] != "vodSecretKey789" {
		t.Fatalf("blank secrets should keep stored values, got %v", stored.Config)
	}
	if stored.Config["sub_app_id"] != "1500000001" {
		t.Fatalf("sub_app_id update lost: %v", stored.Config)
	}
}

func TestVodConnectivityProbeByVerdict(t *testing.T) {
	cases := []struct {
		name   string
		answer string
		wantOK bool
		wantIn string // 错误文案或 detail 里应包含的片段
	}{
		{
			name:   "业务错=连通成功",
			answer: `{"Response":{"Error":{"Code":"ResourceNotFound.TaskIdNotFound","Message":"任务不存在"}}}`,
			wantOK: true,
			wantIn: "ResourceNotFound.TaskIdNotFound",
		},
		{
			name:   "签名失败=凭据失败",
			answer: `{"Response":{"Error":{"Code":"AuthFailure.SignatureFailure","Message":"签名不匹配"}}}`,
			wantOK: false,
			wantIn: "AuthFailure",
		},
		{
			name:   "SubAppId 不被接受",
			answer: `{"Response":{"Error":{"Code":"InvalidParameter.SubAppId","Message":"sub app not found"}}}`,
			wantOK: false,
			wantIn: "SubAppId",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sawAction string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sawAction = r.Header.Get("X-TC-Action")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.answer))
			}))
			defer srv.Close()

			tester := &channel.Tester{Client: srv.Client()}
			ch := channel.Channel{Type: channel.TypeTencentVod, BaseURL: srv.URL,
				Config: map[string]string{"secret_id": "AKIDx", "secret_key": "k", "sub_app_id": "150"}}
			result := tester.Test(context.Background(), ch)
			if result.OK != tc.wantOK {
				t.Fatalf("ok = %v (detail=%q error=%q), want %v", result.OK, result.Detail, result.Error, tc.wantOK)
			}
			if sawAction != "DescribeTaskDetail" {
				t.Fatalf("probe action = %q", sawAction)
			}
			combined := result.Detail + result.Error
			if !strings.Contains(combined, tc.wantIn) {
				t.Fatalf("verdict %q missing %q", combined, tc.wantIn)
			}
		})
	}

	// 凭据缺失:直接可判定的失败,不拨号。
	tester := &channel.Tester{}
	ch := channel.Channel{Type: channel.TypeTencentVod, Config: map[string]string{}}
	if result := tester.Test(context.Background(), ch); result.OK || !strings.Contains(result.Error, "secret_id") {
		t.Fatalf("missing-credentials verdict = %+v", result)
	}
}

// TestMySQLChannelConfigRoundTrip pins the new JSON column through the real
// store (skips when the local test MySQL is unreachable).
func TestMySQLChannelConfigRoundTrip(t *testing.T) {
	store, _ := openTestDB(t)
	ctx := context.Background()

	created, err := store.Create(ctx, channel.Channel{
		Name: "vod-rt", Type: channel.TypeTencentVod, BaseURL: "",
		Config:       map[string]string{"secret_id": "AKIDrt", "secret_key": "rt-secret", "sub_app_id": "150"},
		ModelMap:     map[string]string{"m": "OG image2_low"},
		Capabilities: []channel.Capability{channel.CapImages},
		Priority:     1, Weight: 1, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Config["secret_id"] != "AKIDrt" || got.Config["sub_app_id"] != "150" {
		t.Fatalf("config roundtrip: %v", got.Config)
	}

	// 更新替换 config(合并保留在 handler,store 只管整列替换)。
	got.Config = map[string]string{"secret_id": "AKIDrt2", "secret_key": "rt-secret2", "region": "ap-shanghai"}
	if _, err := store.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	again, _ := store.Get(ctx, created.ID)
	if again.Config["region"] != "ap-shanghai" || again.Config["secret_id"] != "AKIDrt2" {
		t.Fatalf("config update: %v", again.Config)
	}
}

// 裸 API 只改别的字段、请求体不带 config 时,已存凭据必须原样保留
// (与 api_key 缺省 = 保留对称;config 为 nil 而非空串)。
func TestVodUpdateWithoutConfigKeepsStored(t *testing.T) {
	store := newFakeStore()
	srv := newChannelServer(store)
	if w := doJSON(srv, http.MethodPost, "/admin/channels", vodChannelBody()); w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}

	body := map[string]any{
		"name": "tencent-vod-main", "type": "tencent-vod", "base_url": "",
		"model_map": map[string]string{"og-image-2.5": "OG image2.5_sunburst"},
		"priority":  9, "weight": 1, "enabled": true, // 不带 config 键
	}
	if w := doJSON(srv, http.MethodPut, "/admin/channels/1", body); w.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", w.Code, w.Body.String())
	}
	stored, err := store.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Config["secret_id"] != "AKIDtest123456" || stored.Config["sub_app_id"] != "1500000000" {
		t.Fatalf("absent config should keep stored values, got %v", stored.Config)
	}
	if stored.Priority != 9 {
		t.Fatalf("priority update lost: %d", stored.Priority)
	}
}

package tc3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 黄金向量由参考实现(Python,与腾讯云 TC3 文档同构的签名器)在固定
// 时间戳下生成:同一输入必须逐字节复现同一 Signature,签名回归即刻可见。
func TestSignGoldenVectors(t *testing.T) {
	ts := time.Unix(1759142400, 0).UTC() // 2025-09-29T12:00:00Z
	body := []byte(`{"SubAppId": 1, "ModelName": "OG", "ModelVersion": "image2.5_sunburst", "Prompt": "a cat"}`)

	headers := Sign("AKIDtest123", "secretKEY456", "ap-guangzhou", "vod",
		"vod.tencentcloudapi.com", "CreateAigcImageTask", "2018-07-17", ts, body)

	const wantWithRegion = "TC3-HMAC-SHA256 Credential=AKIDtest123/2025-09-29/vod/tc3_request, " +
		"SignedHeaders=content-type;host;x-tc-action;x-tc-region;x-tc-timestamp;x-tc-version, " +
		"Signature=61f3a00effed9e5d4c835ce258a5654657db5604a77dd0c19878875b64cd90b8"
	if got := headers["Authorization"]; got != wantWithRegion {
		t.Fatalf("Authorization with region:\n got %s\nwant %s", got, wantWithRegion)
	}
	if got := headers["x-tc-timestamp"]; got != "1759142400" {
		t.Fatalf("x-tc-timestamp = %q, want 1759142400", got)
	}

	headers = Sign("AKIDtest123", "secretKEY456", "", "vod",
		"vod.tencentcloudapi.com", "DescribeTaskDetail", "2018-07-17", ts, []byte(`{}`))
	const wantNoRegion = "TC3-HMAC-SHA256 Credential=AKIDtest123/2025-09-29/vod/tc3_request, " +
		"SignedHeaders=content-type;host;x-tc-action;x-tc-timestamp;x-tc-version, " +
		"Signature=ac8c0214f1dc1f2407fa8cfa827e1dbdae9cfa9a8b5d859cb62b78a929bbe299"
	if got := headers["Authorization"]; got != wantNoRegion {
		t.Fatalf("Authorization without region:\n got %s\nwant %s", got, wantNoRegion)
	}
}

func TestCallSignsAndPosts(t *testing.T) {
	var seenAuth, seenAction string
	var seenBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenAction = r.Header.Get("X-TC-Action")
		seenBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Response":{"TaskId":"t-1"}}`))
	}))
	defer srv.Close()

	status, body, err := Call(context.Background(), srv.Client(), srv.URL,
		"CreateAigcImageTask", "2018-07-17", "vod", "AKIDx", "k", "", []byte(`{"Prompt":"a cat"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	var env struct {
		Response struct {
			TaskId string `json:"TaskId"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Response.TaskId != "t-1" {
		t.Fatalf("body = %s (err %v), want Response.TaskId t-1", body, err)
	}
	if seenAction != "CreateAigcImageTask" {
		t.Fatalf("X-TC-Action = %q", seenAction)
	}
	if len(seenAuth) == 0 || seenAuth[:16] != "TC3-HMAC-SHA256 " {
		t.Fatalf("Authorization not signed: %q", seenAuth)
	}
	if string(seenBody) != `{"Prompt":"a cat"}` {
		t.Fatalf("body bytes changed between signing and sending: %q", seenBody)
	}
}

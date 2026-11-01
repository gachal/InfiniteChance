package canvas_test

// IP 企划面的 HTTP 契约(69 号票画面风格):handler 挂真 SQLite IP store
// (newSQLiteIPWorld),在 /ips 路由上验 style 的透出、部分更新、清除与限长。

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/canvas"
)

// ipWire is the response shape the assertions read.
type ipWire struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Style string `json:"style"`
}

func newIPRouter(t *testing.T) *gin.Engine {
	t.Helper()
	ips, _, _ := newSQLiteIPWorld(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	canvas.RegisterIPRoutes(r.Group("/ips"), &canvas.IPHandlers{Store: ips})
	return r
}

func decodeIP(t *testing.T, raw []byte) ipWire {
	t.Helper()
	var ip ipWire
	if err := json.Unmarshal(raw, &ip); err != nil {
		t.Fatalf("decode ip: %v (%s)", err, raw)
	}
	return ip
}

func mustCreateIP(t *testing.T, r *gin.Engine) ipWire {
	t.Helper()
	res := do(r, http.MethodPost, "/ips", `{"name":"星穹之愿","description":"logline"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("create ip = %d %s", res.Code, res.Body.String())
	}
	return decodeIP(t, res.Body.Bytes())
}

// style 透出与部分更新:新建为空;只带 style 的 PATCH 合法(trim 后落库);
// GET 单查与列表都带回;只改名字时风格保持;显式空串清除。
func TestIPHandlersExposeAndPatchStyle(t *testing.T) {
	r := newIPRouter(t)
	created := mustCreateIP(t, r)
	if created.Style != "" {
		t.Fatalf("created style = %q, want empty", created.Style)
	}
	if !strings.Contains(do(r, http.MethodGet, "/ips/1", "").Body.String(), `"style":""`) {
		t.Fatalf("GET body must carry the style key even when empty")
	}

	res := do(r, http.MethodPatch, "/ips/1", `{"style":"  赛博朋克霓虹,高对比冷暖撞色  "}`)
	if res.Code != http.StatusOK {
		t.Fatalf("patch style = %d %s", res.Code, res.Body.String())
	}
	if got := decodeIP(t, res.Body.Bytes()).Style; got != "赛博朋克霓虹,高对比冷暖撞色" {
		t.Fatalf("patched style = %q, want trimmed value", got)
	}
	if got := decodeIP(t, do(r, http.MethodGet, "/ips/1", "").Body.Bytes()).Style; got != "赛博朋克霓虹,高对比冷暖撞色" {
		t.Fatalf("GET style = %q", got)
	}
	var list struct {
		IPs []ipWire `json:"ips"`
	}
	if err := json.Unmarshal(do(r, http.MethodGet, "/ips", "").Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.IPs) != 1 || list.IPs[0].Style != "赛博朋克霓虹,高对比冷暖撞色" {
		t.Fatalf("list = %+v, want the style on the row", list.IPs)
	}

	renamed := decodeIP(t, do(r, http.MethodPatch, "/ips/1", `{"name":"星穹之愿 2"}`).Body.Bytes())
	if renamed.Name != "星穹之愿 2" || renamed.Style != "赛博朋克霓虹,高对比冷暖撞色" {
		t.Fatalf("rename = %+v, want style kept", renamed)
	}

	cleared := do(r, http.MethodPatch, "/ips/1", `{"style":"   "}`)
	if cleared.Code != http.StatusOK || decodeIP(t, cleared.Body.Bytes()).Style != "" {
		t.Fatalf("clear style = %d %s, want empty style", cleared.Code, cleared.Body.String())
	}
}

// 限长:风格超 500 字整单 400(invalid_request),库内值不动。
func TestIPHandlersRejectOverlongStyle(t *testing.T) {
	r := newIPRouter(t)
	mustCreateIP(t, r)
	if res := do(r, http.MethodPatch, "/ips/1", `{"style":"水彩手绘"}`); res.Code != http.StatusOK {
		t.Fatalf("seed style = %d %s", res.Code, res.Body.String())
	}

	body, _ := json.Marshal(map[string]string{"style": strings.Repeat("风", 501)})
	res := do(r, http.MethodPatch, "/ips/1", string(body))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("overlong style = %d %s, want 400", res.Code, res.Body.String())
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &env); err != nil || env.Error.Code != "invalid_request" {
		t.Fatalf("error envelope = %s, want invalid_request", res.Body.String())
	}
	if !strings.Contains(env.Error.Message, "500") {
		t.Errorf("message = %q, want the 500-rune limit spelled out", env.Error.Message)
	}
	if got := decodeIP(t, do(r, http.MethodGet, "/ips/1", "").Body.Bytes()).Style; got != "水彩手绘" {
		t.Fatalf("style after rejected patch = %q, want untouched", got)
	}

	// 恰好 500 字放行(按 rune 计,中文不按字节裁)。
	body, _ = json.Marshal(map[string]string{"style": strings.Repeat("风", 500)})
	if res := do(r, http.MethodPatch, "/ips/1", string(body)); res.Code != http.StatusOK {
		t.Fatalf("500-rune style = %d %s, want 200", res.Code, res.Body.String())
	}
}

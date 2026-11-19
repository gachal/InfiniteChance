package settings_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/settings"
)

// decodePet 取响应里的 pet 对象(92 号票桌宠配置)。
func decodePet(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var parsed struct {
		Pet map[string]any `json:"pet"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return parsed.Pet
}

// 缺行 GET 回缺省 pet 配置(空表单可渲染),不是 404 —— 照 storage 先例。
func TestGetPetMissingRowAnswersDefault(t *testing.T) {
	env := newHandlerEnv()

	w := env.do(t, http.MethodGet, "/admin/settings/pet", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	pet := decodePet(t, w.Body.Bytes())
	if pet["name"] != "" || pet["greeting"] != "" || pet["model"] != "" {
		t.Fatalf("default pet has non-empty text fields: %v", pet)
	}
	if chips, ok := pet["chips"].([]any); !ok || len(chips) != 0 {
		t.Fatalf("default pet chips = %v, want empty array", pet["chips"])
	}
	if pet["idle_image"] != "" || pet["interactive_image"] != "" {
		t.Fatalf("default pet has image keys: %v", pet)
	}
	if pet["updated_at"] != "" {
		t.Fatalf("updated_at = %v, want empty", pet["updated_at"])
	}
}

// 完整 PUT 落库并原值回显(桌宠配置无密钥,全部字段原样回读)。
func TestPutPetRoundTripEchoesValues(t *testing.T) {
	env := newHandlerEnv()

	input := map[string]any{
		"name":              " 绿毛球 ",
		"greeting":          " 画布上有什么想改的? ",
		"chips":             []string{" 帮我建一组分镜 ", "读一下画布结构"},
		"model":             "chat-main",
		"idle_image":        "uploads/20261119/aaa.gif",
		"interactive_image": "uploads/20261119/bbb.gif",
	}
	w := env.do(t, http.MethodPut, "/admin/settings/pet", input)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	pet := decodePet(t, w.Body.Bytes())
	if pet["name"] != "绿毛球" {
		t.Fatalf("name = %v, want trimmed 绿毛球", pet["name"])
	}
	if pet["greeting"] != "画布上有什么想改的?" {
		t.Fatalf("greeting = %v, want trimmed", pet["greeting"])
	}
	if chips, ok := pet["chips"].([]any); !ok || len(chips) != 2 || chips[0] != "帮我建一组分镜" {
		t.Fatalf("chips = %v, want trimmed two entries", pet["chips"])
	}
	if pet["idle_image"] != "uploads/20261119/aaa.gif" {
		t.Fatalf("idle_image = %v", pet["idle_image"])
	}
	if pet["updated_at"] == "" {
		t.Fatalf("updated_at should be stamped after save")
	}

	// 回读:GET 与 PUT 响应一致(原值回显)。
	w = env.do(t, http.MethodGet, "/admin/settings/pet", nil)
	again := decodePet(t, w.Body.Bytes())
	if again["name"] != pet["name"] || again["idle_image"] != pet["idle_image"] {
		t.Fatalf("GET diverges from PUT response: %v vs %v", again, pet)
	}
}

// 合并语义:PUT 缺省(未携带)的字段沿用已存;显式空串 = 清空。
func TestPutPetPartialUpdateKeepsStoredFields(t *testing.T) {
	env := newHandlerEnv()

	full := map[string]any{
		"name":       "绿毛球",
		"greeting":   "你好",
		"chips":      []string{"画一个角色"},
		"model":      "chat-main",
		"idle_image": "uploads/20261119/aaa.gif",
	}
	if w := env.do(t, http.MethodPut, "/admin/settings/pet", full); w.Code != http.StatusOK {
		t.Fatalf("seed PUT: HTTP %d: %s", w.Code, w.Body.String())
	}

	// 只带 greeting 与 chips:其余字段沿用已存。
	w := env.do(t, http.MethodPut, "/admin/settings/pet", map[string]any{
		"greeting": "换个问候",
		"chips":    []string{"新的指令"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	pet := decodePet(t, w.Body.Bytes())
	if pet["name"] != "绿毛球" || pet["model"] != "chat-main" || pet["idle_image"] != "uploads/20261119/aaa.gif" {
		t.Fatalf("absent fields must keep stored values: %v", pet)
	}
	if pet["greeting"] != "换个问候" {
		t.Fatalf("greeting = %v, want replaced", pet["greeting"])
	}

	// 显式空串/空数组 = 清空(与「未携带 = 不动」相区别)。
	w = env.do(t, http.MethodPut, "/admin/settings/pet", map[string]any{
		"idle_image": "",
		"chips":      []string{},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	pet = decodePet(t, w.Body.Bytes())
	if pet["idle_image"] != "" {
		t.Fatalf("idle_image = %v, want cleared", pet["idle_image"])
	}
	if chips, ok := pet["chips"].([]any); !ok || len(chips) != 0 {
		t.Fatalf("chips = %v, want cleared", pet["chips"])
	}
	if pet["name"] != "绿毛球" {
		t.Fatalf("name = %v, want kept", pet["name"])
	}
}

// 非法配置拒绝:image 键不带 uploads/ 前缀、带穿越段、chips 非字符串数组。
func TestPutPetRejectsInvalidShapes(t *testing.T) {
	env := newHandlerEnv()

	cases := []struct {
		name  string
		input map[string]any
	}{
		{"idle_image 不带 uploads/ 前缀", map[string]any{"idle_image": "canvases/1/2/image.png"}},
		{"interactive_image 带穿越段", map[string]any{"interactive_image": "uploads/../secrets/x.gif"}},
		{"idle_image 绝对路径", map[string]any{"idle_image": "/etc/passwd"}},
		{"chips 里混入数字", map[string]any{"chips": []any{"ok", 7}}},
		{"name 超长", map[string]any{"name": strings.Repeat("桌", 65)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := env.do(t, http.MethodPut, "/admin/settings/pet", tc.input)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("HTTP %d: %s, want 400", w.Code, w.Body.String())
			}
		})
	}
}

// NormalizePetConfig 直接钉测(票面验证点):缺省回落、动图键缺失放行、
// chips 形状收敛(空串剔除、全空规约 nil)。
func TestNormalizePetConfig(t *testing.T) {
	// 缺省回落:零值配置合法(未配置 = 前端 CSS 缺省形象)。
	var zero settings.PetConfig
	if err := settings.NormalizePetConfig(&zero); err != nil {
		t.Fatalf("zero config should be valid: %v", err)
	}

	// 动图键缺失:两个 image 键都空 = 合法(纯文案配置)。
	onlyText := settings.PetConfig{Name: "球球", Greeting: "嗨", Chips: []string{"a"}}
	if err := settings.NormalizePetConfig(&onlyText); err != nil {
		t.Fatalf("missing image keys should be valid: %v", err)
	}

	// chips 形状:空串剔除、trim、全空规约成 nil。
	chips := settings.PetConfig{Chips: []string{"  a  ", "", "b"}}
	if err := settings.NormalizePetConfig(&chips); err != nil {
		t.Fatalf("chips normalize: %v", err)
	}
	if len(chips.Chips) != 2 || chips.Chips[0] != "a" || chips.Chips[1] != "b" {
		t.Fatalf("chips = %v, want [a b]", chips.Chips)
	}
	empty := settings.PetConfig{Chips: []string{"", "   "}}
	if err := settings.NormalizePetConfig(&empty); err != nil {
		t.Fatalf("all-empty chips should be valid: %v", err)
	}
	if empty.Chips != nil {
		t.Fatalf("all-empty chips = %v, want nil", empty.Chips)
	}

	// 动图键形状:合法 uploads/ 键放行;非法前缀/穿越拒绝。
	okKey := settings.PetConfig{IdleImage: "uploads/20261119/x.webp"}
	if err := settings.NormalizePetConfig(&okKey); err != nil {
		t.Fatalf("valid uploads key rejected: %v", err)
	}
	for _, bad := range []string{
		"relay/20261119/x.gif", // 不是 uploads/ 前缀
		"uploads/../x.gif",     // 穿越
		"uploads/a\\b.gif",     // 反斜杠
		"/uploads/x.gif",       // 绝对路径
	} {
		badKey := settings.PetConfig{IdleImage: bad}
		if err := settings.NormalizePetConfig(&badKey); err == nil {
			t.Fatalf("idle_image %q should be rejected", bad)
		}
	}
}

// 热生效(照 storage 按请求读表):reader 每次调用都读表,改行后立即可见。
func TestNewPetReaderHotReload(t *testing.T) {
	env := newHandlerEnv()
	reader := settings.NewPetReader(env.store)
	ctx := t.Context()

	cfg, err := reader(ctx)
	if err != nil || cfg.Name != "" {
		t.Fatalf("missing row should answer default: %v %+v", err, cfg)
	}

	input := map[string]any{"name": "热生效球", "chips": []string{"a", "b"}}
	if w := env.do(t, http.MethodPut, "/admin/settings/pet", input); w.Code != http.StatusOK {
		t.Fatalf("seed PUT: HTTP %d: %s", w.Code, w.Body.String())
	}
	cfg, err = reader(ctx)
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	if cfg.Name != "热生效球" || len(cfg.Chips) != 2 {
		t.Fatalf("reader did not see the new row: %+v", cfg)
	}
}

// 坏 JSON 行宽容读:回缺省配置,不让画布面瘫痪(照 NewStorageReader)。
func TestNewPetReaderToleratesCorruptRow(t *testing.T) {
	store := newFakeStore()
	if _, err := store.Put(t.Context(), settings.NamePet, []byte("{not json")); err != nil {
		t.Fatalf("seed corrupt row: %v", err)
	}
	reader := settings.NewPetReader(store)
	cfg, err := reader(t.Context())
	if err != nil {
		t.Fatalf("corrupt row should degrade to default, got error: %v", err)
	}
	if cfg.Name != "" {
		t.Fatalf("corrupt row should answer empty default: %+v", cfg)
	}
}

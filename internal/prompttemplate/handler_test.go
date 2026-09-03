package prompttemplate_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/prompttemplate"
)

// fakeStore is an in-memory Store; handler tests never touch MySQL.
type fakeStore struct {
	mu     sync.Mutex
	nextID int64
	byID   map[int64]prompttemplate.Template
}

func newFakeStore() *fakeStore {
	return &fakeStore{byID: map[int64]prompttemplate.Template{}}
}

func (s *fakeStore) List(context.Context) ([]prompttemplate.Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]prompttemplate.Template, 0, len(s.byID))
	for id := int64(1); id <= s.nextID; id++ {
		if t, ok := s.byID[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *fakeStore) ListEnabled(context.Context) ([]prompttemplate.Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]prompttemplate.Template, 0)
	for id := int64(1); id <= s.nextID; id++ {
		if t, ok := s.byID[id]; ok && t.Enabled {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *fakeStore) Get(_ context.Context, id int64) (prompttemplate.Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.byID[id]
	if !ok {
		return prompttemplate.Template{}, prompttemplate.ErrNotFound
	}
	return t, nil
}

func (s *fakeStore) Create(_ context.Context, t prompttemplate.Template) (prompttemplate.Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	t.ID = s.nextID
	s.byID[t.ID] = t
	return t, nil
}

func (s *fakeStore) Update(_ context.Context, t prompttemplate.Template) (prompttemplate.Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[t.ID]; !ok {
		return prompttemplate.Template{}, prompttemplate.ErrNotFound
	}
	s.byID[t.ID] = t
	return t, nil
}

func (s *fakeStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return prompttemplate.ErrNotFound
	}
	delete(s.byID, id)
	return nil
}

type handlerEnv struct {
	engine *gin.Engine
	store  *fakeStore
}

func newHandlerEnv() handlerEnv {
	gin.SetMode(gin.TestMode)
	store := newFakeStore()
	engine := gin.New()
	group := engine.Group("/admin")
	prompttemplate.RegisterAdminRoutes(group, &prompttemplate.Handlers{Store: store})
	return handlerEnv{engine: engine, store: store}
}

func (env handlerEnv) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(raw))
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, reader)
	env.engine.ServeHTTP(w, req)
	return w
}

func bodyError(t *testing.T, w *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var parsed struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("parse error body %q: %v", w.Body.String(), err)
	}
	return parsed.Error.Code, parsed.Error.Message
}

func TestCreateTemplateReturnsCreatedWithEnabledDefaultTrue(t *testing.T) {
	env := newHandlerEnv()
	w := env.do(t, http.MethodPost, "/admin/prompt-templates", map[string]any{
		"name":     "文生图-中文",
		"template": "请为主题「{topic}」写一段英文文生图提示词,只输出提示词本身。",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", w.Code, w.Body.String())
	}
	var got struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Enabled  bool   `json:"enabled"`
		Template string `json:"template"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("id = %d, want 1", got.ID)
	}
	if !got.Enabled {
		t.Errorf("enabled = false, want default true")
	}
	if got.Template != "请为主题「{topic}」写一段英文文生图提示词,只输出提示词本身。" {
		t.Errorf("template round-trip mismatch: %q", got.Template)
	}
}

func TestCreateTemplateValidation(t *testing.T) {
	cases := []struct {
		name    string
		body    map[string]any
		wantMsg string
	}{
		{
			name:    "missing name",
			body:    map[string]any{"template": "画 {topic}"},
			wantMsg: "模板名称不能为空",
		},
		{
			name:    "missing template",
			body:    map[string]any{"name": "空模板"},
			wantMsg: "模板内容不能为空",
		},
		{
			name:    "template without topic placeholder",
			body:    map[string]any{"name": "无占位", "template": "写一段风景提示词"},
			wantMsg: "模板内容必须包含 {topic} 占位符,生成时会替换为输入的主题",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newHandlerEnv()
			w := env.do(t, http.MethodPost, "/admin/prompt-templates", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
			}
			_, message := bodyError(t, w)
			if message != tc.wantMsg {
				t.Errorf("message = %q, want %q", message, tc.wantMsg)
			}
		})
	}
}

func TestCreateTemplateTrimsAndAcceptsExplicitDisabled(t *testing.T) {
	env := newHandlerEnv()
	disabled := false
	w := env.do(t, http.MethodPost, "/admin/prompt-templates", map[string]any{
		"name":     "  草稿模板  ",
		"template": "  主题:{topic}  ",
		"enabled":  &disabled,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", w.Code, w.Body.String())
	}
	var got struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if got.Name != "草稿模板" {
		t.Errorf("name = %q, want trimmed", got.Name)
	}
	if got.Enabled {
		t.Errorf("enabled = true, want explicit false")
	}
}

func TestListTemplatesWrapsEnvelope(t *testing.T) {
	env := newHandlerEnv()
	for _, name := range []string{"甲", "乙"} {
		if w := env.do(t, http.MethodPost, "/admin/prompt-templates", map[string]any{
			"name": name, "template": "{topic}",
		}); w.Code != http.StatusCreated {
			t.Fatalf("seed create: status = %d", w.Code)
		}
	}
	w := env.do(t, http.MethodGet, "/admin/prompt-templates", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got struct {
		Templates []struct {
			Name string `json:"name"`
		} `json:"templates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if len(got.Templates) != 2 || got.Templates[0].Name != "甲" || got.Templates[1].Name != "乙" {
		t.Errorf("templates = %+v, want 甲 then 乙 (id order)", got.Templates)
	}
}

func TestUpdateTemplateReplacesRow(t *testing.T) {
	env := newHandlerEnv()
	env.do(t, http.MethodPost, "/admin/prompt-templates", map[string]any{
		"name": "旧名", "template": "{topic}",
	})

	w := env.do(t, http.MethodPut, "/admin/prompt-templates/1", map[string]any{
		"name": "新名", "template": "新的 {topic} 指令", "enabled": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var got struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if got.Name != "新名" || got.Enabled {
		t.Errorf("updated = %+v, want 新名/disabled", got)
	}
}

func TestUpdateUnknownTemplateAnswers404(t *testing.T) {
	env := newHandlerEnv()
	w := env.do(t, http.MethodPut, "/admin/prompt-templates/9", map[string]any{
		"name": "任意", "template": "{topic}",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestDeleteTemplateRemovesRow(t *testing.T) {
	env := newHandlerEnv()
	env.do(t, http.MethodPost, "/admin/prompt-templates", map[string]any{
		"name": "待删", "template": "{topic}",
	})

	if w := env.do(t, http.MethodDelete, "/admin/prompt-templates/1", nil); w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", w.Code)
	}
	if w := env.do(t, http.MethodDelete, "/admin/prompt-templates/1", nil); w.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", w.Code)
	}
}

func TestMalformedBodyAnswers400(t *testing.T) {
	env := newHandlerEnv()
	w := env.do(t, http.MethodPost, "/admin/prompt-templates", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPathIDMustBePositiveInteger(t *testing.T) {
	env := newHandlerEnv()
	w := env.do(t, http.MethodPut, "/admin/prompt-templates/not-a-number", map[string]any{
		"name": "任意", "template": "{topic}",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

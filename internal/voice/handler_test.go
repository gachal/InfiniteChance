package voice_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/voice"
)

// stubRemover is the gateway delete slice: records calls and answers from a
// script (UpstreamError 形态透传、普通 error 走 502)。
type stubRemover struct {
	calls   []string
	err     error
	onCall  func(channelID int64, voiceID string) error
}

func (s *stubRemover) DeleteVoice(_ context.Context, channelID int64, voiceID, _ string) error {
	s.calls = append(s.calls, voiceID)
	if s.onCall != nil {
		return s.onCall(channelID, voiceID)
	}
	return s.err
}

type handlerEnv struct {
	engine     *gin.Engine
	store      *voice.SQLiteStore
	characters *character.SQLiteStore
	remover    *stubRemover
}

func newHandlerEnv(t *testing.T) *handlerEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, characters, _ := newSQLiteWorld(t)
	remover := &stubRemover{}
	r := gin.New()
	voice.RegisterRoutes(r.Group("/voices"), &voice.Handlers{Store: store, Gateway: remover})
	return &handlerEnv{engine: r, store: store, characters: characters, remover: remover}
}

// bindCharacter lands a character row bound to the voice (voice_in_use
// 防护的数据面)。
func bindCharacter(t *testing.T, e *handlerEnv, voiceID int64) {
	t.Helper()
	c, err := e.characters.CreateCharacter(context.Background(), 1, character.CharacterInput{Name: "小星"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.characters.UpdateCharacter(context.Background(), c.ID, &character.CharacterUpdate{VoiceID: &voiceID}); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int64) string {
	return strings.TrimSpace(strings.Join([]string{fmtInt(n)}, ""))
}

func fmtInt(n int64) string {
	return strconv.FormatInt(n, 10)
}

func (e *handlerEnv) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func errorCode(t *testing.T, body string) string {
	t.Helper()
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("error body not JSON: %s", body)
	}
	return resp.Error.Code
}

func seedHandlerVoice(t *testing.T, e *handlerEnv, voiceID string) voice.Voice {
	t.Helper()
	v, err := e.store.Create(context.Background(), voice.CreateInput{
		VoiceID: voiceID, ChannelID: 3, TargetModel: "qwen-tts-plus",
		Name: "少年音", SourceMode: voice.SourceModeDesign,
		SourcePrompt: "描述", Status: voice.StatusOK,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// GET /voices 全量;PATCH 改名(仅 name,空与缺参 400)。
func TestHandlerVoiceListAndPatch(t *testing.T) {
	e := newHandlerEnv(t)
	v := seedHandlerVoice(t, e, "vd-list-1")

	w := e.do(t, http.MethodGet, "/voices", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	var list struct {
		Voices []struct {
			ID          int64  `json:"id"`
			VoiceID     string `json:"voice_id"`
			Status      string `json:"status"`
			ChannelID   int64  `json:"channel_id"`
			PreviewAsID int64  `json:"preview_asset_id"`
		} `json:"voices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Voices) != 1 {
		t.Fatalf("list = %s, %v", w.Body.String(), err)
	}
	if list.Voices[0].VoiceID != "vd-list-1" || list.Voices[0].Status != voice.StatusOK || list.Voices[0].ChannelID != 3 {
		t.Fatalf("voice = %+v", list.Voices[0])
	}

	w = e.do(t, http.MethodPatch, "/voices/"+itoa(v.ID), `{"name":"清亮少年音"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "清亮少年音") {
		t.Fatalf("patch = %d %s", w.Code, w.Body.String())
	}
	if w = e.do(t, http.MethodPatch, "/voices/"+itoa(v.ID), `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty patch = %d, want 400", w.Code)
	}
	if w = e.do(t, http.MethodPatch, "/voices/"+itoa(v.ID), `{"name":"  "}`); w.Code != http.StatusBadRequest {
		t.Fatalf("blank name = %d, want 400", w.Code)
	}
	if w = e.do(t, http.MethodPatch, "/voices/999", `{"name":"无"}`); w.Code != http.StatusNotFound {
		t.Fatalf("missing voice = %d, want 404", w.Code)
	}
}

// DELETE:先网关删除成功再删行;被角色绑定时 409 voice_in_use 且不触网关;
// 网关拒绝原样透传状态码。
func TestHandlerVoiceDeleteFlow(t *testing.T) {
	e := newHandlerEnv(t)
	v := seedHandlerVoice(t, e, "vd-del-1")

	// 网关 404 透传(上游音色已不在),台账行不动。
	e.remover.onCall = func(int64, string) error {
		return &voice.UpstreamError{Status: http.StatusNotFound, Message: "No such voice."}
	}
	if w := e.do(t, http.MethodDelete, "/voices/"+itoa(v.ID), `{"channel_id":3}`); w.Code != http.StatusNotFound {
		t.Fatalf("upstream 404 = %d, want pass-through", w.Code)
	}
	if _, err := e.store.Get(context.Background(), v.ID); err != nil {
		t.Fatalf("row must survive a failed upstream delete: %v", err)
	}

	// 网关成功 → 行删除,204。
	e.remover.onCall = nil
	if w := e.do(t, http.MethodDelete, "/voices/"+itoa(v.ID), `{"channel_id":3}`); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", w.Code)
	}
	if len(e.remover.calls) != 2 || e.remover.calls[1] != "vd-del-1" {
		t.Fatalf("gateway calls = %v", e.remover.calls)
	}
	if _, err := e.store.Get(context.Background(), v.ID); !errors.Is(err, voice.ErrVoiceNotFound) {
		t.Fatalf("row after delete = %v, want ErrVoiceNotFound", err)
	}

	// 传输层失败 → 502 upstream_error。
	v2 := seedHandlerVoice(t, e, "vd-del-2")
	e.remover.onCall = nil
	e.remover.err = errors.New("dial tcp: connection refused")
	if w := e.do(t, http.MethodDelete, "/voices/"+itoa(v2.ID), `{"channel_id":3}`); w.Code != http.StatusBadGateway {
		t.Fatalf("transport failure = %d, want 502", w.Code)
	}
}

// 删除被角色绑定的音色:409 voice_in_use,网关一次都不触(先本地裁决)。
func TestHandlerVoiceDeleteInUse(t *testing.T) {
	e := newHandlerEnv(t)
	v := seedHandlerVoice(t, e, "vd-inuse")
	bindCharacter(t, e, v.ID)

	e.remover.onCall = func(int64, string) error {
		t.Fatalf("gateway must not be called for an in-use voice")
		return nil
	}
	w := e.do(t, http.MethodDelete, "/voices/"+itoa(v.ID), "")
	if w.Code != http.StatusConflict || errorCode(t, w.Body.String()) != "voice_in_use" {
		t.Fatalf("delete in-use = %d %s, want 409 voice_in_use", w.Code, w.Body.String())
	}
}

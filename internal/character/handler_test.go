package character_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/voice"
)

// fakeStore is the in-memory Store double; handler tests run at the HTTP
// seam without either database. 保序:characters 按 updated_at DESC,
// variants/images 按 sort+id。
type fakeStore struct {
	mu        sync.Mutex
	character map[int64]character.Character
	nextID    int64
	lastVariantDelete int64 // 最近一次 DeleteVariant 的入参(断言路由绑定)
}

func newFakeStore() *fakeStore {
	return &fakeStore{character: map[int64]character.Character{}, nextID: 1}
}

// seed inserts one character with its default variant. 不加锁 —— 内部助手,
// 调用方持锁(CreateCharacter)或单线程测试装配直接调用。
func (f *fakeStore) seed(ipID int64, name string) character.Character {
	now := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	c := character.Character{
		ID: f.nextID, IPID: ipID, Name: name,
		Variants: []character.Variant{{
			ID: f.nextID + 1, CharacterID: f.nextID, Name: "默认",
			Images: []character.Image{},
		}},
		CreatedAt: now, UpdatedAt: now,
	}
	f.nextID += 2
	f.character[c.ID] = c
	return c
}

func (f *fakeStore) CreateCharacter(_ context.Context, ipID int64, in character.CharacterInput) (character.Character, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.seed(ipID, in.Name)
	c.Alias = in.Alias
	c.Description = in.Description
	c.Detail = in.Detail
	f.character[c.ID] = c
	return c, nil
}

func (f *fakeStore) ListCharacters(_ context.Context, ipID int64) ([]character.Character, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []character.Character
	for _, c := range f.character {
		if c.IPID == ipID {
			out = append(out, c)
		}
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j].UpdatedAt.After(out[i].UpdatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (f *fakeStore) GetCharacter(_ context.Context, id int64) (character.Character, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.character[id]
	if !ok {
		return character.Character{}, character.ErrCharacterNotFound
	}
	return c, nil
}

func (f *fakeStore) UpdateCharacter(_ context.Context, id int64, u *character.CharacterUpdate) (character.Character, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.character[id]
	if !ok {
		return character.Character{}, character.ErrCharacterNotFound
	}
	if u.Name != nil {
		c.Name = *u.Name
	}
	if u.Alias != nil {
		c.Alias = *u.Alias
	}
	if u.Description != nil {
		c.Description = *u.Description
	}
	if u.Detail != nil {
		c.Detail = *u.Detail
	}
	if u.VoiceID != nil {
		c.VoiceID = *u.VoiceID
	}
	f.character[id] = c
	return c, nil
}

func (f *fakeStore) DeleteCharacter(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.character[id]; !ok {
		return character.ErrCharacterNotFound
	}
	delete(f.character, id)
	return nil
}

func (f *fakeStore) CreateVariant(_ context.Context, characterID int64, name, note string) (character.Variant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.character[characterID]
	if !ok {
		return character.Variant{}, character.ErrCharacterNotFound
	}
	v := character.Variant{
		ID: f.nextID, CharacterID: characterID, Name: name, Note: note,
		Images: []character.Image{},
	}
	f.nextID++
	c.Variants = append(c.Variants, v)
	f.character[characterID] = c
	return v, nil
}

func (f *fakeStore) UpdateVariant(_ context.Context, id int64, u *character.VariantUpdate) (character.Variant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for cid, c := range f.character {
		for i := range c.Variants {
			if c.Variants[i].ID != id {
				continue
			}
			if u.Name != nil {
				c.Variants[i].Name = *u.Name
			}
			if u.Note != nil {
				c.Variants[i].Note = *u.Note
			}
			if u.Sort != nil {
				c.Variants[i].Sort = *u.Sort
			}
			f.character[cid] = c
			return c.Variants[i], nil
		}
	}
	return character.Variant{}, character.ErrVariantNotFound
}

func (f *fakeStore) DeleteVariant(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastVariantDelete = id
	for cid, c := range f.character {
		if len(c.Variants) == 0 {
			continue
		}
		found := -1
		for i := range c.Variants {
			if c.Variants[i].ID == id {
				found = i
				break
			}
		}
		if found < 0 {
			continue
		}
		if len(c.Variants) <= 1 {
			return character.ErrLastVariant
		}
		c.Variants = append(c.Variants[:found], c.Variants[found+1:]...)
		f.character[cid] = c
		return nil
	}
	return character.ErrVariantNotFound
}

func (f *fakeStore) CreateImage(_ context.Context, characterID int64, in character.ImageInput) (character.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.character[characterID]
	if !ok {
		return character.Image{}, character.ErrCharacterNotFound
	}
	for i := range c.Variants {
		if c.Variants[i].ID != in.VariantID {
			continue
		}
		img := character.Image{
			ID: f.nextID, CharacterID: characterID, VariantID: in.VariantID,
			AssetID: in.AssetID, Label: in.Label, Sort: in.Sort,
		}
		f.nextID++
		c.Variants[i].Images = append(c.Variants[i].Images, img)
		f.character[characterID] = c
		return img, nil
	}
	return character.Image{}, character.ErrVariantNotFound
}

func (f *fakeStore) UpdateImage(_ context.Context, id int64, u *character.ImageUpdate) (character.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for cid, c := range f.character {
		for i := range c.Variants {
			for j := range c.Variants[i].Images {
				if c.Variants[i].Images[j].ID != id {
					continue
				}
				img := &c.Variants[i].Images[j]
				if u.VariantID != nil {
					// 跨角色查目标变体:同角色 → 换挂;别的角色 → mismatch;
					// 哪都不在 → not found。
					owner := int64(0)
					for _, oc := range f.character {
						for k := range oc.Variants {
							if oc.Variants[k].ID == *u.VariantID {
								owner = oc.Variants[k].ID
								break
							}
						}
					}
					if owner == 0 {
						return character.Image{}, character.ErrVariantNotFound
					}
					if owner != img.CharacterID {
						return character.Image{}, character.ErrVariantMismatch
					}
					img.VariantID = owner
				}
				if u.Label != nil {
					img.Label = *u.Label
				}
				if u.Sort != nil {
					img.Sort = *u.Sort
				}
				f.character[cid] = c
				return *img, nil
			}
		}
	}
	return character.Image{}, character.ErrImageNotFound
}

func (f *fakeStore) DeleteImage(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for cid, c := range f.character {
		for i := range c.Variants {
			for j := range c.Variants[i].Images {
				if c.Variants[i].Images[j].ID == id {
					c.Variants[i].Images = append(c.Variants[i].Images[:j], c.Variants[i].Images[j+1:]...)
					f.character[cid] = c
					return nil
				}
			}
		}
	}
	return character.ErrImageNotFound
}

func (f *fakeStore) GetImage(_ context.Context, id int64) (character.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.character {
		for i := range c.Variants {
			for j := range c.Variants[i].Images {
				if c.Variants[i].Images[j].ID == id {
					return c.Variants[i].Images[j], nil
				}
			}
		}
	}
	return character.Image{}, character.ErrImageNotFound
}

func (f *fakeStore) GetVariant(_ context.Context, id int64) (character.Variant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.character {
		for i := range c.Variants {
			if c.Variants[i].ID == id {
				return c.Variants[i], nil
			}
		}
	}
	return character.Variant{}, character.ErrVariantNotFound
}

func (f *fakeStore) EnsureSchema(context.Context) error { return nil }

// fakeIPs is the canvas.IPStore double: only the existence seam matters
// (建角色/列角色的 IP 存在性裁决)。
type fakeIPs struct {
	exists map[int64]bool
}

func newFakeIPs() *fakeIPs { return &fakeIPs{exists: map[int64]bool{1: true}} }

func (f *fakeIPs) IPExists(_ context.Context, id int64) (bool, error) {
	return f.exists[id], nil
}

func (f *fakeIPs) ListIPs(context.Context) ([]canvas.IP, error)                  { return nil, nil }
func (f *fakeIPs) GetIP(context.Context, int64) (canvas.IP, error)               { return canvas.IP{}, canvas.ErrIPNotFound }
func (f *fakeIPs) CreateIP(context.Context, string, string, int64) (canvas.IP, error) {
	return canvas.IP{}, nil
}
func (f *fakeIPs) UpdateIP(context.Context, int64, *canvas.IPUpdate) (canvas.IP, error) {
	return canvas.IP{}, nil
}
func (f *fakeIPs) DeleteIP(context.Context, int64) error   { return nil }
func (f *fakeIPs) EnsureSchema(context.Context) error      { return nil }

// newRouter mounts both character route groups without auth (handler tests
// exercise the JSON seam, not the JWT middleware).
func newRouter(store *fakeStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &character.Handlers{Store: store, IPs: newFakeIPs()}
	character.RegisterIPRoutes(r.Group("/ips/:id/characters"), h)
	character.RegisterRoutes(r.Group("/characters"), h)
	character.RegisterVariantRoutes(r.Group("/character-variants"), h)
	character.RegisterImageRoutes(r.Group("/character-images"), h)
	return r
}

func do(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 建角色:201 + 完整树(含默认变体)+ 封面投影字段。断言按 wire 形状
// (snake_case)解码,verify 的是 JSON 缝而非 Go 类型。
func TestHandlerCreateCharacter(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)

	w := do(r, "POST", "/ips/1/characters", `{"name":"小星","alias":"星酱","description":"一句话","detail":"详细设定"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	var created struct {
		ID           int64  `json:"id"`
		IPID         int64  `json:"ip_id"`
		Name         string `json:"name"`
		Alias        string `json:"alias"`
		Description  string `json:"description"`
		Detail       string `json:"detail"`
		CoverAssetID int64  `json:"cover_asset_id"`
		Variants     []struct {
			Name string `json:"name"`
		} `json:"variants"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "小星" || created.IPID != 1 || created.Alias != "星酱" ||
		created.Description != "一句话" || created.Detail != "详细设定" ||
		len(created.Variants) != 1 || created.Variants[0].Name != "默认" {
		t.Fatalf("created = %+v", created)
	}

	// 校验:名字必填。
	if w := do(r, "POST", "/ips/1/characters", `{"name":"  "}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty name status = %d", w.Code)
	}
	// 校验:超长名字(256 runes)。
	long := strings.Repeat("星", 256)
	if w := do(r, "POST", "/ips/1/characters", `{"name":"`+long+`"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("long name status = %d", w.Code)
	}
	// 跨 IP:不存在的 IP 拒建(404)。
	if w := do(r, "POST", "/ips/99/characters", `{"name":"小星"}`); w.Code != http.StatusNotFound {
		t.Fatalf("missing ip status = %d", w.Code)
	}
	// 列表同样验 IP 存在性。
	if w := do(r, "GET", "/ips/99/characters", ""); w.Code != http.StatusNotFound {
		t.Fatalf("list missing ip status = %d", w.Code)
	}
}

// 列表树形状:characters 数组、变体/图嵌套、cover_asset_id 投影。
func TestHandlerListTreeShape(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)
	c := store.seed(1, "小星")

	// 手工给默认变体挂一张图(走 fake 内部,模拟 store 已组好的树)。
	if _, err := store.CreateImage(context.Background(), c.ID, character.ImageInput{
		VariantID: c.Variants[0].ID, AssetID: 77, Label: "三视图",
	}); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	w := do(r, "GET", "/ips/1/characters", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Characters []struct {
			ID           int64  `json:"id"`
			Name         string `json:"name"`
			CoverAssetID int64  `json:"cover_asset_id"`
			Variants     []struct {
				Name   string `json:"name"`
				Images []struct {
					AssetID int64  `json:"asset_id"`
					Label   string `json:"label"`
				} `json:"images"`
			} `json:"variants"`
		} `json:"characters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Characters) != 1 {
		t.Fatalf("characters = %+v", body.Characters)
	}
	got := body.Characters[0]
	if got.ID != c.ID || got.Name != "小星" || got.CoverAssetID != 77 {
		t.Fatalf("character = %+v", got)
	}
	if len(got.Variants) != 1 || got.Variants[0].Name != "默认" || len(got.Variants[0].Images) != 1 || got.Variants[0].Images[0].AssetID != 77 {
		t.Fatalf("tree = %+v", got.Variants)
	}
}

// 最后一个变体不可删 → 409 variant_last_one(管理面统一错误形状)。
func TestHandlerLastVariantConflict(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)
	c := store.seed(1, "小星")

	w := do(r, "DELETE", "/character-variants/"+itoa(c.Variants[0].ID), "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody.Error.Code != "variant_last_one" {
		t.Fatalf("code = %q, want variant_last_one", errBody.Error.Code)
	}
}

// 图操作错误映射:换变体跨角色 400;不存在的实体 404;删角色 204。
func TestHandlerImageErrorsAndDelete(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)
	c := store.seed(1, "小星")
	img, err := store.CreateImage(context.Background(), c.ID, character.ImageInput{
		VariantID: c.Variants[0].ID, AssetID: 5,
	})
	if err != nil {
		t.Fatalf("seed image: %v", err)
	}
	other := store.seed(1, "阿月")

	// 换变体到别的角色 → 400。
	body := `{"variant_id":` + itoa(other.Variants[0].ID) + `}`
	if w := do(r, "PATCH", "/character-images/"+itoa(img.ID), body); w.Code != http.StatusBadRequest {
		t.Fatalf("cross-character move status = %d body %s", w.Code, w.Body.String())
	}
	// 正常 PATCH label → 200 回图行。
	if w := do(r, "PATCH", "/character-images/"+itoa(img.ID), `{"label":"色板"}`); w.Code != http.StatusOK {
		t.Fatalf("patch label status = %d", w.Code)
	}
	if _, err := store.GetImage(context.Background(), img.ID); err != nil {
		t.Fatalf("get image: %v", err)
	}
	// 不存在的图 → 404。
	if w := do(r, "PATCH", "/character-images/999", `{"label":"x"}`); w.Code != http.StatusNotFound {
		t.Fatalf("missing image status = %d", w.Code)
	}
	// 删角色 → 204,再取 404。
	if w := do(r, "DELETE", "/characters/"+itoa(c.ID), ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete character status = %d", w.Code)
	}
	if w := do(r, "GET", "/characters/"+itoa(c.ID), ""); w.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d", w.Code)
	}
	// 畸形请求体 → 400。
	if w := do(r, "POST", "/ips/1/characters", `not-json`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d", w.Code)
	}
	// id 0 不是合法路径参数 → 400(而非误导性的 404)。
	if w := do(r, "DELETE", "/characters/0", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("id 0 status = %d", w.Code)
	}
}

// 变体创建:201 回变体行;挂在缺失角色下 404。
func TestHandlerVariantCreate(t *testing.T) {
	store := newFakeStore()
	r := newRouter(store)
	c := store.seed(1, "小星")

	w := do(r, "POST", "/characters/"+itoa(c.ID)+"/variants", `{"name":"校服","note":"藏青色"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	var v struct {
		ID          int64  `json:"id"`
		CharacterID int64  `json:"character_id"`
		Name        string `json:"name"`
		Note        string `json:"note"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Name != "校服" || v.Note != "藏青色" || v.CharacterID != c.ID {
		t.Fatalf("variant = %+v", v)
	}
	if w := do(r, "POST", "/characters/999/variants", `{"name":"x"}`); w.Code != http.StatusNotFound {
		t.Fatalf("missing character status = %d", w.Code)
	}
	if w := do(r, "POST", "/characters/"+itoa(c.ID)+"/variants", `{"name":""}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty variant name status = %d", w.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// 路由形状冒烟:角色四组路由与画布面的 /ips/:id 同树注册(gin 对同层不同
// 名的通配符会在启动期 panic,51 号票的 /ips/:id/characters 必须与 50 号票
// 的 /ips/:id 共用 :id)。注册成功即无冲突。
func TestRouteShapesCoexistWithIPRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &character.Handlers{Store: newFakeStore(), IPs: newFakeIPs()}
	canvas.RegisterIPRoutes(r.Group("/ips"), &canvas.IPHandlers{Store: nil})
	character.RegisterIPRoutes(r.Group("/ips/:id/characters"), h)
	character.RegisterRoutes(r.Group("/characters"), h)
	character.RegisterVariantRoutes(r.Group("/character-variants"), h)
	character.RegisterImageRoutes(r.Group("/character-images"), h)
}

// ---- 绑定实时校准(57 号票评审 F1)----

// fakeVoiceChecker is the VoiceChecker double: 台账行由测试播种。
type fakeVoiceChecker struct {
	rows map[int64]voice.Voice
}

func (f *fakeVoiceChecker) Get(_ context.Context, id int64) (voice.Voice, error) {
	v, ok := f.rows[id]
	if !ok {
		return voice.Voice{}, voice.ErrVoiceNotFound
	}
	return v, nil
}

// fakeVoiceQuerier is the voice.UpstreamQuerier double: 应答由测试给定,
// 调用参数全量留痕(渠道/句柄/审计标记的断言用)。
type fakeVoiceQuerier struct {
	status string
	err    error
	calls  []string // "channelID:voiceID:source"
}

func (f *fakeVoiceQuerier) QueryVoice(_ context.Context, channelID int64, voiceID, source string) (string, error) {
	f.calls = append(f.calls, itoa(channelID)+":"+voiceID+":"+source)
	if f.err != nil {
		return "", f.err
	}
	return f.status, nil
}

// newVoiceRouter mounts the character routes with the voices checker and the
// gateway querier wired (绑定校准的完整装配,与 wiring 的生产姿势同构)。
// querier 为 nil 时 Gateway 保持 nil 接口(生产「未装配」语义,不是
// typed-nil 陷阱)。
func newVoiceRouter(store *fakeStore, checker *fakeVoiceChecker, querier *fakeVoiceQuerier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var gateway voice.UpstreamQuerier
	if querier != nil {
		gateway = querier
	}
	h := &character.Handlers{Store: store, IPs: newFakeIPs(), Voices: checker, Gateway: gateway}
	character.RegisterIPRoutes(r.Group("/ips/:id/characters"), h)
	character.RegisterRoutes(r.Group("/characters"), h)
	return r
}

// 绑定全绿路径:台账行存在且 status=OK、网关 query 应答 OK → 200 且
// voice_id 落列;校准确经台账行的钉定渠道与上游句柄,审计标记归因到
// 角色绑定。
func TestHandlerVoiceBindingCalibratesOK(t *testing.T) {
	store := newFakeStore()
	c := store.seed(1, "小星")
	checker := &fakeVoiceChecker{rows: map[int64]voice.Voice{
		3: {ID: 3, VoiceID: "vd-ok", ChannelID: 4, Name: "少年音", Status: voice.StatusOK},
	}}
	querier := &fakeVoiceQuerier{status: voice.StatusOK}
	r := newVoiceRouter(store, checker, querier)

	w := do(r, "PATCH", "/characters/"+itoa(c.ID), `{"voice_id":3}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	var got struct {
		VoiceID int64 `json:"voice_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.VoiceID != 3 {
		t.Fatalf("voice_id = %d, want 3", got.VoiceID)
	}
	if len(querier.calls) != 1 || querier.calls[0] != "4:vd-ok:canvas=characters" {
		t.Fatalf("query calls = %v, want one calibrated call on the pinned channel", querier.calls)
	}
}

// 上游已翻 UNDEPLOYED(台账还停在 OK 的漂移形态)→ 409 voice_undeployed,
// 绑定不落列。
func TestHandlerVoiceBindingRejectsUndeployed(t *testing.T) {
	store := newFakeStore()
	c := store.seed(1, "小星")
	checker := &fakeVoiceChecker{rows: map[int64]voice.Voice{
		3: {ID: 3, VoiceID: "vd-bad", ChannelID: 4, Name: "少年音", Status: voice.StatusOK},
	}}
	querier := &fakeVoiceQuerier{status: voice.StatusUndeployed}
	r := newVoiceRouter(store, checker, querier)

	w := do(r, "PATCH", "/characters/"+itoa(c.ID), `{"voice_id":3}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody.Error.Code != "voice_undeployed" {
		t.Fatalf("code = %q, want voice_undeployed", errBody.Error.Code)
	}
	persisted, err := store.GetCharacter(context.Background(), c.ID)
	if err != nil || persisted.VoiceID != 0 {
		t.Fatalf("character voice_id = %d, %v; want 0 (拒绝即不落列)", persisted.VoiceID, err)
	}
}

// 网关侧音色已不存在(404)→ 404 语义错误;其余网关 4xx 原样透传状态与
// 摘要(upstream_error)。
func TestHandlerVoiceBindingPassesGatewayRejectionsThrough(t *testing.T) {
	for _, tc := range []struct {
		name       string
		upstream   *voice.UpstreamError
		wantStatus int
		wantCode   string
	}{
		{"upstream gone", &voice.UpstreamError{Status: http.StatusNotFound, Message: "voice not found"}, http.StatusNotFound, "not_found"},
		{"circuit open", &voice.UpstreamError{Status: http.StatusTooManyRequests, Message: "渠道熔断中"}, http.StatusTooManyRequests, "upstream_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			c := store.seed(1, "小星")
			checker := &fakeVoiceChecker{rows: map[int64]voice.Voice{
				3: {ID: 3, VoiceID: "vd-gone", ChannelID: 4, Name: "少年音", Status: voice.StatusOK},
			}}
			querier := &fakeVoiceQuerier{err: tc.upstream}
			r := newVoiceRouter(store, checker, querier)

			w := do(r, "PATCH", "/characters/"+itoa(c.ID), `{"voice_id":3}`)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d body %s, want %d", w.Code, w.Body.String(), tc.wantStatus)
			}
			var errBody struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if errBody.Error.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", errBody.Error.Code, tc.wantCode)
			}
			if tc.upstream.Status == http.StatusNotFound && errBody.Error.Message != "音色「少年音」在上游已不存在(可能已被删除),不能绑定" {
				t.Fatalf("404 message = %q", errBody.Error.Message)
			}
			if tc.upstream.Status == http.StatusTooManyRequests && errBody.Error.Message != "渠道熔断中" {
				t.Fatalf("passthrough message = %q, want the gateway summary intact", errBody.Error.Message)
			}
		})
	}
}

// 解除绑定(voice_id = 0)不校准:网关一次都不打;网关未装配时解除与
// 角色其余字段照常可改(只有绑定动作依赖网关)。
func TestHandlerVoiceUnbindSkipsCalibration(t *testing.T) {
	store := newFakeStore()
	c := store.seed(1, "小星")
	checker := &fakeVoiceChecker{rows: map[int64]voice.Voice{
		3: {ID: 3, VoiceID: "vd-ok", ChannelID: 4, Name: "少年音", Status: voice.StatusOK},
	}}
	querier := &fakeVoiceQuerier{status: voice.StatusOK}
	r := newVoiceRouter(store, checker, querier)

	// 先绑上(fake store 直写,聚焦解除语义)。
	bound := int64(3)
	if _, err := store.UpdateCharacter(context.Background(), c.ID, &character.CharacterUpdate{VoiceID: &bound}); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	w := do(r, "PATCH", "/characters/"+itoa(c.ID), `{"voice_id":0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("unbind status = %d body %s", w.Code, w.Body.String())
	}
	if len(querier.calls) != 0 {
		t.Fatalf("query calls = %v, want none on unbind", querier.calls)
	}
	persisted, err := store.GetCharacter(context.Background(), c.ID)
	if err != nil || persisted.VoiceID != 0 {
		t.Fatalf("voice_id = %d, %v; want 0", persisted.VoiceID, err)
	}

	// 网关未装配:解除照常 200,绑定以 gateway_unconfigured 拒绝。
	rNoGateway := newVoiceRouter(store, checker, nil)
	if w := do(rNoGateway, "PATCH", "/characters/"+itoa(c.ID), `{"voice_id":0}`); w.Code != http.StatusOK {
		t.Fatalf("unbind without gateway status = %d body %s", w.Code, w.Body.String())
	}
	if w := do(rNoGateway, "PATCH", "/characters/"+itoa(c.ID), `{"voice_id":3}`); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("bind without gateway status = %d body %s", w.Code, w.Body.String())
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(do(rNoGateway, "PATCH", "/characters/"+itoa(c.ID), `{"voice_id":3}`).Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody.Error.Code != "gateway_unconfigured" {
		t.Fatalf("code = %q, want gateway_unconfigured", errBody.Error.Code)
	}
}

// 台账行缺失仍是 400(校准前先过本地存在性,网关不被无效 id 打扰)。
func TestHandlerVoiceBindingRejectsUnknownLedgerRow(t *testing.T) {
	store := newFakeStore()
	c := store.seed(1, "小星")
	querier := &fakeVoiceQuerier{status: voice.StatusOK}
	r := newVoiceRouter(store, &fakeVoiceChecker{rows: map[int64]voice.Voice{}}, querier)

	if w := do(r, "PATCH", "/characters/"+itoa(c.ID), `{"voice_id":99}`); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	if len(querier.calls) != 0 {
		t.Fatalf("query calls = %v, want none for unknown rows", querier.calls)
	}
}

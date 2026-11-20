package settings_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/settings"
)

// decodePetsDoc 取响应里的 doc 对象(94 号票多桌宠文档)。
func decodePetsDoc(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var parsed struct {
		Doc map[string]any `json:"doc"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return parsed.Doc
}

// NormalizePetsDoc 钉测(票面验证点):缺省构造合法、default 形象恒空规约、
// id 非空唯一、default 必在且恒居首位、active 失靶回落、上限 21、逐字段
// 校验沿用 PetConfig 规则。
func TestNormalizePetsDoc(t *testing.T) {
	// 缺省构造:绿毛球单实体,active 指向它,合法。
	def := settings.DefaultPetsDocument()
	if err := settings.NormalizePetsDoc(&def); err != nil {
		t.Fatalf("default doc should be valid: %v", err)
	}
	if def.ActiveID != settings.DefaultPetID || len(def.Pets) != 1 || def.Pets[0].ID != settings.DefaultPetID {
		t.Fatalf("default doc shape drifted: %+v", def)
	}

	// default 形象键恒规约为空(锁定 CSS 形象的 enforcement,不报错)。
	withImage := settings.PetsDocument{
		ActiveID: settings.DefaultPetID,
		Pets: []settings.PetEntry{
			{ID: settings.DefaultPetID, PetConfig: settings.PetConfig{IdleImage: "uploads/20261120/a.gif", InteractiveImage: "uploads/20261120/b.gif"}},
		},
	}
	if err := settings.NormalizePetsDoc(&withImage); err != nil {
		t.Fatalf("default with image keys should normalize silently: %v", err)
	}
	if withImage.Pets[0].IdleImage != "" || withImage.Pets[0].InteractiveImage != "" {
		t.Fatalf("default image keys must be forced empty: %+v", withImage.Pets[0])
	}

	// id 非空 + 唯一。
	dup := settings.PetsDocument{ActiveID: settings.DefaultPetID, Pets: []settings.PetEntry{
		{ID: settings.DefaultPetID},
		{ID: "x", PetConfig: settings.PetConfig{Name: "甲"}},
		{ID: " x ", PetConfig: settings.PetConfig{Name: "乙"}},
	}}
	if err := settings.NormalizePetsDoc(&dup); err == nil {
		t.Fatalf("duplicate ids (post-trim) should be rejected")
	}
	emptyID := settings.PetsDocument{ActiveID: settings.DefaultPetID, Pets: []settings.PetEntry{
		{ID: settings.DefaultPetID},
		{ID: "  ", PetConfig: settings.PetConfig{Name: "无名"}},
	}}
	if err := settings.NormalizePetsDoc(&emptyID); err == nil {
		t.Fatalf("empty id should be rejected")
	}

	// default 必在数组。
	noDefault := settings.PetsDocument{ActiveID: "x", Pets: []settings.PetEntry{
		{ID: "x", PetConfig: settings.PetConfig{Name: "无缺省"}},
	}}
	if err := settings.NormalizePetsDoc(&noDefault); err == nil {
		t.Fatalf("doc without default entry should be rejected")
	}

	// active 失靶规约回落 default;default 恒居首位。
	doc := settings.PetsDocument{ActiveID: "ghost", Pets: []settings.PetEntry{
		{ID: "first", PetConfig: settings.PetConfig{Name: "先来的"}},
		{ID: settings.DefaultPetID, PetConfig: settings.PetConfig{Name: "绿毛球"}},
	}}
	if err := settings.NormalizePetsDoc(&doc); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if doc.ActiveID != settings.DefaultPetID {
		t.Fatalf("dangling active should fall back to default, got %q", doc.ActiveID)
	}
	if doc.Pets[0].ID != settings.DefaultPetID {
		t.Fatalf("default must stay first, got order %v", []string{doc.Pets[0].ID, doc.Pets[1].ID})
	}

	// 上限:总数 21(default + 20 自定义)合法,22 拒。
	full := settings.PetsDocument{ActiveID: settings.DefaultPetID}
	full.Pets = append(full.Pets, settings.PetEntry{ID: settings.DefaultPetID})
	for i := 0; i < settings.MaxCustomPets; i++ {
		full.Pets = append(full.Pets, settings.PetEntry{ID: "custom-" + string(rune('a'+i))})
	}
	if err := settings.NormalizePetsDoc(&full); err != nil {
		t.Fatalf("21 pets should be valid: %v", err)
	}
	over := full
	over.Pets = append(over.Pets, settings.PetEntry{ID: "custom-x"})
	if err := settings.NormalizePetsDoc(&over); err == nil {
		t.Fatalf("22 pets should be rejected")
	}

	// 逐字段校验沿用 PetConfig 规则(自定义项形象键非法 → 拒;chips 收敛照旧)。
	bad := settings.PetsDocument{ActiveID: settings.DefaultPetID, Pets: []settings.PetEntry{
		{ID: settings.DefaultPetID},
		{ID: "c", PetConfig: settings.PetConfig{IdleImage: "canvases/1/2/x.png"}},
	}}
	if err := settings.NormalizePetsDoc(&bad); err == nil {
		t.Fatalf("custom pet with non-uploads key should be rejected")
	}
	chips := settings.PetsDocument{ActiveID: settings.DefaultPetID, Pets: []settings.PetEntry{
		{ID: settings.DefaultPetID, PetConfig: settings.PetConfig{Name: " 绿毛球 ", Chips: []string{"  a  ", ""}}},
	}}
	if err := settings.NormalizePetsDoc(&chips); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if chips.Pets[0].Name != "绿毛球" || len(chips.Pets[0].Chips) != 1 || chips.Pets[0].Chips[0] != "a" {
		t.Fatalf("per-entry normalization drifted: %+v", chips.Pets[0])
	}
}

// 旧 `pet` 行一次性惰性迁移(票面验证点):有形象分支。
func TestPetsReaderMigratesLegacyPetWithImages(t *testing.T) {
	store := newFakeStore()
	legacy := map[string]any{
		"name":              "老球",
		"greeting":          "老问候",
		"chips":             []string{"老指令"},
		"model":             "chat-old",
		"idle_image":        "uploads/20261119/old.gif",
		"interactive_image": "uploads/20261119/old-hit.gif",
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	if _, err := store.Put(t.Context(), settings.NamePet, raw); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	reader := settings.NewPetsDocReader(store)
	doc, err := reader(t.Context())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// 转成一只自定义桌宠(文案照搬)并设为 active;default 在首位。
	if doc.ActiveID == settings.DefaultPetID {
		t.Fatalf("legacy with images should become the active custom pet, active=%q", doc.ActiveID)
	}
	if len(doc.Pets) != 2 || doc.Pets[0].ID != settings.DefaultPetID {
		t.Fatalf("doc = %+v, want default first + one custom", doc)
	}
	custom := doc.Pets[1]
	if custom.ID != doc.ActiveID || custom.Name != "老球" || custom.Model != "chat-old" ||
		custom.IdleImage != "uploads/20261119/old.gif" || custom.InteractiveImage != "uploads/20261119/old-hit.gif" {
		t.Fatalf("custom entry diverged from legacy config: %+v", custom)
	}

	// 迁移写库:pets 行已落,旧行保留不删。
	if _, err := store.Get(t.Context(), settings.NamePets); err != nil {
		t.Fatalf("migrated pets row missing: %v", err)
	}
	if _, err := store.Get(t.Context(), settings.NamePet); err != nil {
		t.Fatalf("legacy pet row must be kept: %v", err)
	}

	// 幂等:二次读不再迁移(pets 行为准;改掉 pets 行内容即见新值)。
	doc.Pets[1].Name = "改名后"
	mutated, _ := json.Marshal(doc)
	if _, err := store.Put(t.Context(), settings.NamePets, mutated); err != nil {
		t.Fatalf("mutate pets row: %v", err)
	}
	again, err := reader(t.Context())
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if len(again.Pets) != 2 || again.Pets[1].Name != "改名后" {
		t.Fatalf("second read re-migrated from legacy row: %+v", again)
	}
}

// 旧行纯文案无形象:文案并入绿毛球,active 保持 default。
func TestPetsReaderMigratesLegacyTextIntoDefault(t *testing.T) {
	store := newFakeStore()
	legacy := map[string]any{
		"name": "纯文案球",
		"chips": []string{"指令"},
		"model": "chat-old",
	}
	raw, _ := json.Marshal(legacy)
	if _, err := store.Put(t.Context(), settings.NamePet, raw); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	doc, err := settings.NewPetsDocReader(store)(t.Context())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if doc.ActiveID != settings.DefaultPetID || len(doc.Pets) != 1 {
		t.Fatalf("text-only legacy should merge into default, got %+v", doc)
	}
	def := doc.Pets[0]
	if def.Name != "纯文案球" || def.Model != "chat-old" || len(def.Chips) != 1 {
		t.Fatalf("default entry should carry legacy text: %+v", def)
	}
	if def.IdleImage != "" || def.InteractiveImage != "" {
		t.Fatalf("default entry must stay image-free: %+v", def)
	}
}

// 零值/坏 JSON 容错落缺省文档;坏 pets 行同款宽容。
func TestPetsReaderToleratesBadRows(t *testing.T) {
	// 旧行坏 JSON → 缺省文档(仍写 pets 行,迁移完成)。
	store := newFakeStore()
	if _, err := store.Put(t.Context(), settings.NamePet, []byte("{not json")); err != nil {
		t.Fatalf("seed corrupt legacy row: %v", err)
	}
	doc, err := settings.NewPetsDocReader(store)(t.Context())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if doc.ActiveID != settings.DefaultPetID || len(doc.Pets) != 1 || doc.Pets[0].Name != "" {
		t.Fatalf("corrupt legacy row should fall back to default doc: %+v", doc)
	}
	if _, err := store.Get(t.Context(), settings.NamePets); err != nil {
		t.Fatalf("default doc should still be persisted: %v", err)
	}

	// pets 行坏 JSON → 宽容回缺省(照 NewPetReader 先例)。
	store2 := newFakeStore()
	if _, err := store2.Put(t.Context(), settings.NamePets, []byte("]broken")); err != nil {
		t.Fatalf("seed corrupt pets row: %v", err)
	}
	doc2, err := settings.NewPetsDocReader(store2)(t.Context())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(doc2.Pets) != 1 || doc2.Pets[0].ID != settings.DefaultPetID {
		t.Fatalf("corrupt pets row should degrade to default doc: %+v", doc2)
	}
}

// 全新库(两行皆缺):读即落缺省文档,后续读走 pets 行。
func TestPetsReaderFreshStoreAnswersDefaultAndPersists(t *testing.T) {
	store := newFakeStore()
	doc, err := settings.NewPetsDocReader(store)(t.Context())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if doc.ActiveID != settings.DefaultPetID || len(doc.Pets) != 1 {
		t.Fatalf("fresh store should answer default doc: %+v", doc)
	}
	if _, err := store.Get(t.Context(), settings.NamePets); err != nil {
		t.Fatalf("default doc should be persisted on first read: %v", err)
	}
}

// 兜底确认:MaxCustomPets=20(文档总数 ≤21)。
func TestPetsLimitsConstants(t *testing.T) {
	if settings.MaxCustomPets != 20 || settings.MaxPetsTotal != 21 {
		t.Fatalf("limits drifted: custom=%d total=%d", settings.MaxCustomPets, settings.MaxPetsTotal)
	}
	if !strings.HasPrefix(settings.NamePets, "pets") {
		t.Fatalf("row name drifted: %q", settings.NamePets)
	}
}

// C1/S2 评审修复:LoadPetsDocForWrite 是管理写路径的合并基座 —— 库存行
// decode/规约失败回 ErrPetsDocCorrupt(绝不回落缺省文档,防静默覆盖库存);
// LoadPetsDocStamped 是管理 GET 的宽容读,一次 Get 同取文档与行时戳(坏行
// 回落缺省文档但绝不写库,时戳照带)。缺行时 strict 同走惰性迁移 —— 迁移
// 产物是真基座,不是覆盖。
func TestLoadPetsDocForWriteRejectsCorruptRow(t *testing.T) {
	store := newFakeStore()
	over := settings.DefaultPetsDocument()
	for i := 0; i < settings.MaxCustomPets+1; i++ {
		over.Pets = append(over.Pets, settings.PetEntry{ID: "c" + string(rune('a'+i))})
	}
	raw, err := json.Marshal(over)
	if err != nil {
		t.Fatalf("marshal corrupt doc: %v", err)
	}
	if _, err := store.Put(t.Context(), settings.NamePets, raw); err != nil {
		t.Fatalf("seed corrupt row: %v", err)
	}

	// strict(写基座):规约失败即拒。
	if _, _, err := settings.LoadPetsDocForWrite(t.Context(), store); !errors.Is(err, settings.ErrPetsDocCorrupt) {
		t.Fatalf("strict load on 22-pet row: err = %v, want ErrPetsDocCorrupt", err)
	}

	// tolerant(管理 GET):缺省回落 + 行时戳照带,不报错。
	doc, stamp, err := settings.LoadPetsDocStamped(t.Context(), store)
	if err != nil {
		t.Fatalf("tolerant load: %v", err)
	}
	if doc.ActiveID != settings.DefaultPetID || len(doc.Pets) != 1 || doc.Pets[0].ID != settings.DefaultPetID {
		t.Fatalf("tolerant load should answer default doc: %+v", doc)
	}
	if stamp == "" {
		t.Fatalf("tolerant load should carry the row stamp even on fallback")
	}

	// 坏 JSON 行:strict 同款拒绝(同一静默覆盖隐患)。
	if _, err := store.Put(t.Context(), settings.NamePets, []byte("]broken")); err != nil {
		t.Fatalf("seed bad json row: %v", err)
	}
	if _, _, err := settings.LoadPetsDocForWrite(t.Context(), store); !errors.Is(err, settings.ErrPetsDocCorrupt) {
		t.Fatalf("strict load on bad json: err = %v, want ErrPetsDocCorrupt", err)
	}

	// 缺行:strict 走惰性迁移并带回迁移时戳。
	fresh := newFakeStore()
	legacy := map[string]any{"name": "老球", "idle_image": "uploads/20261119/old.gif"}
	lraw, _ := json.Marshal(legacy)
	if _, err := fresh.Put(t.Context(), settings.NamePet, lraw); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	doc, stamp, err = settings.LoadPetsDocForWrite(t.Context(), fresh)
	if err != nil {
		t.Fatalf("strict load on missing pets row: %v", err)
	}
	if len(doc.Pets) != 2 || doc.Pets[0].ID != settings.DefaultPetID || doc.Pets[1].Name != "老球" {
		t.Fatalf("strict load should migrate legacy row: %+v", doc)
	}
	if stamp == "" {
		t.Fatalf("migration persist should stamp the row")
	}
}

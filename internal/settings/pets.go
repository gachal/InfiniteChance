// 多桌宠文档(94 号票):settings KV 新 key `pets`,单 JSON 文档
// `{active_id, pets: [...]}` —— 照画布图「一列文档」哲学,零 schema 变更
// (KV 行不是 DDL)。绿毛球是内置实体固定 id `default` 完整存数组首位
// (形象锁定为前端 CSS 缺省形象,服务端把它的形象键恒规约为空);自定义
// 桌宠上限 20 只;active 是文档级指针,切桌宠 = 换皮 + 换人设。
//
// 旧 `pet` 行(92 号票单桌宠)一次性惰性迁移:首次读 `pets` 缺行时按旧
// 配置形态转换(有形象 → 自定义桌宠设 active;纯文案 → 并入绿毛球),
// 迁移写库后旧行保留不删,Reader 不做双形状兼容。
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
)

// NamePets is the settings row carrying the multi-pet document(94 号票)。
const NamePets = "pets"

// DefaultPetID 是绿毛球的固定 id:内置、不可删除、列表恒居首位。
const DefaultPetID = "default"

// 自定义桌宠上限与文档总数上限(含 default)。
const (
	MaxCustomPets = 20
	MaxPetsTotal  = MaxCustomPets + 1
)

// PetEntry is one pet in the document:条目形状沿用 92 号票 PetConfig 字段
// (嵌入即得 name/greeting/chips/model/idle_image/interactive_image 线形)。
// default 条目的形象键恒为空(锁定 CSS 形象)。
type PetEntry struct {
	ID string `json:"id"`
	PetConfig
}

// PetsDocument is the decoded `pets` row。绿毛球完整实体存 pets 数组首位,
// ActiveID 指向当前桌宠(缺省 default),三处(列表/表单/指针)统一口径。
type PetsDocument struct {
	ActiveID string     `json:"active_id"`
	Pets     []PetEntry `json:"pets"`
}

// DefaultPetsDocument answers the zero-config shape:绿毛球单实体、文案全空
// (画布面走前端内置兜底文案与 CSS 缺省形象,与桌面版零配置同一语义)。
func DefaultPetsDocument() PetsDocument {
	return PetsDocument{
		ActiveID: DefaultPetID,
		Pets:     []PetEntry{{ID: DefaultPetID}},
	}
}

// ActiveEntry answers the currently active pet;失靶(库被手改)回落 default
// 条目,再兜底首条 —— NormalizePetsDoc 已保证 default 在册,这里只是防御。
func (d PetsDocument) ActiveEntry() PetEntry {
	for _, e := range d.Pets {
		if e.ID == d.ActiveID {
			return e
		}
	}
	for _, e := range d.Pets {
		if e.ID == DefaultPetID {
			return e
		}
	}
	if len(d.Pets) > 0 {
		return d.Pets[0]
	}
	return PetEntry{ID: DefaultPetID}
}

// NormalizePetsDoc trims and validates the whole document a fresh write must
// satisfy:逐只套用 PetConfig 规则(NormalizePetConfig),文档级新规 ——
// pets 数 1..MaxPetsTotal、id 非空且唯一(trim 后)、default 必在数组且恒居
// 首位、default 的形象键恒规约为空(锁定形象的 enforcement,不报错)、
// active_id 失靶规约回落 default。
func NormalizePetsDoc(doc *PetsDocument) error {
	if doc == nil {
		return fmt.Errorf("桌宠文档为空")
	}
	if len(doc.Pets) < 1 || len(doc.Pets) > MaxPetsTotal {
		return fmt.Errorf("桌宠数量必须在 1..%d 之间(绿毛球 + 最多 %d 只自定义)", MaxPetsTotal, MaxCustomPets)
	}

	seen := make(map[string]bool, len(doc.Pets))
	defIdx := -1
	for i := range doc.Pets {
		e := &doc.Pets[i]
		e.ID = strings.TrimSpace(e.ID)
		if e.ID == "" {
			return fmt.Errorf("桌宠 id 不能为空")
		}
		if seen[e.ID] {
			return fmt.Errorf("桌宠 id 重复:%s", e.ID)
		}
		seen[e.ID] = true
		if e.ID == DefaultPetID {
			defIdx = i
		}
		if err := NormalizePetConfig(&e.PetConfig); err != nil {
			return err
		}
	}
	if defIdx < 0 {
		return fmt.Errorf("默认桌宠 %q 必须存在(不可删除)", DefaultPetID)
	}

	// default 形象锁定:恒规约为空(93 号票 CSS 缺省形象,无图槽)。
	doc.Pets[defIdx].IdleImage = ""
	doc.Pets[defIdx].InteractiveImage = ""

	// default 恒居首位(stable move,其余相对次序不变)。
	if defIdx != 0 {
		def := doc.Pets[defIdx]
		copy(doc.Pets[1:defIdx+1], doc.Pets[:defIdx])
		doc.Pets[0] = def
	}

	// active 失靶规约回落 default(不报错)。
	doc.ActiveID = strings.TrimSpace(doc.ActiveID)
	if !seen[doc.ActiveID] {
		doc.ActiveID = DefaultPetID
	}
	return nil
}

// PetsDocReader answers the whole pets document per request(热生效,调用量
// 小不加缓存,storage/prompt_templates 先例)。
type PetsDocReader func(ctx context.Context) (PetsDocument, error)

// NewPetsDocReader reads the `pets` row per call,首次缺行时惰性迁移旧
// `pet` 行。坏 JSON 行宽容回缺省文档,不让画布面瘫痪(NewPetReader 先例)。
func NewPetsDocReader(s Store) PetsDocReader {
	return func(ctx context.Context) (PetsDocument, error) {
		return loadPetsDoc(ctx, s)
	}
}

// loadPetsDoc reads the pets row with the legacy lazy migration;迁移写库
// 失败时回内存文档(画布面不因写失败瘫痪,照防御性写裁量)。
func loadPetsDoc(ctx context.Context, s Store) (PetsDocument, error) {
	row, err := s.Get(ctx, NamePets)
	if err == nil {
		var doc PetsDocument
		if jerr := json.Unmarshal(row.Value, &doc); jerr != nil {
			log.Printf("settings: pets row undecodable, falling back to default doc: %v", jerr)
			return DefaultPetsDocument(), nil
		}
		if nerr := NormalizePetsDoc(&doc); nerr != nil {
			log.Printf("settings: pets row failed normalization, falling back to default doc: %v", nerr)
			return DefaultPetsDocument(), nil
		}
		return doc, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return PetsDocument{}, err
	}

	// 缺行 → 一次性惰性迁移(迁移即落库,后续读走 pets 行;幂等由此保证)。
	doc := migrateLegacyPet(ctx, s)
	if value, merr := json.Marshal(doc); merr == nil {
		if _, perr := s.Put(ctx, NamePets, value); perr != nil {
			log.Printf("settings: persist migrated pets row: %v (answering in-memory doc)", perr)
		}
	} else {
		log.Printf("settings: marshal migrated pets doc: %v", merr)
	}
	return doc, nil
}

// migrateLegacyPet converts the legacy single-pet row (92 号票) into the
// multi-pet document。有形象(idle_image 非空)→ 转自定义桌宠设 active;
// 纯文案 → 并入绿毛球(名称非空也并入),active 保持 default。零值/坏
// JSON/非法形状一律落缺省文档不报错(防御性迁移);旧行保留不删。
func migrateLegacyPet(ctx context.Context, s Store) PetsDocument {
	doc := DefaultPetsDocument()

	row, err := s.Get(ctx, NamePet)
	if errors.Is(err, ErrNotFound) {
		return doc // 全新库:无旧行,直接缺省文档
	}
	if err != nil {
		log.Printf("settings: read legacy pet row for migration: %v", err)
		return doc
	}
	var legacy PetConfig
	if uerr := json.Unmarshal(row.Value, &legacy); uerr != nil {
		log.Printf("settings: legacy pet row undecodable, migrating to default doc: %v", uerr)
		return doc
	}
	if nerr := NormalizePetConfig(&legacy); nerr != nil {
		log.Printf("settings: legacy pet row failed normalization, migrating to default doc: %v", nerr)
		return doc
	}

	if legacy.IdleImage != "" {
		// 有形象:整份配置照搬成一只自定义桌宠并设为 active。
		entry := PetEntry{ID: uuid.NewString(), PetConfig: legacy}
		doc.Pets = append(doc.Pets, entry)
		doc.ActiveID = entry.ID
	} else {
		// 纯文案:并入绿毛球(default 形象锁定,遗留的纯 interactive 键
		// 不带入 —— 对象成孤儿可接受,92 号票同款裁量)。
		def := &doc.Pets[0]
		def.Name = legacy.Name
		def.Greeting = legacy.Greeting
		def.Chips = legacy.Chips
		def.Model = legacy.Model
		doc.ActiveID = DefaultPetID
	}

	if nerr := NormalizePetsDoc(&doc); nerr != nil {
		log.Printf("settings: migrated doc failed normalization, falling back to default doc: %v", nerr)
		return DefaultPetsDocument()
	}
	return doc
}

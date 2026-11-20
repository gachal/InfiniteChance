// 多桌宠文档的 admin REST 五端点(94 号票):GET / POST / PUT :id /
// DELETE :id / PUT active。CRUD 落 pet 包而非 settings 包,是 import 方向
// 使然 —— DELETE 要物理回收对象键(objectstore),而 settings 不可反向
// 依赖 objectstore(objectstore.Dynamic 按 settings 存储驱动路由)。语义
// 照 92 号票先例:PUT 指针字段合并(未携带沿用已存,显式空串/空数组清空),
// 无密钥字段全部原值回显;桌宠配置无 assets 行,「先删行后删对象」的行侧
// 即 KV 行,对象回收按文档内引用计数去重(兄弟条目仍登记的键不删)。
//
// 读写分工(94 号票评审修复轮 C1):GET /admin/pets 与画布面读同口径容错
// —— 库存行损坏回落缺省文档应答、绝不写库;四个写端点的 load 走
// loadDocForWrite(settings.LoadPetsDocForWrite)—— 库存行 decode/规约
// 失败即 500 pets_doc_corrupt 拒写,绝不拿缺省文档继续 mutate→put(否则
// 一次普通管理写就把损坏库存静默覆盖成缺省文档)。
package pet

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/settings"
)

// petEntryJSON is the wire form of one pet entry。
type petEntryJSON struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Greeting         string   `json:"greeting"`
	Chips            []string `json:"chips"`
	Model            string   `json:"model"`
	IdleImage        string   `json:"idle_image"`
	InteractiveImage string   `json:"interactive_image"`
}

// petsDocJSON is the wire form of the whole document;updated_at 为空表示
// 文档从未写过(迁移/缺省构造不算写,GET 首开即渲染表单)。
type petsDocJSON struct {
	ActiveID  string         `json:"active_id"`
	Pets      []petEntryJSON `json:"pets"`
	UpdatedAt string         `json:"updated_at"`
}

// petEntryInput is the POST/PUT body。指针字段表达「未携带 = 沿用已存」
// (照 92 号票 petInputJSON 的合并哲学):显式空串/空数组才是清空。
type petEntryInput struct {
	Name             *string   `json:"name"`
	Greeting         *string   `json:"greeting"`
	Chips            *[]string `json:"chips"`
	Model            *string   `json:"model"`
	IdleImage        *string   `json:"idle_image"`
	InteractiveImage *string   `json:"interactive_image"`
}

// petActiveInput is the PUT /pets/active body。
type petActiveInput struct {
	ID string `json:"id"`
}

func toEntryJSON(e settings.PetEntry) petEntryJSON {
	chips := e.Chips
	if chips == nil {
		chips = []string{}
	}
	return petEntryJSON{
		ID:               e.ID,
		Name:             e.Name,
		Greeting:         e.Greeting,
		Chips:            chips,
		Model:            e.Model,
		IdleImage:        e.IdleImage,
		InteractiveImage: e.InteractiveImage,
	}
}

func toDocJSON(doc settings.PetsDocument, updatedAt string) petsDocJSON {
	out := petsDocJSON{ActiveID: doc.ActiveID, UpdatedAt: updatedAt}
	out.Pets = make([]petEntryJSON, 0, len(doc.Pets))
	for _, e := range doc.Pets {
		out.Pets = append(out.Pets, toEntryJSON(e))
	}
	return out
}

// docWithStamp reads the document plus the row's updated_at in one Get
// (S2 评审修复:旧实现 loadDoc 与取时戳各 Get 一次,双读且时戳读错被
// 静默吞掉 —— 现在两者同源,Get 失败随错误传播)。容错口径与画布面读
// 一致:坏行回落缺省文档应答、绝不写库;只服务 GET /admin/pets,写路径
// 走 loadDocForWrite。空时戳 = 行从未落库(迁移写失败的空窗,可接受 ——
// 管理页只拿它显示「上次保存时间」)。
func (h *Handlers) docWithStamp(ctx context.Context) (settings.PetsDocument, string, error) {
	if h.Store == nil {
		return settings.PetsDocument{}, "", errStoreUnconfigured
	}
	return settings.LoadPetsDocStamped(ctx, h.Store)
}

// loadDocForWrite reads the merge base for the four admin write endpoints
// (C1 评审修复):库存行 decode/规约失败回 settings.ErrPetsDocCorrupt,
// 由 fail 映射成 500 pets_doc_corrupt —— 绝不拿缺省文档继续 mutate→put。
// 同一次 Get 连行时戳一起取(S2)。
func (h *Handlers) loadDocForWrite(ctx context.Context) (settings.PetsDocument, string, error) {
	if h.Store == nil {
		return settings.PetsDocument{}, "", errStoreUnconfigured
	}
	return settings.LoadPetsDocForWrite(ctx, h.Store)
}

// findEntry answers the index of id in the doc(-1 = 无此条目);Update /
// Delete / SetActive 三处按 id 定位共用(S1 评审修复)。
func findEntry(doc settings.PetsDocument, id string) int {
	for i := range doc.Pets {
		if doc.Pets[i].ID == id {
			return i
		}
	}
	return -1
}

// putDoc normalizes the document in place, then persists it, answering the
// row's stamp。调用方随后的 doc 即规约后形状(回显用)。
func (h *Handlers) putDoc(ctx context.Context, doc *settings.PetsDocument) (string, error) {
	if err := settings.NormalizePetsDoc(doc); err != nil {
		return "", errInvalidInput{err.Error()}
	}
	value, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	row, err := h.Store.Put(ctx, settings.NamePets, value)
	if err != nil {
		return "", err
	}
	return row.UpdatedAt.Format(time.RFC3339Nano), nil
}

// errInvalidInput carries a 400-able message from normalization。
type errInvalidInput struct{ msg string }

func (e errInvalidInput) Error() string { return e.msg }

// ListPets answers the whole document(缺行回答缺省文档而非 404:管理页
// 第一次打开就要能渲染列表)。
func (h *Handlers) ListPets(c *gin.Context) {
	doc, stamp, err := h.docWithStamp(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"doc": toDocJSON(doc, stamp)})
}

// CreatePet adds a custom pet with a server-generated uuid id;绿毛球恒居
// 首位、active 不因新建偏移。
func (h *Handlers) CreatePet(c *gin.Context) {
	if err := h.requireStore(c); err != nil {
		return
	}
	var raw petEntryInput
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的桌宠配置 JSON")
		return
	}

	doc, stamp, err := h.loadDocForWrite(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	if len(doc.Pets) >= settings.MaxPetsTotal {
		apierr.InvalidRequest(c, "桌宠数量已达上限(绿毛球 + 最多 20 只自定义)")
		return
	}

	entry := settings.PetEntry{ID: uuid.NewString()}
	h.applyInput(&entry.PetConfig, &raw)
	doc.Pets = append(doc.Pets, entry)
	// 回显条目用写前捕获的切片元素指针:NormalizePetsDoc 只就地改写、不增删
	// 条目,规约后的值经同指针读出(S5 评审修复:删掉「落库后全表找回」的
	// 不可达兜底分支)。
	created := &doc.Pets[len(doc.Pets)-1]

	stamp, err = h.putDoc(c.Request.Context(), &doc)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"pet": toEntryJSON(*created), "doc": toDocJSON(doc, stamp)})
}

// UpdatePet replaces one entry's editable fields with 92 号票 merge semantics。
// default 条目只收文案字段(形象锁定,携带形象键即 400)。
func (h *Handlers) UpdatePet(c *gin.Context) {
	if err := h.requireStore(c); err != nil {
		return
	}
	id := c.Param("id")
	var raw petEntryInput
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的桌宠配置 JSON")
		return
	}
	if id == settings.DefaultPetID && (raw.IdleImage != nil || raw.InteractiveImage != nil) {
		apierr.InvalidRequest(c, "默认桌宠形象锁定为内置 CSS 形象,不接受形象键")
		return
	}

	doc, stamp, err := h.loadDocForWrite(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	idx := findEntry(doc, id)
	if idx < 0 {
		apierr.NotFound(c, "桌宠不存在")
		return
	}
	updated := &doc.Pets[idx]
	h.applyInput(&updated.PetConfig, &raw)

	stamp, err = h.putDoc(c.Request.Context(), &doc)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pet": toEntryJSON(*updated), "doc": toDocJSON(doc, stamp)})
}

// applyInput merges pointer fields onto a config(未携带沿用已存,显式
// 空串/空数组清空 —— 92 号票先例)。
func (h *Handlers) applyInput(cfg *settings.PetConfig, raw *petEntryInput) {
	apply := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	apply(&cfg.Name, raw.Name)
	apply(&cfg.Greeting, raw.Greeting)
	apply(&cfg.Model, raw.Model)
	apply(&cfg.IdleImage, raw.IdleImage)
	apply(&cfg.InteractiveImage, raw.InteractiveImage)
	if raw.Chips != nil {
		cfg.Chips = *raw.Chips
	}
}

// DeletePet removes a custom pet. id=default 拒绝(409);被删条目登记且
// 文档内再无引用的对象键物理回收(先删行后删对象;对象删除失败仅记日志,
// 键成孤儿可接受 —— 92/94 号票同款裁量);删 active 桌宠时 active 回落
// default。
func (h *Handlers) DeletePet(c *gin.Context) {
	if err := h.requireStore(c); err != nil {
		return
	}
	id := c.Param("id")
	if id == settings.DefaultPetID {
		apierr.Conflict(c, "default_pet_undeletable", "默认桌宠不可删除")
		return
	}

	doc, _, err := h.loadDocForWrite(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	idx := findEntry(doc, id)
	if idx < 0 {
		apierr.NotFound(c, "桌宠不存在")
		return
	}
	removed := doc.Pets[idx]
	doc.Pets = append(doc.Pets[:idx], doc.Pets[idx+1:]...)
	if doc.ActiveID == id {
		doc.ActiveID = settings.DefaultPetID
	}

	if _, err = h.putDoc(c.Request.Context(), &doc); err != nil {
		h.fail(c, err)
		return
	}

	// 行已落,回收失去文档内引用的对象键(先删行后删对象;无共享键才删)。
	if h.Storage != nil {
		for _, key := range []string{removed.IdleImage, removed.InteractiveImage} {
			if key == "" || docReferencesKey(doc, key) {
				continue
			}
			if err := h.Storage.Delete(c.Request.Context(), key); err != nil {
				log.Printf("pet: recycle object %q after delete: %v", key, err)
			}
		}
	}
	c.Status(http.StatusNoContent)
}

// SetActivePet switches the active pointer(body {id});失靶 404、空 400。
func (h *Handlers) SetActivePet(c *gin.Context) {
	if err := h.requireStore(c); err != nil {
		return
	}
	var raw petActiveInput
	if err := c.ShouldBindJSON(&raw); err != nil {
		apierr.InvalidRequest(c, "请求体不是合法的切换请求 JSON")
		return
	}
	id := raw.ID
	if id == "" {
		apierr.InvalidRequest(c, "缺少要切换到的桌宠 id")
		return
	}

	doc, stamp, err := h.loadDocForWrite(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	if findEntry(doc, id) < 0 {
		apierr.NotFound(c, "桌宠不存在")
		return
	}
	doc.ActiveID = id

	stamp, err = h.putDoc(c.Request.Context(), &doc)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"doc": toDocJSON(doc, stamp)})
}

// requireStore rejects admin writes when the settings store isn't wired。
func (h *Handlers) requireStore(c *gin.Context) error {
	if h.Store == nil {
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return errStoreUnconfigured
	}
	return nil
}

// fail maps handler errors onto the统一 error shape:规约消息 400,库存行
// 损坏 500 pets_doc_corrupt(写保护,修复库存前拒绝一切管理写),其余 500。
func (h *Handlers) fail(c *gin.Context, err error) {
	var inv errInvalidInput
	if errors.As(err, &inv) {
		apierr.InvalidRequest(c, inv.msg)
		return
	}
	if errors.Is(err, settings.ErrPetsDocCorrupt) {
		log.Printf("pet: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Write(c, http.StatusInternalServerError, "pets_doc_corrupt",
			"桌宠配置行损坏,已拒绝本次写入以保护现有配置;请先手工修复 settings 表 pets 行")
		return
	}
	if errors.Is(err, errStoreUnconfigured) {
		log.Printf("pet: settings store not wired")
	} else {
		log.Printf("pet: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	}
	apierr.Internal(c, "服务内部错误,请稍后再试")
}

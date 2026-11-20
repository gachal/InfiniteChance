// 多桌宠文档的 admin REST 五端点(94 号票):GET / POST / PUT :id /
// DELETE :id / PUT active。CRUD 落 pet 包而非 settings 包,是 import 方向
// 使然 —— DELETE 要物理回收对象键(objectstore),而 settings 不可反向
// 依赖 objectstore(objectstore.Dynamic 按 settings 存储驱动路由)。语义
// 照 92 号票先例:PUT 指针字段合并(未携带沿用已存,显式空串/空数组清空),
// 无密钥字段全部原值回显;桌宠配置无 assets 行,「先删行后删对象」的行侧
// 即 KV 行,对象回收按文档内引用计数去重(兄弟条目仍登记的键不删)。
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

// docWithStamp reads the document plus the row's updated_at(空串 = 行从未
// 写过;迁移/缺省构造的首次落库不算用户写入,但行会存在,读到的是迁移时戳
// —— 可接受,管理页只拿它显示「上次保存时间」)。
func (h *Handlers) docWithStamp(ctx context.Context) (settings.PetsDocument, string, error) {
	doc, err := h.loadDoc(ctx)
	if err != nil {
		return settings.PetsDocument{}, "", err
	}
	stamp := ""
	if h.Store != nil {
		if row, gerr := h.Store.Get(ctx, settings.NamePets); gerr == nil {
			stamp = row.UpdatedAt.Format(time.RFC3339Nano)
		}
	}
	return doc, stamp, nil
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

	doc, stamp, err := h.docWithStamp(c.Request.Context())
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

	stamp, err = h.putDoc(c.Request.Context(), &doc)
	if err != nil {
		h.fail(c, err)
		return
	}
	for _, e := range doc.Pets {
		if e.ID == entry.ID {
			c.JSON(http.StatusCreated, gin.H{"pet": toEntryJSON(e), "doc": toDocJSON(doc, stamp)})
			return
		}
	}
	// 理论不可达(条目刚插入且规约不删条目);防御性回答整份文档。
	c.JSON(http.StatusCreated, gin.H{"doc": toDocJSON(doc, stamp)})
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

	doc, stamp, err := h.docWithStamp(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	idx := -1
	for i := range doc.Pets {
		if doc.Pets[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		apierr.NotFound(c, "桌宠不存在")
		return
	}
	h.applyInput(&doc.Pets[idx].PetConfig, &raw)

	stamp, err = h.putDoc(c.Request.Context(), &doc)
	if err != nil {
		h.fail(c, err)
		return
	}
	for _, e := range doc.Pets {
		if e.ID == id {
			c.JSON(http.StatusOK, gin.H{"pet": toEntryJSON(e), "doc": toDocJSON(doc, stamp)})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"doc": toDocJSON(doc, stamp)})
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

	doc, _, err := h.docWithStamp(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	idx := -1
	for i := range doc.Pets {
		if doc.Pets[i].ID == id {
			idx = i
			break
		}
	}
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
			if key == "" || keyReferenced(doc, key) {
				continue
			}
			if err := h.Storage.Delete(c.Request.Context(), key); err != nil {
				log.Printf("pet: recycle object %q after delete: %v", key, err)
			}
		}
	}
	c.Status(http.StatusNoContent)
}

// keyReferenced answers whether any entry still registers the key(回收前
// 的引用计数,兄弟条目共享的键不删)。
func keyReferenced(doc settings.PetsDocument, key string) bool {
	for _, e := range doc.Pets {
		if e.IdleImage == key || e.InteractiveImage == key {
			return true
		}
	}
	return false
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

	doc, stamp, err := h.docWithStamp(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	found := false
	for _, e := range doc.Pets {
		if e.ID == id {
			found = true
			break
		}
	}
	if !found {
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

// fail maps handler errors onto the统一 error shape:规约消息 400,其余 500。
func (h *Handlers) fail(c *gin.Context, err error) {
	var inv errInvalidInput
	if errors.As(err, &inv) {
		apierr.InvalidRequest(c, inv.msg)
		return
	}
	if errors.Is(err, errStoreUnconfigured) {
		log.Printf("pet: settings store not wired")
	} else {
		log.Printf("pet: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	}
	apierr.Internal(c, "服务内部错误,请稍后再试")
}

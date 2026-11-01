package canvastask

// 无画布任务端点族(70 号票,[ADR 0007]):IP 库级生成路径的第一条 ——
// 角色设定图生成。任务行 canvas_id=0、node_id 空、ip_id 携归属;prompt 由
// 服务端按五类型模板拼装(类型模板 + 角色四件套 + 变体名/服装描述 + IP
// 风格段),落任务行可审计 —— 抽屉轻量生成对话框没有 prompt 编辑面
// (票面 B5),拼装是唯一口径。端点形状不排他:场景/道具将来复用另票。
//
// 网关未配置时提交同码 gateway_unconfigured 拒绝(画布任务同纪律);失败
// 退款/原地重试语义不在本面(无画布任务的进度看发起对话框,产物兜底在
// 素材域)。

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/pricing"
)

// 设定图五类型(70 号票,label 白名单六类去「其他」)。默认三视图。
const (
	SheetTypeTurnaround = "三视图"
	SheetTypeMultiView  = "多视图"
	SheetTypeExprSheet  = "表情集"
	SheetTypePalette    = "色板"
	SheetTypePose       = "姿态"
)

// SheetTypes 是合法类型的固定序(默认档排第一,前端下拉同序同默认)。
var SheetTypes = []string{SheetTypeTurnaround, SheetTypeMultiView, SheetTypeExprSheet, SheetTypePalette, SheetTypePose}

// sheetTypeTemplates 是五类型的 prompt 模板常量(照分析指令先例,不进
// 技能库):三视图按票面定案 = 全身站姿正/侧/背三视图对照白底设定图。
// 前端 canvas/web/src/sheet.ts 备同字面量一份(composer 入口预填拼装),
// 两侧各自用字面量测试钉住防漂移 —— 改文案必须两侧同改。
var sheetTypeTemplates = map[string]string{
	SheetTypeTurnaround: "角色设定图·三视图:同一角色的全身站姿正面、侧面、背面三视图对照,白底,全身完整无裁切,各视角比例一致,标准设定图画风",
	SheetTypeMultiView:  "角色设定图·多视图:同一角色的全身多角度视图对照(正面、四分之三、侧面、背面),白底,各视角比例一致,标准设定图画风",
	SheetTypeExprSheet:  "角色设定图·表情集:同一角色头部的多种表情对照(喜、怒、哀、惊等),白底,构图整齐,表情差异清晰,标准设定图画风",
	SheetTypePalette:    "角色设定图·色板:该角色的配色色板展示(发色、肤色、服装主色与辅色色块并列),白底,标注干净,标准设定图画风",
	SheetTypePose:       "角色设定图·姿态集:同一角色的多个典型动作与姿态对照,全身,白底,姿态自然清晰,标准设定图画风",
}

// MaxSheetPromptRunes 是拼装后的 prompt 截断上限(70 号票定案 ≤2000
// runes,与配乐/音效的 2000 同档)。四件套里的 detail 上限 20000,不截
// 会顶穿,所以截断是拼装的固定尾步。
const MaxSheetPromptRunes = 2000

// SheetFacts 是拼装的角色事实(name 必给,其余可空整行跳过)。
type SheetFacts struct {
	Name        string
	Alias       string
	Description string
	Detail      string
	VariantName string
	VariantNote string
}

// BuildSheetPrompt assembles the server-side sheet prompt:类型模板 +
// 角色四件套(name/alias/description/detail)+ 变体名与服装描述(note),
// IP 风格段(69 号票 StyleSegment,有则拼)放 prompt 前部单独成行,最后
// 按 rune 截断到 MaxSheetPromptRunes。未知类型跳过模板行(handler 在
// 落行前已拦,这里是防御面)。与 TS 侧 sheet.ts buildSheetPrompt 同形,
// 两侧测试各自钉住同一组字面量。
func BuildSheetPrompt(sheetType string, f SheetFacts, style string) string {
	lines := make([]string, 0, 7)
	if tpl, ok := sheetTypeTemplates[sheetType]; ok {
		lines = append(lines, tpl)
	}
	lines = append(lines, "角色:"+f.Name)
	if f.Alias != "" {
		lines = append(lines, "别名:"+f.Alias)
	}
	if f.Description != "" {
		lines = append(lines, "简介:"+f.Description)
	}
	if f.Detail != "" {
		lines = append(lines, "详细设定:"+f.Detail)
	}
	lines = append(lines, "造型:"+f.VariantName)
	if f.VariantNote != "" {
		lines = append(lines, "服装:"+f.VariantNote)
	}
	prompt := strings.Join(lines, "\n")
	if seg := canvas.StyleSegment(style); seg != "" {
		prompt = seg + "\n" + prompt
	}
	return firstRunes(prompt, MaxSheetPromptRunes)
}

// firstRunes keeps the first n runes of s(硬截断不加省略号 —— prompt
// 截断是预算约束,client.go 的 truncateRunes 是摘要语义,不同规不复用)。
func firstRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// CharacterGetter is the slice of the character store the IP task handler
// needs:角色存在性与归属裁决(IP 严收)与变体隶属校验都靠整棵树回答。
type CharacterGetter interface {
	GetCharacter(ctx context.Context, id int64) (character.Character, error)
}

// RegisterIPRoutes mounts (relative to the group, which wiring mounts at
// /ips/:id/tasks behind the JWT middleware, alongside the characters
// directory routes):
//
//	POST /           — submit one sheet task {character_id, variant_id, sheet_type, model, …}
//	GET  /           — the IP's canvas-free tasks (兜底枚举)
//	GET  /:tid       — one task (对话框轮询)
func RegisterIPRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("", h.IPCreate)
	group.GET("", h.IPList)
	group.GET("/:tid", h.IPGet)
}

// ipTaskInput is the wire form of one sheet-task submit。prompt 不收 ——
// 由服务端拼装(70 号票定案,拼装落任务行可审计)。
type ipTaskInput struct {
	CharacterID int64    `json:"character_id"`
	VariantID   int64    `json:"variant_id"`
	SheetType   string   `json:"sheet_type"`
	Model       string   `json:"model"`
	Size        string   `json:"size"`
	Background  string   `json:"background"`
	ImageURLs   []string `json:"image_urls"`
}

// ipCreateMaxImageRefs 与画布图生图同值(maxImageRefs),语义同源:
// 厂商侧配额对齐,独立演进互不牵连。
const ipCreateMaxImageRefs = 9

// IPCreate accepts one sheet generation for the IP. The row lands queued
// with canvas_id=0 —— worker 接手后的失败退款/重启恢复纪律与画布任务同路。
func (h *Handlers) IPCreate(c *gin.Context) {
	ipID, ok := bindIPID(c)
	if !ok {
		return
	}
	exists, err := h.IPs.IPExists(c.Request.Context(), ipID)
	if err != nil {
		h.failStore(c, err)
		return
	}
	if !exists {
		apierr.NotFound(c, "IP 企划不存在")
		return
	}
	if !h.requireGateway(c) {
		return
	}

	var in ipTaskInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {character_id, variant_id, sheet_type, model} JSON")
		return
	}
	if in.CharacterID < 1 {
		apierr.InvalidRequest(c, "character_id 必须是正整数")
		return
	}
	if in.VariantID < 1 {
		apierr.InvalidRequest(c, "variant_id 必须是正整数")
		return
	}
	// 角色必须存在且严格属于本 IP(52 号票消费面同款校验;跨 IP 即按
	// 不存在答 404,不泄露其他 IP 的目录)。
	ch, err := h.Characters.GetCharacter(c.Request.Context(), in.CharacterID)
	if errors.Is(err, character.ErrCharacterNotFound) || (err == nil && ch.IPID != ipID) {
		apierr.NotFound(c, "角色不存在或不属于本 IP")
		return
	}
	if err != nil {
		h.failStore(c, err)
		return
	}
	variantName, variantNote, ok := variantFacts(ch, in.VariantID)
	if !ok {
		apierr.InvalidRequest(c, "变体不属于该角色")
		return
	}
	sheetType := strings.TrimSpace(in.SheetType)
	if sheetType == "" {
		// 缺省 = 默认档三视图(前端下拉同默认)。
		sheetType = SheetTypes[0]
	}
	if !validSheetType(sheetType) {
		apierr.InvalidRequest(c, "sheet_type 必须是三视图、多视图、表情集、色板或姿态之一")
		return
	}
	model := strings.TrimSpace(in.Model)
	if model == "" {
		apierr.InvalidRequest(c, "model 不能为空")
		return
	}
	if utf8.RuneCountInString(model) > pricing.ModelNameRunes {
		apierr.InvalidRequest(c, "model 名最多 200 个字符")
		return
	}
	size := strings.TrimSpace(in.Size)
	if utf8.RuneCountInString(size) > pricing.MaxSizeRunes {
		apierr.InvalidRequest(c, "size 最多 64 个字符")
		return
	}
	// 透明背景与画布 image 任务同款校验(37 号票):仅 transparent 一个
	// 枚举值,严格全等不 trim。
	background := ""
	if in.Background != "" {
		if in.Background != "transparent" {
			apierr.InvalidRequest(c, "background 仅支持 transparent")
			return
		}
		background = in.Background
	}
	// 参考图(70 号票 B3:变体已存设定图默认带最近一张,可手动移除):
	// 与画布图生图同一条解引用(data: URI 拒、逐条解析、超限拒)。
	var imageRefs []string
	if len(in.ImageURLs) > 0 {
		if len(in.ImageURLs) > ipCreateMaxImageRefs {
			apierr.InvalidRequest(c, "image_urls 最多 "+strconv.Itoa(ipCreateMaxImageRefs)+" 条")
			return
		}
		for _, raw := range in.ImageURLs {
			resolved, err := h.resolveImageRef(c.Request.Context(), strings.TrimSpace(raw))
			if err != nil {
				h.failImageRef(c, err)
				return
			}
			if utf8.RuneCountInString(resolved) > maxImageRefRunes {
				apierr.InvalidRequest(c, "image_urls 每条最多 4096 个字符")
				return
			}
			imageRefs = append(imageRefs, resolved)
		}
	}
	// 提交前先看价:生图按次轨,未配价/轨道不符的提交注定失败,当场拒绝
	// (画布 image 任务同款)。
	price, err := h.Models.ByModel(c.Request.Context(), model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+model+" 未配置计价,请先在管理端配置价格")
		return
	}
	if err != nil {
		h.failStore(c, err)
		return
	}
	if price.Unit != pricing.UnitCall || price.Call == nil {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+model+" 不是按次计价的生图模型")
		return
	}

	// IP 画面风格(69 号票)读库拼段:读失败不拦生成(风格是增益不是
	// 前置),只留日志按未定处理。
	var style string
	ip, err := h.IPs.GetIP(c.Request.Context(), ipID)
	if err != nil {
		log.Printf("canvastask: read ip %d style for sheet prompt: %v", ipID, err)
	} else {
		style = ip.Style
	}
	prompt := BuildSheetPrompt(sheetType, SheetFacts{
		Name:        ch.Name,
		Alias:       ch.Alias,
		Description: ch.Description,
		Detail:      ch.Detail,
		VariantName: variantName,
		VariantNote: variantNote,
	}, style)

	id, err := NewID()
	if err != nil {
		h.failStore(c, err)
		return
	}
	task, err := h.Tasks.Create(c.Request.Context(), Task{
		ID: id, CanvasID: 0, NodeID: "", IPID: ipID, Kind: KindImage,
		Prompt: prompt, Model: model, Size: size, Background: background,
		ImageRefs: imageRefs,
	})
	if err != nil {
		h.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"task": toTaskJSON(task)})
}

// IPList answers the IP's canvas-free tasks, newest first(兜底枚举;
// 对话框轮询走单查)。
func (h *Handlers) IPList(c *gin.Context) {
	ipID, ok := bindIPID(c)
	if !ok {
		return
	}
	tasks, err := h.Tasks.ListByIP(c.Request.Context(), ipID, listLimit)
	if err != nil {
		h.failStore(c, err)
		return
	}
	out := make([]taskJSON, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, toTaskJSON(t))
	}
	c.JSON(http.StatusOK, gin.H{"tasks": out})
}

// IPGet answers one task scoped to the IP —— id 命名空间按 IP 收窄,别的
// IP 查不到(与画布任务按 canvas 收窄同语义);画布任务(canvas_id>0)
// 一律 404,不借道本面。
func (h *Handlers) IPGet(c *gin.Context) {
	ipID, ok := bindIPID(c)
	if !ok {
		return
	}
	taskID := strings.TrimSpace(c.Param("tid"))
	task, err := h.Tasks.Get(c.Request.Context(), taskID)
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "任务不存在")
		return
	}
	if err != nil {
		h.failStore(c, err)
		return
	}
	if task.CanvasID != 0 || task.IPID != ipID {
		apierr.NotFound(c, "任务不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": toTaskJSON(task)})
}

// variantFacts answers the variant's name and note when it belongs to the
// character;ok=false 否则(变体隶属校验,400 的裁决点)。
func variantFacts(ch character.Character, variantID int64) (name, note string, ok bool) {
	for _, v := range ch.Variants {
		if v.ID == variantID {
			return v.Name, v.Note, true
		}
	}
	return "", "", false
}

func validSheetType(t string) bool {
	for _, want := range SheetTypes {
		if t == want {
			return true
		}
	}
	return false
}

// bindIPID parses the :id path segment as an IP id(正整数;歪客户端答
// 400 而非误导性 404)。
func bindIPID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, "IP id 必须是正整数")
		return 0, false
	}
	return id, true
}

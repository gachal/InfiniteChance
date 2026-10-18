# 47 生成对话框素材库挑选 + 素材最近使用排序

Type: feat
Status: implemented(2026-10-18 实现;口径注记:任务行存已解析地址,重试复用行内地址不再二次解引用——touch 落在提交时的一次性解析上)

## Question

生成对话框(图片/视频模式)添加参考图/参考视频至今只有本机上传一条路(21/24 号票定形),CONTEXT.md「待补」明确记着「素材库挑选作为参考来源(图片模式仅本机上传、视频模式亦同)」为遗留缺口。Agent 节点已有素材库挑选(32 号票 AgentMediaPicker,组件头注释写明「生成对话框的素材库挑选仍留『待补』,本面板不连带」)。事实底座齐备:素材行有 kind(image/video/audio)、`GET /assets` 支持 kind 过滤、参考条结构已有 asset_id 字段、服务端 resolveMediaURL 已把 `/api/assets/{id}/content` 解成真实地址并校验 kind 与角色相符——缺的只是前端接线,后端零障碍。

另:素材枚举恒按创建序(id DESC),没有「用过」的概念;挑选场景需要「最新用过的排前面」,须先给「用过」一个数据口径与写入点。

## Answer

**素材库挑选(图片模式)**:对话框「+」(本机上传)旁加素材库入口,面板只列 kind=image,选中即进参考图条(`ComposerRef`,asset_id + 内容寻址路径)。

**素材库挑选(视频模式)**:同一入口,面板列图/视频/音频(顶部 kind 过滤档,「全部」= 三类);选中按 44 号票默认角色规则落条——图片条目:参考条已有任何图片条目(首帧或参考图)→ 参考图,仅完全空白时默认首帧;视频 → 参考视频;音频 → 参考音频。落条后照常 chip 上切角色;上限守卫(VIDEO_ROLE_CAPS、三类合计 ≤9)与上传同款,超限整条拒收;多选连选按序逐条落(默认角色规则逐条应用:空白条多选图时第一张落首帧、其余落参考图)。

**组件泛化**:AgentMediaPicker 泛化为通用素材选择器 AssetPicker(props:kind 白名单、标题、已选 id 集),Agent 节点现有入口与交互行为不变(泛化重构的回归面);生成对话框两模式共用同一组件;素材面板(AssetPanel)不加「用作参考」动作(out of scope——对话框内挑选已覆盖需求)。

**「用过」口径 = 服务端解引用即算**(前端挑选/插入草稿不计,避免反复选删污染时间戳):canvastask 参考解引用(image_urls / video_refs / image_ref)与 promptgen 的 resolveMedia(Agent 媒体附件、反推、分析)在素材行解析成功的共用路径上 touch last_used_at——校验失败(不存在/无可用地址/角色与种类不符)不 touch;实现注记(2026-10-18):任务行存的是已解析地址,重试复用行内地址不再二次解引用,touch 只落在提交时的一次性解析,「重试另计」需让任务行保留原始引用另票;管理面读取、worker 轮询不算。初始值 = created_at(从未使用的按创建序排在已使用之后)。

**排序**:assets 双方言加列 `last_used_at`;`GET /assets` 加 `sort=last_used` 参数(缺省仍 id DESC,管理端审计视角不变);前端选择器与 AssetPanel 用 last_used 序,管理端素材页保持创建序。

后端改动合计 = 加列 + 解引用点 touch + 列表 sort 参数;引用契约、resolveMediaURL 解析语义、上限守卫零变化。

## 产出面

- `internal/asset`:store 双方言 EnsureSchema 加列 last_used_at(MySQL 走 information_schema 逐列加宽迁移先例)+ 普通索引;Store 接口加 TouchLastUsed;List 支持 sort 参数;handler List 解析 query。
- `internal/canvastask/handler.go` `resolveMediaURL`:命中素材行后 touch(一次任务多参考逐条 touch;校验失败不 touch)。
- `internal/promptgen/handler.go` `resolveMedia`:同款 touch。
- `canvas/web`:`AgentMediaPicker.vue` 改名 `AssetPicker.vue` 并泛化(props 扩展 + 引用同步更新);`GenerationComposer.vue` 加素材库入口按钮;`CanvasEditorView.vue` 两模式接线(kind 白名单:图片模式 [image]、视频模式 [image,video,audio];落条复用 appendComposerRef / appendVideoRef 既有守卫与 44 号票默认角色函数);`composer.ts` 增加从 AssetRecord 构造条目的入口(带 asset_id + 内容寻址路径)。
- `AssetPanel.vue`:listAssets 加 sort=last_used。
- `packages/api`:`ListAssetsParams` 加 sort;`AssetRecord` 加 last_used_at。
- `CONTEXT.md`:「生成对话框」词条补素材库挑选与落条角色规则;「素材」词条补 last_used_at 口径;「待补」清单销账「素材库挑选作为参考来源」。

## 验证点

- 图片模式选择器仅见图片;视频模式三类可见,默认角色按 44 号票规则(空白首图→首帧、已有图→参考图、视频→参考视频、音频→参考音频),chip 可切角色,合计守卫拒收文案与上传一致。
- 提交带素材引用的生成任务后,该素材在选择器与 AssetPanel 升到最前;管理端素材页顺序不变;Agent 媒体/反推/分析同样触发 touch。
- Agent 节点挑选行为不回归(泛化重构回归面)。
- 双方言:MySQL 存量表增列迁移 + SQLite 新建即含;桌面版冒烟。
- `make test` + `make lint` + 前端构建 + 浏览器点验。

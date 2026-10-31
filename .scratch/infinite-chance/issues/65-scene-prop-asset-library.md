# 65 场景/道具资产库(IP 资产库三类实体)(2026-10-31 grilling 定案)

Type: feat
Status: drafted(2026-10-31 grilling 定案;与 64 号票可并行;行业依据:LTX Studio Elements(Characters/Objects/Locations 三类资产建一次全项目复用)与即梦短剧工作室「人物/道具/场景」资产库,调研报告 2026-10-31)

## Question

短剧一致性的刚需不止角色:「同一场景/关键道具跨镜复用」。角色库(51/52 号票)只有 characters 三表。本票把角色库扩为 **IP 资产库三类实体**(角色/场景/道具),为 66 号票工具自动入库与 67 号票 cut 角色引用铺底。

## 定案(2026-10-31 grilling)

1. **两张新表,照 characters 族同构但简化(无造型变体)**:`scenes`(id / ip_id / name / alias 可空 / description(1024) / detail TEXT / created_at / updated_at,索引 (ip_id,id)),`props` 同构;变体语义(场景的日景/夜景、道具的使用态)用 images 的 **label** 表达,不上 variants 表。
2. **images 表**:`scene_images` / `prop_images`(id / scene_id|prop_id / asset_id 纯引用 / label / sort),同构 character_images;label 前端白名单(场景:全景/日景/夜景/局部/其他;道具:多视图/细节/使用态/其他),后端存串不校验(照 character_images 先例)。删除语义照角色库:删实体级联删图行、素材行不动;被引素材删除后图行留「素材不可用」占位可手动移除。
3. **API 照 characters 族平移**(canvas/server JWT 面):`GET /api/ips/:id/scenes`(目录树一次取全)、`/api/scenes` 系 CRUD(含 images 子资源);props 同构。设定图消费 = 逐图「落图节点」素材插入语义 + AssetPicker 挑选,本票不建注入链。
4. **管理 UI**:CharacterDrawer 泛化为「资产库抽屉」三 tab(角色/场景/道具);AssetPicker 加场景/道具分组(角色→变体两级已有;场景/道具→图一级)。
5. **纯文本工具入库**(66 号票执行):工具自动入库只建文本资产(名称/别名/描述/细节),**不自动生成设定图**(花钱+需筛选;即梦式「出图确认」留三期批量联动);设定图人工经生图对话框/上传后挂图行。
6. voice_id 类比不做(场景/道具无音色);IP 归属/删除防护/素材引用哲学全部沿用角色库先例。

## 验证点

- 双方言 EnsureSchema(MySQL+SQLite;「先加列后建索引」纪律沿用)。
- 资产库抽屉三 tab 与 AssetPicker 分组的浏览器点验。

## Out of Scope

- 造型变体表(真需要时另票加)。
- 场景/道具的 Agent 设定注入 chip(52 号票同族,二期);cut 引用场景/道具(67 号票只上 character_ids,场景/道具引用随二期注入链)。

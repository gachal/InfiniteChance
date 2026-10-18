# 49 素材缩略:云厂商原生图片处理的显示层压缩

Type: feat
Status: implemented(2026-10-18 实现;注记:①`ThumbnailParam` 按接口惯例带 ctx——票面草图签名无参 ctx,而 Dynamic 委派活跃驱动需要读 settings,三驱动不使用该参;②云处理 302 带 `Cache-Control: private, max-age=300` 短缓存,content 兜底 302 不带;③前端回落簿粒度 = 显示点 × 原图地址(canvas/web `thumbnail.ts` 共享 helper,两阶段回落:缩略失败先回原图、原图失败才落各自不可用态),admin-web 自拼 `/canvas-api` 前缀同款规则;④GenerationRecordsPanel 仅图片行走缩略派生,视频行与 asset_id=0 厂商直连行保持原地址;⑤`thumbnail_url` 字段的消费策略:AssetRecord 直挂点(素材面板/挑选器)直接吃字段,载荷派生点(chip/历史/节点/记录行)按票面「chip 按 asset_id 派生显示地址」策略由共享 helper 从原图地址派生,「前端零判空」读作「直挂点无需判空直用」;⑥云处理 URL 拼接复用 `asset.PublicAddress` 的基座+对象键规则,不另写一份)

## Question

画布图片节点与素材库(素材面板/挑选器/参考图 chip/Agent 历史)、管理后台素材页的图片显示**全部直接消费原图字节**(`GET /assets/{id}/content` 流式回源),仅靠 CSS 缩小 + `loading="lazy"`——几 MB 的原图在 280px 节点卡片与百 px 级缩略格里白白消耗带宽与首屏时间。期望:显示层用压缩过的小图,且压缩能力**不绑定单一云厂商**(OSS、现有 COS 都要能工作)。

排除项先行定案:原图字节完全不动——下载(`?download=1`)、生图参考图、反推/分析喂模型、灯箱大图**全走原图**,压缩只发生在「显示」这一面;视频封面帧缩略另票(服务端提帧需 ffmpeg,依赖重,本票只做图片)。

## Answer

**路线定案(grilling Q1,用户选 b):云厂商原生图片处理,动态拼参,不落第二份字节**。无生成时机、无存量回填、无对象存储写路径改动——存储里每张图仍只有原字节一份,缩略是「读时的 URL 变换」。这也是它对多厂商通用的根本原因:能力长在我们拼 URL 的方式上,不挑存储。

**语义抽象(grilling Q2' 选①)**:单档「限定框 768 内等比缩小、只缩不放、不转格式」。各家参数:

- COS:`?imageMogr2/thumbnail/768x768>`(归属**数据万象 CI**,需控制台给桶开通并绑定,按处理次数计费,单价以官网为准);
- OSS:`?x-oss-process=image/resize,m_lfit,w_768,h_768,limit_1`(IMG 为 OSS 内置能力,按次计费);
- local 恒不支持(空参数)。

只缩不转格式下 GIF 动画、PNG alpha 两家都自动保真,无首帧/透明格式决策;转 WebP 留作将来在拼装处加一段参数的事,零结构改动。

**参数拼装按驱动分发(「原生 SDK × N」哲学,19/23 号票同款)**:`objectstore.Store` 接口加 `ThumbnailParam(longestEdge int) string`(返回带 `?` 前缀的处理参数串;local 与未知驱动恒空 = 不支持),OSS/COS 驱动各实现一份,`Dynamic` 委派活跃驱动——切驱动即时生效,无需任何迁移。

**地址契约(grilling Q5' 选 a):纯 302 端点,object_key 与桶地址继续不出 API**。`GET /assets/{id}/thumb` 与 content 同组注册、**刻意不挂 JWT**(`<img>` 带不了 Authorization,同 10 号票理由,不要"修复"它),语义矩阵:

1. 行不存在 → 404(同 content);
2. 行有 object_key 且活跃驱动支持处理(ThumbnailParam 非空)→ 302 `public_base_url + "/" + object_key + 处理参数`;
3. 其余一切(无 object_key 的历史行、data: URI 行、local 驱动、非图片 kind 的防御分支)→ 302 到 `/api/assets/{id}/content`——content 的既有语义全量兜底(data: 内联、26 号票读兜底、Dynamic 读回退)。

302 响应可带短 Cache-Control 允许浏览器缓存跳转(实现细节,非强制)。

**已知降级路径(接受,不做存在性探测)**:驱动切换前仅存 local 卷的存量对象,在云驱动活跃时缩略 URL 打到云侧 404——每请求 Head 探测不值,由前端 onerror 回落兜底;content 端点的 Dynamic 读回退保证原图始终可读。

**下发与前端(grilling Q4' 定案做回落)**:`AssetRecord` 加 `thumbnail_url` 字段(相对路径 `/api/assets/{id}/thumb`,所有 kind 恒下发,兜底语义收在端点内,前端零判空)。前端一处共享 helper:缩略加载 onerror 时把 src 换回 content_url——CI 未开通、厂商 5xx、存量 local 对象 404 三种降级全靠它,显示点共用。

换用 thumbnail_url 的显示点(仅 `<img>` 展示 src):

| 显示点 | 文件 | 备注 |
| --- | --- | --- |
| 图片节点展示 | `canvas/web/.../nodes/ImageNode.vue` | 下载 href 与灯箱入口仍 data.url |
| 素材面板缩略 | `AssetPanel.vue` | |
| 素材挑选器图片条目 | `AssetPicker.vue` | 视频/音频条目本来就是图标/`<video>`,不动 |
| 生成对话框参考图 chip | `GenerationComposer.vue` | 仅显示面换;条目 `url` 字段是发送载荷,**不动**(chip 按 asset_id 派生显示地址) |
| Agent 历史媒体缩略 | `AgentHistoryPanel.vue` | 同上,`md.ref` 是发送载荷不动 |
| Agent 附件 chip | `AgentNode.vue` | 同上 |
| 生成记录面板 | `GenerationRecordsPanel.vue` | 仅 asset_id>0 行;asset_id=0 的厂商直连地址行无 thumb 端点,保持原图 |
| admin 素材页表格缩略 | `admin-web/.../AssetsView.vue` | 自拼 `/canvas-api/assets/{id}/thumb` 同款前缀规则;大图预览弹窗不动 |

**不换**:灯箱(原图)、一切下载链接、发送链路(参考条/附件的上传与提交 URL 语义零改动)、视频节点 `#t=0.1` 首帧技巧(本票不动,封面帧另票)。

**schema 零改动**:本票双方言都不动表(无新列——缩略不落任何字节与行状态),素材行 `object_key` 语义不变,48 号票去重/删除防护完全不受影响。

## 产出面

- `internal/objectstore`:`Store` 接口加 `ThumbnailParam`;`filesystem.go`/`oss.go`/`cos.go` 三驱动实现;`Dynamic` 委派。
- `internal/asset`:thumb 端点(302 语义矩阵,挂 RegisterContentRoutes 同组)+ 列表/上传响应加 `thumbnail_url`。
- `packages/api`:`AssetRecord` 加 `thumbnail_url`。
- `canvas/web`:共享 helper + 7 处显示点替换;`admin-web`:AssetsView 表格缩略替换。
- `CONTEXT.md`「素材」词条回写「素材缩略」;AGENTS.md 硬性约束不动(无新对象、无键布局变化、无鉴权面变化)。
- 测试:thumb 语义矩阵(有/无 object_key × 云/local 驱动 × 图片/非图片/历史行)、双方言零 schema 改动确认、`make test` / `make lint` / 前端 build。

## 部署侧动作(代码外)

- COS:腾讯云控制台开通数据万象并绑定桶(按处理次数计费,单价以官网为准);
- OSS:确认 IMG 可用(内置,按次计费);
- local / 桌面版:零动作(不启用,显示原图即现状)。

## 验证点

- 两家真桶实测:768 限定框语义(横/竖/方图)、只缩不放(小图原样回)、PNG alpha 保真、GIF 动画保真、CI 未开通/参数被拒时的错误形态(404 或 4xx,前端 onerror 均能接住)。
- 驱动切换降级:local 写的旧对象 + 云驱动活跃 → 缩略 404 → onerror 回落原图可读。
- 回归:灯箱、下载、参考图提交、反推/分析仍走原图字节;视频缩略(`#t=0.1`)行为不变;发送载荷 URL 与改造前逐字节同形。
- 浏览器点验 8 处显示点 + admin 素材页,验证 302 跳转在 dev 代理 / 部署 nginx / 桌面 webmux 三形态下均通(桌面应恒落 content 兜底)。

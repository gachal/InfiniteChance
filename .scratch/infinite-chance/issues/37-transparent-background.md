# 37 生图透明背景:background 参数贯通网关与画布

Type: grilling
Status: implemented(2026-10-03 grilling 定案;同日实现。edits 通路 ExtInfo 已有契约级测试覆盖,真 key 实测仍待补;画布全链路 dev 点验待做)

## Question

og-image-2.5(tencent-vod 渠道)上游支持透明背景,画布如何才能选到?现状是三层断点:画布任务链字段封闭(`canvas_tasks`/worker 无参数槽位)、网关 VOD adaptor 白名单丢弃未知参数(`vodImagesRequest` 只认 model/prompt/n/size/ratio/image)、上游构造体只有 ModelName/ModelVersion/Prompt/OutputConfig{StorageMode,Resolution,AspectRatio,OutputImageCount}——透明能力在现有链路上完全不可达。

## Answer

1. **上游机制(2026-10-03 实证)**:腾讯 VOD `CreateAigcImageTask` 无独立背景参数;透明图层 = `ExtInfo` 双层 JSON 编码字符串 `{"AdditionalParameters":"{\"background\":\"transparent\"}"}`(SDK `ExtInfo` 注释文档级证据)+ `OutputConfig.OutputFormat: "png"`。直调实测(`OG image2.5_sunburst_low` 档、SubAppId 通路、提示词刻意不含透明字样):产物真 PNG,四角 alpha=0、透明占比 35.4%——透明归因于 ExtInfo 而非提示词,「提示词+png」路线否决。产物基线 `/tmp/vod_t1.png`。顺带发现 `AdditionalParameters` 另支持自由分辨率(像素 16 整除、总面积 65.5 万–829 万),记「待补」不展开。

2. **网关契约 = OpenAI 风格 `background` 参数(定案)**:`/v1/images/generations` JSON 体与 `/v1/images/edits` multipart 均收 `background`,MVP 仅收 `transparent` 一个枚举值,其他值(`auto`/`opaque` 等)400 拒绝、报错信息写明当前仅支持 transparent;缺省不传 = 现状行为逐字节不变。openai 型渠道全量透传(OpenAI 原生 gpt-image 有同名参数,一套契约两类渠道都天然正确);VOD adaptor 翻译为 ExtInfo + 强制 `OutputFormat=png`(generations 白名单加字段、edits multipart 解析加字段,共用同一段翻译)。**generations + edits 全覆盖**——语义收敛为「开 = 透明,无论有无参考图」,不做「有参考图静默失效」裂缝;worker 的 edits 表单加同一字段,增量一行。

3. **画布链路 = 加列**:`canvas_tasks` 原地加 nullable 列 `background`(VARCHAR/TEXT,MySQL 与 SQLite 双方言 EnsureSchema,ADR 0001 纪律);createInput 绑定;worker 提交时非空即上送。对齐 24 号票 `ratio`/`video_refs` 加列先例,每参数一列、任务行可审计,不引入通用 params JSON 列。

4. **UI = 统一开关,不按模型特判**:生成对话框图片模式参数区(比例 × 分辨率旁)加「透明背景」开关,默认关,**所有生图模型显示**;上游不认时 4xx 原样透出、节点可见可重试(既有原则),不搞「仅 og-image-2.5 显示」的硬编码名单(那需要把渠道能力暴露进模型目录,多一条接线)。开关状态进图片模式独立草稿(与其他参数同款),不上节点 data。生成记录面板参数展示含透明标记。视频模式不受影响。

5. **计价与存储零影响**:次轨按尺寸系数与格式无关;转存链按魔数嗅探落 `.png`、alpha 字节拷贝保真,无改动。

6. **显式 out of scope**:ExtInfo 自由分辨率(另立票)、`auto`/`opaque` 枚举扩展、模型变体式透明(`og-image-2.5-transparent`,已否决:透明不是模型版本,特判注入污染公开名语义)、视频模式透明。

7. **产出面**:`internal/relay`(imagesRequest 校验 + vod adaptor generations/edits 两路翻译)、`internal/canvastask`(加列 + createInput + worker client 两端点)、`canvas/web`(对话框开关 + 草稿)、`packages/api`(请求类型);CONTEXT.md 回写四条目(「生图转发」契约加参数段、「VOD AIGC 渠道」ExtInfo 翻译、「画布任务」background 列、「生成对话框」透明开关)随实现提交。不立 ADR(ExtInfo 诧异点由 adaptor 注释 + CONTEXT.md 承载,够不上架构决策)。

8. **验证点**:edits 通路透明(multipart + ExtInfo)未实测,第一验证点;画布全链路点验(开关 → 任务行 → 产物 alpha,直调基线 `/tmp/vod_t1.png`);openai 渠道为契约级信任(无本地 gpt-image 渠道则只验透传形状);转存抽查 `.png` 扩展名与 alpha;`make test` + `make lint` + 前端构建 + dev 实际点验。

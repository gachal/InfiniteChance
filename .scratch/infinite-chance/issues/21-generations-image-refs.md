# 21 生图参考图:generations 的 image[] URL 形态与 ratio 参数

Type: grilling
Status: implemented（2026-09-29 按草案实现:`internal/relay/vod.go` 扩展——`image` 参数(自定义 UnmarshalJSON 同时接受字符串与数组)、http(s) 校验/data: 拒绝/≤9 张 400、`ratio` 显式优先于 size 推导的 AspectRatio、Url 型 FileInfos 构造、Resolution 改长边 ≥2048 判 2K;relay 骨架与 edits 零改动(openai 渠道全量透传自然携带 image/ratio)。测试:单串/数组形态、约束四例、ratio 优先、长边尺寸表(2048x1152/1152x2048)、端到端 JSON+image[] 断言 FileInfos 与结算,go 17 包全绿;docs/api.md 3.5 增图生图示例。待补:画布素材参考图公网地址联动、网关代下载 Base64 回落）

## Question

图生图能否不再走 `/v1/images/edits` 的 multipart 上传,而是沿用 `/v1/images/generations` 的 JSON 形态、参考图以 URL 数组传入(对齐聚合商通行形态,如 `image: ["https://…", …]`)?参考图怎么交给腾讯(URL 直传 vs 网关代下载)?数量与协议约束?`ratio` 显式宽高比参数要不要接?Resolution 判定按短边还是长边?

## Answer

1. **契约形态:`/v1/images/generations` 的 JSON 请求带 `image` 参数即图生图,不带即文生图**。`image` 接受**单个字符串或字符串数组**(http(s) URL,顺序即参考序,提示词里的「图1/图2」按下标对应);`/v1/images/edits`(multipart 上传字节)保留现状不动,OpenAI 官方 edits 契约完整,两种形态并存。relay 骨架零改动——generations 本就全量透传(仅重写 model),预扣只看 model/n/size,翻译职责全在 VOD adaptor。

2. **参考图传递:URL 型 FileInfos 直传**——adaptor 把 URL 原样填进 `{"Type":"Url","Url":…}`,由腾讯侧自行拉取(参考实现已验证该形态;要求 URL 是腾讯公网可匿名下载的图片直链)。网关零下载零转码,无 SSRF 面。**不做**「网关代下载转 Base64」回落(需要时另立票)。

3. **约束**:每条 `image` 必须以 `http(s)://` 开头,**`data:` 内联 URI 直接 400 拒绝**(12 号票否决内联媒体进网关契约的同款决策);**上限 9 张**(参考实现的生图参考图配额,超限 400 带明确文案,好过腾讯的晦涩报错);空串/非字符串形态 400。

4. **`ratio` 参数(新增)**:显式宽高比,给出时**优先于**从 `size` 推导的 AspectRatio(两者同给以 ratio 为准);不校验枚举交腾讯裁(枚举随模型演进,网关不硬编码)。`size` 仍照常用于计价系数与 Resolution 推导。

5. **Resolution 判定修正:短边 ≥2048 改为长边 ≥2048 判 2K**——横版 `2048x1152`(长边 2048)按旧规则会落 1K,长边判定才符合「2048 系是 2K」的直觉;`1024x1024`→1K、`3072x2048`→2K 等既有取值不受影响,已配价格与映射不受影响。

6. **openai 兼容渠道零改动、不剥离**:`image`/`ratio` 随请求体原样到达上游——上游识别(聚合商兼容层)则生效,不识别则 4xx 原样透回,与「v1 全量透传」既有哲学一致,票内写明即可。

7. **计价**:次轨照旧(单价 × 尺寸系数 × n 预扣,按实交 `data` 张数结算),参考图不另计费。**待补**:画布侧拿素材库图片当参考图,需 18/19 号票的公网地址解析(`asset.PublicAddress`)接入生图动作——另立票;`docs/api.md` 的生图示例随本票更新。

8. **产出面**:仅 `internal/relay/vod.go`(adaptor 解析 `image`/`ratio` + 约束校验 + FileInfos 构造 + 长边判定)与其测试;CONTEXT.md「生图转发」词条修订。

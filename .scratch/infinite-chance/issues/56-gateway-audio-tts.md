# 56 网关 TTS 与音色定制面

Type: feat
Status: decided(2026-10-26 grilling 定案;上游为阿里云百炼 Qwen-Audio-TTS 系列,目标模型 `qwen-audio-3.1-tts-plus`)

## Question

音色设计与克隆要进画布并绑定到角色(57 号票消费)。模型调研结论(2026-10-26 官方文档查证):`qwen-audio-3.1-tts-plus` 为百炼 Qwen-Audio-TTS 系列当前主力档(`qwen3-tts-flash` 反而不支持自定义音色),音色设计(`voice_prompt` 文本描述)与音色克隆(声音样本)走同一端点 `POST /api/v1/services/audio/tts/customization`(`model` 恒 `voice-enrollment`),同步返回 `voice_id` + Base64 试听音频;克隆创建后经 `DEPLOYING → OK/UNDEPLOYED` 状态机生效(靠 `query_voice` 轮询,不是异步任务句柄);**音色绑定 target_model 与工作空间,合成/管理必须打回创建渠道**——打破「同模型多渠道换道」假设;台词合成 `POST /api/v1/services/audio/tts/SpeechSynthesizer` 同步返回约 24h 临时 OSS 地址;上游计价按输入字符(汉字计 2,plus 档 1.4 元/万字符),enrollment 按音色个数(定价页 0.01 元/个,用户指南称免费,以账单为准)。网关现状:零 TTS(无 audio 端点、无 audio 能力位)、计价无字符轨。

本票只落网关面(curl + docs/api.md 可独立验证);画布消费在 57 号票。

## Answer

**渠道与调度**:

- 渠道能力集追加 `audio`(`CapAudio`):音色定制与台词合成只落带 audio 能力的渠道,同一显式开启原则(07/08 号票);历史行 nil = 仅 chat 语义不变。dashscope 渠道类型即持有该能力(46 号票 BaseURL 工作空间域名根与 TTS 端点天然同域)。
- **钉渠道,不换道**:四端点请求体必带 `channel_id`,网关校验渠道存在 + 启用 + 带 audio 能力 + 模型映射命中,然后单渠道尝试;上游临时失败记该渠道熔断账并直接失败(预扣原退),上游 4xx 透传。换道无意义是有意取舍:音色在别家渠道(别的工作空间)必不可用,换道只会制造难懂错误。
- 渠道模型映射需含 `voice-enrollment`(上游同名,音色定制的调度与计费键)与 `qwen-audio-3.1-tts-plus`(合成)。

**端点组**(自定义契约,对齐 /v1/videos 先例;/v1 鉴权照旧;错误统一 OpenAI error object;上游错误体为顶层 `{code,message,request_id}` 形,ErrorSummary 覆写提取,视频同款):

- `POST /v1/audio/voices` — `{model:"voice-enrollment", target_model, mode:"design"|"clone", channel_id, voice_prompt?, audio?, preview_text, prefix?}`。design 必填 `voice_prompt`(≤500 字,上游仅支持中英);clone 必填 `audio`(**仅 http(s) 地址**,`data:` URI 拒绝 `audio_inline_unsupported`,12 号票契约家族;上游自拉,样本 ≤10MB 由上游裁决);`preview_text` 15–200 字;`prefix`(≤10 位字母数字)缺省自动生成。响应 `{voice_id, target_model, status, preview_audio:{data(Base64), sample_rate, format}}`。计费:`voice-enrollment` 次轨。
- `POST /v1/audio/voices/{voice_id}/query` — body `{channel_id}` → 归一 `{voice_id, status: DEPLOYING|OK|UNDEPLOYED, ...}`。管理动作,不计费不落用量行。
- `DELETE /v1/audio/voices/{voice_id}` — body `{channel_id}` → 上游 delete_voice。管理动作,不计费不落用量行。
- `POST /v1/audio/speech` — `{model, voice(voice_id 或系统音色名), text(服务端上限 600 字符;上游 HTTP 单次上限未公布,取 qwen3-tts 同族保守值), instruction?, channel_id}` → 同步 `{audio:{url}, usage:{chars}}`(上游 OSS 约 24h 临时地址,转存是调用方职责,与视频直连同现状;`usage.chars` 为服务端口径供对账)。计费:字符轨。`instruction` = 自然语言演绎指令(可选,透传);输出固定 WAV 24kHz(不暴露格式/采样率参数)、不开 SSML。

**计价**:

- **字符轨**:`Unit` 枚举追加 `UnitCharacter`,价格载荷 `CharacterPrice{ CNYPer10KCharsMicros }`(微人民币/万字符);Normalize 加分支校验;快照 `{unit:"character", request:{chars}, cny_per_10k_chars_micros}`;admin 人类单位 `cny_per_10k_chars`。
- **计费字符口径(服务端自算,不信客户端)**:CJK 汉字与全角字符计 2、其余 rune 计 1;**基数 = text + instruction 之和**——上游是否对 instruction 计费未证实,按「扣费向上取整不低估」取保守口径,真 key 对账证实不计后再放宽。
- 预扣 = ⌈字符数 × 单价 / 10000⌉,请求时即知精确值 → 预扣即实结(成功不补退、失败全额退款);usage_logs `unit=character`、量列存字符数。
- enrollment 次轨:公开模型 `voice-enrollment`,参考价 0.01 元/次(管理员配);计费同生图纪律(预扣→实结→失败退款)。

**adaptor**:dashscope adaptor 扩展四个音频翻译方法(接口加方法族,非 dashscope 类型落显式不支持桩——调度已按能力过滤,桩只作兜底);路径 `{base}/api/v1/services/audio/tts/customization` 与 `{base}/api/v1/services/audio/tts/SpeechSynthesizer`;Bearer 同现状;**不带 X-DashScope-Async**(同步面)。

**转存**:网关不做音频转存(speech 透传上游临时 URL;canvas worker 转存在 57 号票;直连产物转存另票,同视频待补)。

**连通测试不变**(同一把 key,46 号票探测方式照旧;音色链路错误经任务/响应透出)。

## 产出面

- `internal/channel`:`CapAudio` + 能力集校验。
- `internal/pricing`:`UnitCharacter`/`CharacterPrice`/Normalize/快照/admin API 字符轨字段。
- `internal/relay`:四端点路由与 handler(校验、预扣结算、来源标记透传)、字符计数纯函数(独立单测)、dashscope adaptor 音频翻译、钉渠道调度分支。
- `admin-web`:价格页字符轨表单;用量审计 `unit=character` 行展示;渠道表单能力位加 `audio` 勾选。
- `docs/api.md`:四端点 curl 用例。
- CONTEXT.md:「渠道」「阿里云百炼渠道」「计价」词条增补 + 新增「音色」「台词合成」词条(已随定案落笔)。

## 部署侧动作(代码外)

- dashscope 渠道(或新建)能力位勾 `audio`;模型映射加 `voice-enrollment` 与 `qwen-audio-3.1-tts-plus`。
- 配价:`qwen-audio-3.1-tts-plus` 字符轨 1.4 元/万字符(北京地域,2026-09-22 价);`voice-enrollment` 次轨 0.01 元/次。
- 真 key 验证(验证点清单)。

## 验证点

- 单测:字符计数(汉字/全角/混合/emoji/空 instruction)、四端点请求校验(data: 拒绝、长度上限、mode 互斥必填)、adaptor 提交/归一形状、计费算术(微整数向上取整)。
- `make test` + `make lint`;MySQL 集成 skip 语义不变。
- 真 key 全链:design 创建 → DEPLOYING → OK 实际时长;clone(真实 10–20s 样本)创建与生效;UNDEPLOYED(违规描述,如模仿名人)真实报错文案;合成产物采样率/格式实测;**instruction 是否计费**(对账上游账单,决定口径是否放宽);日语等多语言在 `language_hints=["zh"]` 下的表现(决定是否开放);create 响应字段完备性(target_model/status/preview_audio 缺失时的行为 —— 现 adaptor 只校验 voice_id,status 缺失 canvas 侧 QueryVoice 会报错,留待真 key 实证)。

## Out of Scope(另票)

- `list_voice` / `update_voice` 代理(台账以调用方为准);OpenAI 兼容 `/v1/audio/speech` 形状;SSE/WebSocket 流式合成与长文本(continue-task 20 万字);SSML;CosyVoice 等其他 TTS 系列(其 enrollment 体系是否同端点未查证);直连音频产物转存(同视频待补);连通测试加音色探测;per-channel 差异化价格。

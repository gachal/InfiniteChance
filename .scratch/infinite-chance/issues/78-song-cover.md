# 78 歌曲翻唱(参考原曲换语言演唱)——VOD AIGC 聚合 AudioInfos 参考通路

Type: feat
Status: implemented(2026-11-04 定案当日 implement-spec 落 feat/78-song-cover)

## Question

用户需求:音乐节点添加歌曲翻唱功能,如英文歌变成中文歌。音频域现有四物种(配乐/歌曲/音效/台词音频)均不带参考维度;网关 `/v1/audio/music` 契约注释明写「参考翻唱是上游 cover 模式,显式 out of scope」(`relay/music.go`)。本票把「翻唱」立为音频域第 5 物种,打通 VOD 聚合的参考音频通路,并解决「换语言」的歌词来源。

## 事实底座(2026-11-04 调研;`[官方]` = 厂商文档;「未证实」处列真 key 验证点)

- **腾讯云 VOD/MPS 聚合 `CreateAigcAudioTask` 有参考音频参数** `[官方]`:顶层 `AudioInfos.N`(`AigcAudioReferenceAudioInfo`),MPS 官方文档明文「**MiniMaxMusic的翻唱功能使用 AudioInfos**」;VOD 域文档只写「传入音频生成音乐时需要传入」。现通路不用换接口——`AudioInfos` + AdditionalParameters(lyrics/is_instrumental,62 号票已接)即官方翻唱形态。**AudioInfos 字段形状矛盾**:官方数据结构页只有 `AudioUrl` 单字段,第三方文档称 VOD 域用 `Type+Url`——未证实,真 key 打样定形。
- **MiniMax 直连**有两步翻唱(`music_cover_preprocess` 免费 ASR 提原词出 `formatted_lyrics` → 改词 → `music-cover` 以 lyrics+prompt+cover_feature_id 生成),但 `[官方]`「自 2026-08-20 起付费接口(音乐生成、歌词生成)不再面向新用户提供服务」——直连不可入。TokenHub(腾讯云 MaaS)有 `minimax-music-v3.0/v2.6` 但文档无参考音频/翻唱参数。
- **Mureka** 聚合侧 AdditionalParameters 透传 `reference_id`/`melody_id`,但该 ID 须经 Mureka 官方 `files/upload` 获取、经腾讯聚合如何取得**未证实**——列后续,本票不做。
- **聚合侧不回传原曲歌词**(直连的 `formatted_lyrics` 模式用不上)→ 英文原词用户自备;这也是「翻译链做成确认式而非全自动」的事实前提。
- **刊例价**(VOD 按量计费(新),2026-11-03 版):MiniMaxMusic 2.0 = 0.25 元/首、2.5/2.6/3.0 = **1.00 元/首**;Mureka = 0.33 元/次;GL 3.0/3.0-pro = 0.300/0.600 元/次;EL 按秒。
- **代码底座**:`musicRequest{Model, Prompt, Instrumental, Lyrics}`(`relay/music.go`)注释预留参考扩展点;MPS 族 adaptor `MusicSubmit` 只送 `{model, prompt}`、写死 `is_instrumental:true`(62 号票「MPS 静默降级」边界)——带参考请求若落 MPS 渠道会**静默丢参考**,调度面必须显式防;参考素材解引用链现成(`canvastask/handler.go` `resolveAudioSample`:素材 id → kind=audio 校验 → 解地址,touch last_used);worker 重提纪律(62 号票 instrumental 列的存在理由);同步聊天通道现成(`promptgen.Client.GenerateChat`,token 计费与 `X-InfiniteChance-Source` 归属现成);音频素材进料链全通(上传嗅探/素材面板/音频节点)。

## 定案

1. **物种与入口**:音频域第 5 物种「**翻唱(cover)**」——以参考原曲音频为旋律/编曲基底、以(通常换语言的)新歌词重新演唱的音乐生成产物;与配乐/歌曲/音效/台词音频互不混用,UI 与文档按「翻唱」命名。音频节点 `source` 联合加 `'cover'`(第三创作态,兼作意图标记,composer 对账不覆盖);右键菜单「创作」段追加「翻唱」条目(与配乐/音效并列的第三音频入口,落 audio 节点带 `data.source:'cover'`;77 号票二级菜单创作段 8 条 → 9 条)。
2. **表单**(创作态 cover 形态,照 57 号票克隆样本范式):参考音频 chip **必填恰好 1 条**(无参考不可发送——表单校验;参考是翻唱的身份所在;来源 = 本机上传 kind=audio 经 `/api/assets/upload` + 素材库挑选 AssetPicker audio 过滤,双入口照克隆样本 chip);风格描述 prompt(必填 1–2000 runes 照 music,占位文案写明是演绎/风格描述);歌词多行框(选填 ≤3500,**空 = 上游自动写词**,表单注记「留空将由模型自动写词、非原曲歌词」);「英→中」翻译按钮(歌词框旁,空框禁用;点击调翻译端点,结果**回填歌词框替换内容**、人工可再改,发送仍走人工确认——Q2 确认式);模型下拉列全量 `/music-models` 目录、缺省 list[0]、**不做 cover 过滤**(不按模型特判;选非 cover 模型由网关 400 兜底,错误文案指路 cover 公开名);预估价照 music(call 轨单价 ×1)。
3. **渠道与公开名**:渠道类型沿用 `tencent-vod`(music 能力位已备),**独立公开名** `vod-minimax-music-3.0-cover`(model_map → 上游 `MiniMaxMusic 3.0`)——公开名即调度单元,62 号票 `vod-` 前缀防混调度同思路,翻唱只落配置了该名的渠道,天然不与 MPS 同名模型混调度;普通公开名 `vod-minimax-music-3.0` 行为零变化。MiniMaxMusic 2.0 档(0.25 元/首)部署侧自选 model_map 行 + 配价即可,票面不绑、零代码。
4. **网关契约**:`POST /v1/audio/music` 请求体加 **`audio_ref`**(可选单串:厂商 http(s) 地址或素材内容寻址路径,服务端解出真实地址——照 21/24 号票参考解引用先例;`data:` 内联 URI 400 照 12 号票契约;URL 长度上限对齐既有 runes 上限;**缺省不传 = 现状行为逐字节不变**,直连调用方同享此参数)。轮询端点、无取消面、30min 计费时限、call 轨计费(一任务一首,预扣→成功恒等结清→失败退款)全部照 60/62 号票;**music_tasks 网关侧零迁移**(audio_ref 与 lyrics 同待遇:只随请求上送,不落网关任务行)。
5. **adaptor 翻译**:vod 族 `MusicSubmit` 扩参——audio_ref 非空时请求体带 `AudioInfos[{AudioUrl: <已解析地址>}]`(形状按官方结构页 `AudioUrl`;真 key 打样若证实需 `Type+Url` 结构,在 adaptor 单点改),AdditionalParameters 照 62 号票(is_instrumental + lyrics);普通请求(无 audio_ref)提交体逐字节不变。
6. **渠道不匹配语义**:mps adaptor 的 `MusicSubmit` 检测到 audio_ref 非空 → 返回**请求级 400** `invalid_request_error`「当前模型不支持参考翻唱,请选择 cover 系公开模型」——`CreateMusicGeneration` 归为**不换道、不记熔断账**的请求级失败(「计价轨道不匹配同码拒绝」`model_not_priced` 同构先例);**不静默丢参考**(62 号票歌词静默降级边界在翻唱场景不可接受:参考是身份所在,静默丢 = 产物级错误)。
7. **任务行**:`canvas_tasks` 双方言加列 **`audio_ref`**(MySQL TEXT NULL / SQLite TEXT NULL;照 62 号票加列纪律,存量行零迁移,NULL = 普通音乐/歌曲任务)。`createMusicTask` 扩:audio_ref 可选,非空时服务端解引用(素材内容寻址 → 素材行 kind 必须 audio → 解出厂商可达地址**落行**,http(s) 直通,data: 拒;解引用成功 touch `last_used`,47 号票「服务端解引用即算」)——行内落**已解析地址**保持「提交时解析、行自足」纪律(24 号票 video_refs 先例);素材 id 不进任务行(音频节点 data 持有 `ref_asset_id` 供显示与溯源)。kind 仍为 `music`(cover 由前端 source + audio_ref 列区分)。**worker `runMusic` 重提/恢复路径显式携带 audio_ref**(62 号票 instrumental 纪律);轮询/转存/结清零改动。
8. **一键翻译端点**:`POST /canvases/:id/translate-lyrics`(canvas/server JWT 会话面;入参 `{text, model}`:text 1–3500 runes、model 须 token 轨配价(`model_not_priced` 照旧);出参 `{text}`;错误统一 `{"error":{code,message}}` 管理面形状)。翻译指令 = canvas/server **固定常量**(17 号票分析指令先例):翻成中文、逐行对齐原词、音节数尽量贴近原词保可唱性、保留 `[Verse]`/`[Chorus]` 等结构标记;实现走 `promptgen.Client` 同步聊天(stream:false),token 轨计费、来源标记 `canvas=<id> gen=lyrics-translate`;**不进 canvas_tasks、不进生成记录面板**(同步聊天,与「生成提示词」同族);网关零改动。
9. **产物与回填**:产物 = `kind=audio` 素材行落 `canvases/{canvasID}/{taskID}/audio.{ext}`(60 号票 archiveAudio 链照旧),回填音频节点自身;`AudioNodeData` cover 元数据配对 `{ref_asset_id, prompt, model, lyrics}`;composer 对账(audioSyncPatch)扩 cover 字段(62 号票 lyrics 回填先例)。
10. **admin-web 零改动 + 部署欠账**:VOD 渠道 model_map 配 `vod-minimax-music-3.0-cover` → `MiniMaxMusic 3.0`;call 轨配价**估价 1.00 元/首**录入(账单出后核对修正,60/62 号票同款);上游模型白名单与 62 号票欠账同一行覆盖(MiniMaxMusic 3.0)。
11. **前端守卫**:参考音频**不做**大小/时长守卫(聚合侧无明文限制,硬守卫可能误拒;上游裁决 4xx 透出、节点可见可重试的既有原则)。
12. **文档回写(实现时执行)**:CONTEXT.md「翻唱」新词条(物种定义、参考语义、与歌曲的分界:歌曲凭空写词、翻唱以原曲为基底)、「音频节点」词条增补 cover 创作态与翻译按钮、「配乐」词条「音频域四个物种」句改五个物种、docs/api.md 补 `audio_ref` curl 示例与 translate-lyrics 用例。无 ADR(62 号票同款:可逆、无重大架构取舍)。

## 验证点(真 key)

1. **AudioInfos 字段形状**:官方结构页 `AudioUrl` vs 第三方 `Type+Url` 结构——打样定形,必要时 adaptor 单点改。
2. **翻唱实际行为**:旋律/编曲保持度;带中文 lyrics 时的歌词跟随;不带 lyrics 时自动写词的语言倾向。
3. **参考音频实际上限**:时长/大小边界与上游错误文案。
4. MiniMaxMusic 2.0 是否同样吃 AudioInfos(部署自选 model_map 行验证,决定 0.25 元档能否放开)。
5. audio_ref 请求误落 MPS 渠道时:网关 400 文案、不换道、不记熔断账的行为核实。
6. 翻译端点:token 计费入账、来源标记 `gen=lyrics-translate` 落审计、结构标记保留效果。

## Out of Scope

- 声音克隆唱歌(用指定音色演唱;Mureka `vocal_id` / MiniMax 直连 `refer_voice`)。
- ASR 自动提词回填(从原曲音频自动转写歌词;需音频多模态聊天入口或上游回传,现无通路)。
- 音频分离/stem(取伴奏/人声)。
- 非中文目标语言(翻译方向固定中文)。
- Agent 音频输入(听歌写词类多模态)。
- Mureka 参考通路(`reference_id` 经腾讯聚合的获取路径未证实;真 key 证实后加 model_map 行即可,零代码)。
- 时长控制(62 号票已列,上游默认歌长)。

## 落地注记(2026-11-04 implement-spec 同日实现)

- **分支与提交**:`feat/78-song-cover`(基于 main 顶点 948e2a7,--no-ff 并 main):实现四提交 219907e(relay:`/v1/audio/music` audio_ref 契约 + vod AudioInfos + mps 请求级 400)/ 2516e8f(canvas:`canvas_tasks` 双方言加列 audio_ref、createMusicTask 解引用落行 touch last_used、worker runMusic 重提携带)/ e97eeaf(canvas:translate-lyrics 端点,固定指令四要素、token 轨、`gen=lyrics-translate`)/ 15f7117(canvas-web:cover 创作态表单、右键菜单创作段 9 条、英→中翻译按钮)→ docs 回写 616b5d0(CONTEXT.md 翻唱词条 + api.md 用例;折入「@ 引用」条目结构修复)/ a3b20d7(节点词条创作段条目数)→ 评审修复轮 c79eb70(gofmt 测试排版、翻唱文案对齐、翻译空目录 toast)。
- **验证**:make test(go vet + go test 23 包实跑 + vitest canvas-web 553 测)/ make lint / make build(含 vue-tsc)三连全绿;新增 Go 测试 13(relay 8 / canvastask 3 / promptgen 2)、前端 6(validateCoverForm 4、菜单 cover 命令 1、audioSyncPatch cover 1);mps 遇参考「不换道/不记熔断账/预扣原退」全语义钉住(TestRelayMPSMusicRefusesAudioRefAsRequestError);audio_ref 缺省逐字节不变钉住(TestRelayVODMusicCoverCarriesAudioInfos 对照面)。
- **双轴评审**:Spec 轴缺失 0(12 条定案全落);Standards 轴硬违规(docs 提交带票号 ×2)与 CONTEXT.md「@ 引用」条目结构破坏已修(历史整形,保 body);4 个测试文件 gofmt 漂移已修;其余异味(resolveAudioRef 两侧双实现、3500 上限跨包多份等)沿用仓库既有惯例保留。
- **遗留欠账**:① 真 key 验证 6 项(AudioInfos 形状矛盾——官方 `AudioUrl` vs 第三方 `Type+Url`,若证实需 Type+Url 在 `internal/relay/vod.go` MusicSubmit 单点改;翻唱行为/歌词跟随/参考上限/2.0 档兼容/MPS 400 行为/翻译落账);② 部署:VOD 渠道 model_map 配 `vod-minimax-music-3.0-cover` → `MiniMaxMusic 3.0`、call 轨配价估价 1.00 元/首、上游白名单与 62 号票欠账同行;③ 浏览器点验 cover 表单全流程与翻译按钮。
- **域修正轮(2026-11-05)**:真 key 实证 78 定案 3/5/6 的域选错了——翻唱参考通路落地在 **MPS 域**(`mps.tencentcloudapi.com`,862 族文档;`AudioInfos` 单字段 **`AudioUrl`**,实测拒 `Type` 收 `AudioUrl`),VOD 域收下 `Type+Url` 结构却不执行翻唱(DescribeTaskDetail 回显 Input 完整含参考、产物仍自重新编曲,即用户报告的「自己重新编曲」)。修复:`relay/mps.go` 收 audio_ref 发 `AudioInfos[{AudioUrl}]`(直连探针实证接受出片 167s)、`AdditionalParameters` 扩 is_instrumental/lyrics(62 号票 MPS 静默降级边界随之消除)、`mpsView` bucket 改可选(实证无 StoreCosParam 出片);`relay/vod.go` 反转——audio_ref 非空即请求级 400(`errAudioRefUnsupported` 语义反转,不换道不记熔断账,文案指路 tencent-mps 渠道);钉测全部随迁(MPS carry/VOD reject 双向)。**部署欠账更新**:建 `tencent-mps` 渠道(secret_id/key/region,bucket 可选)并把 cover 公开名 model_map 从 VOD 渠道移过去(VOD 行必须删);配价行不动(公开名未变);探针产物(参考+同歌词走 MPS 域)已出片待用户试听验收。

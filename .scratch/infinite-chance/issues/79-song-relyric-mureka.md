# 79 同曲换词(曲不变词变)——Mureka 官方 API Song Edit 直连

Type: feat
Status: implemented(2026-11-05 定案当日 implement-spec 落 feat/79-relyric-mureka;真 key 验证与部署欠账见文末实现收口节)——**通路搁置(2026-11-05,用户裁决按次成本过高)**:真 key 验证不跑,代码保留休眠、留复活权(不建渠道即零流量零成本);同曲换词主通路转 ACE-Step 1.5 自托管,见 80 号票。

## Question

用户诉求:生成「与原曲旋律一致、仅歌词不同」的歌曲(曲不变词变)。78 号票翻唱通路修通 MPS 域后,五轮收束(R5)实证上游参考音频对旋律**零约束**——色谱对撞实验:MPS 双档产物与原曲帧级旋律轮廓对齐 ±0.07–0.08,与已知阴性(旧 VOD 产物)同级,远低于同曲阳性对照 +0.985;上游澄清报告转正。风格级翻唱可达,旋律锁定换词在该通路不可达。本票立音频域第六物种「同曲换词」,另寻通路。

## 事实底座(2026-11-05 调研;`[官方]` = 厂商文档;「未证实」处列真 key 验证点)

- **Mureka 官方功能矩阵 `[官方]`**(mureka.ai):**Reference**(上传参考音频提取风格/速度/情绪/配器)、**Remix**(保留核心旋律和歌词重新编曲,可换人声;2026 升级宣称「旋律更遵循原曲」)、**Song Edit**(官方知识库明文「**单独换歌词(保持旋律不变)** / 单独改旋律(保持歌词不变) / 两者同时重新生成」)。fal 托管端点 `mureka/api/song/edit`:参数 Song Id / Upload Audio Id(支持上传音频作基底)/ Lyrics(必填)/ Edit Start / Edit End(必填,选区制——可覆盖全曲),异步队列,fal 侧 $0.10/编辑区域。
- **价格**:fal Edit $0.10/编辑区域;useapi.net 转售(官方接口兼容层):Section Edit $0.10(V8)、Remix $0.20(V8)、**12 stems 分离含 MIDI $0.70**(Plan B 管线的现成积木);官方订阅制 credit。汇率折算后入 call 轨配价,账单出后核对。
- **MPS 聚合内的 Mureka 不押注**:MPS `CreateAigcAudioTask` 聚合的参考通路只文档化给 MiniMaxMusic(862 文档),MPS 侧 Mureka 档为 instrumental 乐器档(`relay/mps.go` 模型名实证)——MPS 内换上游大概率不通,零代码顺路实验可、不立项。
- **ACE-Step 1.5 `[官方]`**(开源自托管,Plan B):cover 模式以参考音频约束作曲/和弦/结构;flow-edit 支持**局部**换词保旋律人声伴奏,**全曲换词社区无成熟工作流**(GitHub discussion #398);VRAM <4–6GB(offload + 0.6B 模型)~ 8–16GB(1.7B/vllm backend);Apple Silicon 兼容未验证。
- **Suno cover**:官方宣称保旋律、可上传音频,但 API 仅第三方代理(UniFically/APIPASS 等)——封号/ToS/稳定性风险,**定案不碰**。
- **MiniMax 直连两步翻唱**已被 2026-08-20 付费 API 停新关闭(78 号票已证),无新通路。
- **代码底座(78 号票已铺)**:`/v1/audio/music` 契约带可选 `audio_ref`(canvas_tasks 行解引用落地址,「提交时解析、行自足」);渠道不匹配语义双向钉测(vod 检 audio_ref 400 / mps 走 AudioInfos);worker `runMusic` 重提显式携带 audio_ref+lyrics;`translate-lyrics` 端点现成;素材上传/挑选/音频节点进料链全通。**本票网关侧预期零 schema 变更**(复用 audio_ref 列与 music kind,行级语义由公开名区分)。

## 定案

1. **物种与词条**:音频域第六物种「**同曲换词(relyric)**」——以参考原曲音频为**旋律锁定**、以新歌词重唱的音乐生成产物;验收下限**旋律级**(人声旋律与原曲可听辨为同一首,编曲允许模型近似重做),目标态**对轨级**(伴奏沿用原曲、人声逐音符对齐)。与「翻唱」(风格级,MPS 通路)并存分立,词条与 UI 分开命名;CONTEXT.md 词条分裂随定案回写(已写,实现后补实现结论)。
2. **技术路线**:Mureka 官方 API 直连先行,只接 **Song Edit** 模式(正中目标,最小面);通道官方 `api.mureka.ai` 优先,注册/付费不通再评估 useapi.net 转售。实测(色谱对撞复用+人耳终审)达旋律级下限即定 Mureka 为正式通路;不达标走 Plan B 链:ACE-Step 1.5 自托管(Apple Silicon 兼容先验)→ 分轨 MIDI/SVS 管线(另票)。Suno 第三方代理不碰。MPS 内换上游仅作顺路实验不立项。
3. **编辑选区固定全曲**:Edit Start=0 / Edit End=曲长,一次画布提交 = 一次 Edit 调用 = 一 call;上游按区域计费,多段 = 多次调用 = 多次计费,首版不引入选段 UI(留增量)。
4. **渠道与公开名**:新渠道类型 **`mureka`**(普通 HTTPS+Bearer 鉴权,非 TC3;照 adaptor 模式在 `internal/relay` 新增文件并注册渠道类型表);独立公开名 **`mureka-song-edit`**(model_map → 上游 V8/V9.5 档名,真 key 后定档)——公开名即调度单元,同曲换词只落配置了该名的渠道;调度语义照配乐/歌曲(受理时按优先级/权重选渠道、上游临时失败可换道、受理后钉死)。admin-web 渠道表单加 mureka 类型(music 能力位 + API key 字段;密钥处理照硬性约束 1:只写不读、尾 4 位)。
5. **网关契约**:复用 `POST /v1/audio/music` 端点与既有 `audio_ref` 参数,**不新建端点**;audio_ref 请求落 mureka 渠道 → Song Edit 语义(落 vod 维持 400、落 mps 维持 AudioInfos 翻唱,78 号票双向钉测不动)。**mureka adaptor 仅实现 Edit 语义**:audio_ref 非空而 lyrics 空 → 请求级 400 `invalid_request_error`(词是身份必需,Edit 无自动写词;不换道、不记熔断账、预扣原退,78 号票同构);audio_ref 为空的普通音乐请求 → 请求级 400(文案指路普通音乐公开名)。风格 prompt 选填(Edit 是否吃 prompt 未证实,上游有则透传)。music_tasks 网关侧零迁移(audio_ref/lyrics 只随请求上送)。
6. **上游上传链**:Edit 基底须先取得 Mureka upload id(fal 参数 Upload Audio Id;官方 `files/upload` 形态未证实)——adaptor 内:已解析参考地址 →(下载字节)→ Mureka 上传 → upload id → 发 Edit;URL 直传若支持则免下载步。上传失败按请求级失败处理(参考是身份所在,不静默降级)。
7. **任务行零迁移**:`canvas_tasks` 不加列——复用 `audio_ref` 列(78 已加)与 `lyrics`/`instrumental` 列(62 已加),kind 仍 `music`,行级语义由 model 公开名(`mureka-song-edit` vs cover 系名)区分;worker 重提纪律沿用(62/78 号票已实现,零改动)。
8. **画布面**:右键菜单「创作」段第 10 条「**同曲换词**」,audio 节点 `source:'relyric'`(第四创作态,composer 对账不覆盖);创作态表单:参考曲 chip **必填恰好 1**(本机上传 + AssetPicker audio 过滤双入口,57/78 范式;不做大小/时长守卫,上游裁决 4xx 透出)+ 新歌词多行框**必填** ≤3500(无「自动写词」选项——词是身份必需)+ 风格 prompt 选填 1–2000 + 「英→中」翻译按钮复用 `translate-lyrics`(译文回填替换、人工可再改、发送人工确认)+ 模型下拉列全量 `/music-models` 目录、缺省 list[0]、**不做 relyric 过滤**(选非 mureka 名由网关 400 兜底,78 号票「不按模型特判」纪律)+ 预估价 call 轨 ×1;data 加 `ref_asset_id` 与 `lyrics`(与 cover 同形),提交 = kind=music 任务带 `audio_ref` + lyrics。
9. **产物与归档**:产物 = `kind=audio` 素材行落 `canvases/{canvasID}/{taskID}/audio.{ext}`,回填音频节点自身;`AudioNodeData` relyric 元数据配对 `{ref_asset_id, prompt, model, lyrics}`;composer 对账(audioSyncPatch)扩 relyric 字段(62/78 号票先例)。
10. **部署欠账**:官方注册/credit 充值(通道见验证点 1);建 `mureka` 渠道(API key + base url);model_map 配 `mureka-song-edit` → 上游档名;call 轨配价录入(估价 $0.10 × 汇率,账单出后核对修正,60/62/78 号票同款)。
11. **验收方法**:色谱对撞实验原样复用(78 号票 R5 方法,带同曲阳性对照)作客观判读 + 用户人耳终审;判级:达旋律级下限 → 定 Mureka 正式通路;仅风格级(≈78 号票翻唱水平)→ 走 Plan B 链并回写本票。
12. **文档回写(实现时执行)**:CONTEXT.md「同曲换词」词条补实现结论、docs/api.md 补 `mureka-song-edit` 用例(audio_ref + lyrics 必填、选区全曲语义)。无 ADR(单渠道 adaptor 接入,可逆、无重大架构取舍;Plan B 若立分轨管线另议 ADR)。

## 验证点(真 key)

1. **通道可用性**:官方 api.mureka.ai 注册、credit 购买(国内网络/支付);不通 → useapi.net 转售评估(接口兼容层,价格见事实底座)。
2. **版权墙**:Edit 对任意第三方上传音频(非 Mureka 生成)的接受度——Suno 有版权校验先例,若 Mureka 拒收第三方音频则整条路线降级,**须第一时间验证**。
3. **API 形状**:官方 Edit 端点精确参数面(与 fal 托管参数对齐度)、模型档(V8/V9.5 哪些有 Edit)、`files/upload` 接收形态(URL 直传 vs multipart 字节)、选区/时长/词长上限、输出格式、prompt 参数有无。
4. **旋律保持度**:色谱对撞复用(阳性对照同曲)+ 人耳终审,判级见定案 11。
5. **歌词跟随度**:音节数匹配的新词演唱是否贴旋律;超长词/音节失配时的行为(塌调 or 自动妥协)。

## Out of Scope

- Mureka Reference(参考风格创作)/ Remix(保旋律保词重新编曲)模式(同渠道类型后续增量)。
- 选段编辑 UI(固定全曲;上游按区域计费,多段多计费,首版不引入)。
- MIDI/SVS 分轨管线(UVR 人声分离 + basic-pitch 旋律转 MIDI/f0 + f0 引导歌声合成 + 混音)——Plan B,另票。
- Suno 第三方代理渠道(封号/ToS/稳定性风险,定案不碰)。
- MPS 内换上游(Mureka/ElevenLabs 聚合档)——参考通路只文档化 MiniMaxMusic,顺路实验可、不立项。
- 声音克隆指定音色演唱(Mureka `vocal_id`;78 号票已列)。
- 78 号票翻唱通路的改动与收口(人耳终审另行闭,保留推翻权)。

## 实现收口(2026-11-05 implement-spec,当日定案当日落)

**分支拓扑**:集成分支 `feat/79-relyric-mureka` 收四实现支(--no-ff):admin-web `3243cc3`(渠道表单 mureka 类型)、画布 `1d9c54b…9c2e150`(右键第 10 条 + `source:'relyric'` 第四创作态全链,6 笔)、网关 `89322e1…8592fcc`(渠道类型 + Song Edit adaptor + prompt 豁免 + 文档回写,6 笔)、评审修复轮 `37f420e…4ded398`(5 笔);已 --no-ff 并 main,push 未做。

**实现关键裁决**(实现时定,不回改票面):
- prompt 选填的网关落地 = 「audio_ref 非空 && lyrics 非空 && 该模型候选渠道全部为 mureka」豁免,校验移到调度之后(vod/mps 的 promptless 语义与 78 钉测字节不动);canvas handler 同款豁免(audio_ref+lyrics 齐备);双缺形状先报歌词必填(修复轮对齐定案 5)。
- 上传链失败(下载/上传)= 请求级 sentinel(定案 6 字面),文案区分三 sentinel(缺词 / 缺参考 / 上传失败);参考下载超 32MiB 由静默截断改显式拒绝(修复轮)。
- mp3 时长嗅探容忍 ID3v2 头、ID3v1/APE 尾标与尾部杂标(修复轮),m4a 走 mvhd 不受影响。

**[官方] 钉定事实**(platform.mureka.ai,2026-11-05):提交 `POST /v1/song/region-edit`、轮询 `GET /v1/song/query/{task_id}`、上传 `POST /v1/files/upload`(multipart/form-data,`file` 与 `url` 互斥,`purpose` 必填枚举,音频 mp3/m4a ≤10MB);Edit 拒收 `model` 参数(useapi 实证)→ 提交体不透传模型档。

**验证**:make test / lint / build 三绿;go 23 包全过(MySQL 实跑非 skip),mureka 相关 29 例;canvas-web vitest 560/560;78 号票 `music_cover_test.go` 原样全绿(vod/mps 字节不动实证)。

**真 key 验证点(部署后按序,①不过则路线降级走 Plan B 链并回写本票)**:
1. 版权墙:region-edit 对第三方上传音频的接受度(先打这个再谈接入);
2. API 形状:region-edit 请求字段名(现按 fal 形状 `audio_id`/`lyrics`/`edit_start`/`edit_end` 兜底)、`edit_end` 可否省略(可省则选区函数免嗅探)、上传 `purpose` 取值(现 `audio`)与应答 id 字段(现从宽 `id`/`file_id`);
3. 状态词官方枚举(现按常见同义词收敛,防 MergeStatus 误杀);
4. 产物 URL 字段(现三候选形状解析);
5. 旋律保持度:色谱对撞复用(带同曲阳性对照)+ 人耳终审,判级见定案 11。

**部署欠账**:官方注册/credit 充值;建 `mureka` 渠道(API key + BaseURL 裸域名 `https://api.mureka.ai` + music 能力位);model_map 配 `mureka-song-edit` → 任意非空档名(不透传上游,仅过调度命中);call 轨配价录入(估价 $0.10 × 汇率,账单出后核对);`make up --build` 重建部署。

**遗留**:浏览器点验(右键第 10 条落节点 → relyric 表单全链、产物卡元数据、帮助浮层词条)、Wails WebView 实测;78 号票人耳终审另行闭。

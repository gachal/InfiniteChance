# 62 歌曲生成(带歌词人声音乐)——VOD AIGC 聚合渠道

Type: feat
Status: drafted(2026-10-28 grilling 定案;实现前无需再 grilling,真 key 验证点见文末)

## Question

60 号票定案把「歌曲」物种划走另票(CONTEXT.md「歌曲」词条:带歌词与人声的音乐生成产物,「一旦开放歌词或人声开关即进入歌曲域,需要时另票」)。本票落地:画布音频节点支持带歌词的歌曲生成,渠道从阿里/火山/腾讯三家中选型。

事实底座(2026-10-28 调研;`[官方]` = 厂商文档或用户提供的接入指南摘录):

- **腾讯云 VOD AIGC 聚合**(用户提供的【通用】VOD AIGC服务接入指南,更新记录至 2026-10-23,3.13 章)`[官方]`:生音频走 `CreateAigcAudioTask` @ `vod.tencentcloudapi.com`(service=vod, 2018-07-17),查询 `DescribeTaskDetail`,任务子对象 `AigcAudioTask`(现有 `vodTaskKey` 正则 `^(Aigc|SceneAigc)\w*Task$` 已命中)、产物 URL 在 Output 子树(`collectURLs` 已收 FileUrl)。`SceneType=music`;文生音乐模型:MiniMaxMusic(2.0/2.5/2.6/**3.0**)、GL(Google Lyria,3.0-clip/3.0-pro)、Mureka(**song_8/song_9**)、EL(compose_v2)。MiniMaxMusic 与 Mureka 的 `lyrics`、`is_instrumental` 经 `AdditionalParameters`(JSON 序列化字符串)透传;GL 靠 prompt 拼接规则(有词+风格 / 无词自动写词 / 纯音乐 `, instrumental, no vocals.` 三场景)。OutputConfig:`StorageMode` **Temporary(临时 URL 有效期 7 天)**/Permanent(FileId)、`OutputAudioFormat`(mp3/wav/flac)、音乐 Duration「默认不填」(模型自定歌长)。渠道凭据(secret_id/secret_key/region/sub_app_id)与 20 号票生图共用且已在用,零新配置。
- 同指南 3.13 章亦含**文生音效**(Kling `SceneType=sfx` Duration [3,10] 秒、EL sound_t2s_v2 [1,30] 秒)——大陆可达的独立音效上游出现,触发 61 号票自设的「渠道选择重开」验证点;61 号票同日重开改写(渠道前提从 ElevenLabs 换 tencent-vod),本票先行实现。
- 阿里云百炼 Fun-Music(`fun-music-v1`)`[官方]`:歌词入参齐全(中文 5–350 字),但**邀测阶段无公开申请入口**,且同步 POST/SSE 契约(无任务轮询)——挂起。
- 火山引擎豆包音乐模型(`GenSongV4`/`QuerySong`)`[官方]`:公开可开通(企业认证)、异步契约与 musictask 同形、功能最全(歌词精准生成+Duration 30–240s),但不在方舟体系,需新渠道类型 + 火山 AK/SK HMAC-SHA256 签名——列后续渠道。
- MPS(tencent-mps)侧 MiniMaxMusic/Mureka 亦支持歌词(MPS 文档 862/132830),但 60 号票 adaptor 写死 `is_instrumental:true`,且 60 号票真 key 验证仍在欠账;**本票歌曲改走 VOD,MPS 渠道保持纯音乐现状不动**。

## 定案

1. **渠道与模型**:渠道类型沿用 `tencent-vod`,新增声明 `music` 能力位。首批模型两个:`MiniMaxMusic 3.0` 与 `Mureka song_9`(model_map 上游名以空格拆 ModelName/ModelVersion,照生图同款)。**公开名带 `vod-` 前缀**(`vod-minimax-music-3.0`、`vod-mureka-song-9`),不与 MPS 侧同名模型混调度——VOD 刊例未核前防换道误选。GL(拼接式翻译是另一套 adaptor 逻辑)、火山、阿里列「后续渠道」,不在本票。
2. **契约**:`POST /v1/audio/music` 请求体在 `{model, prompt}` 上加 `instrumental`(bool,**缺省 true,60 号票既有调用完全兼容**)与 `lyrics`(可选字符串)。校验:prompt 必填 1–2000 runes 不变;lyrics ≤3500 字符(同模型 TokenHub 直连口径,VOD 透传无额外限制,真 key 复核);**`instrumental:true` 且携带 lyrics → 400 `invalid_request_error`**(显式拒绝优于静默忽略)。`instrumental:false` 无 lyrics = 模型自行写词(上游默认行为兜底,不做编排)。`GET /v1/audio/music/tasks/{id}`、`GET /v1/audio/music-models`(music 能力 ∩ call 配价)、OpenAI error 形状、**无取消面、30min 计费时限**全部照 60 号票。
3. **adaptor**(`internal/relay` vod 族扩展):`MusicSubmit` = CreateAigcAudioTask——`SceneType:"music"`、`Prompt`、`AdditionalParameters` 按模型映射(**MiniMaxMusic 翻 `is_instrumental` + `lyrics`;Mureka 只翻 `lyrics`**,其纯音乐参数表达未证,真 key 验证点)、`OutputConfig{StorageMode:"Temporary", OutputAudioFormat:"mp3"}`、`SubAppId`;`MusicQuery` = DescribeTaskDetail → `vodTaskResult` 提取后**状态词必须翻译成 MPS 词表再归一**(WAITING→WAIT、RUNNING→RUN、SUCCESS/FINISH/DONE→DONE、FAIL/FAILED/ERROR→FAIL)——`musictask.MergeStatus` 对未知态归 failed,直透 VOD 词会把在途任务误杀成失败。复用 `vodView`/`vodCall`/`vodErrorStatus`,分发在 `adaptorFor` 加 tencent-vod 的 music 分支。
4. **计费**:call 轨按次,一任务一首,预扣→成功恒等结清→失败退款照 60 号票;VOD AIGC 音乐刊例未公开收录(公开渠道搜不到,2026-10-28),单价**估价录入**、`model_not_priced` 兜底、账单出后核对修正(60 号票 MPS 同款处理)。
5. **任务行**:music_tasks 零迁移(lyrics 与 prompt 同待遇,不落网关行);歌词只落 canvas 侧。
6. **canvas**:`canvas_tasks` 加 `lyrics` 列(**MySQL 与 SQLite 两条 EnsureSchema 同改**;SQLite 加列须先于索引建,48 号票教训);`createMusicTask` 校验扩 lyrics(≤3500);`client_music.go` MusicRequest 扩字段;`music.ts` 校验与上限常量;`AudioNode.vue` 创作态表单加「人声歌曲」开关 + 歌词多行框(开关开才显示、选填);`graph.ts` AudioNodeData 加 `lyrics`、`source` 联合加 `'song'`(提交时 instrumental=false 记);`composer.ts` audioSyncPatch 回填 lyrics;`packages/api` createCanvasTask body 扩字段。产物仍单音频落 `canvases/{canvasID}/{taskID}/audio.{ext}`,worker archiveAudio/轮询/恢复句柄全部照 60 号票。
7. **admin-web**:零改动(tencent-vod 渠道 config 字段齐备、music 能力勾选现成)。部署时对 VOD 渠道勾 music、配 model_map、配价。
8. **文档回写**(实现时执行):CONTEXT.md「歌曲」词条转正(暂无链路 → 62 号票 VOD 渠道)、「配乐」词条物种边界句更新、「渠道」词条 tencent-vod 段补 music 能力位;docs/api.md 补 `instrumental`/`lyrics` 字段 curl 示例。

## 验证点(真 key)

- VOD 控制台「用量统计 > 生图/生视频 > 权限管理」给 MiniMaxMusic 3.0、Mureka song_9 开模型白名单(指南 2.5.4 节:配额/模型白名单/IP 白名单在此配)。
- `instrumental:false` + lyrics 全链出人声歌曲;无 lyrics 自动写词;`instrumental:true` + lyrics 网关 400。
- Mureka song_9 透传 lyrics 实测;其纯音乐表达(`is_instrumental` 对 Mureka 是否生效)顺带验证。
- 临时 URL 7 天窗口实测与 archiveAudio 转存;mp3 输出过素材嗅探。
- 账单出后核对刊例,修正估价配价。

## Out of Scope

- 时长控制字段(VOD 音乐 Duration 默认不填,模型自定歌长;要控时长另票)。
- 模型实际采用的歌词回填节点(火山式 Lyrics/Captions 元数据)。
- GL 的 prompt 拼接式翻译、火山豆包音乐渠道(需新签名)、阿里 Fun-Music(邀测 + 同步契约)。
- MPS 侧歌曲(`is_instrumental` 放开)——本票不动 MPS。
- lyrics_optimizer 式独立自动写词端点(自动写词由上游模型行为兜底)。

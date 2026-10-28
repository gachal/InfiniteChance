# InfiniteChance API 示例

全部端点的可复制调用示例。路由与请求/响应形状以代码为准(本文由 `internal/wiring/wiring.go` 及各 handler 的路由注册对照生成)。示例统一使用以下变量:

```bash
GATEWAY=http://localhost:8080   # 网关(中转面 /v1 + 管理面 /admin + /auth)
CANVAS=http://localhost:8081    # 画布服务(画布面)
GWKEY="sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"  # 网关发放的 API key(中转面)
TOKEN="eyJhbGciOiJIUzI1NiIs..."                        # 管理员 JWT 会话(管理面/画布面)
```

两种鉴权互不混用:

- **中转面 `/v1`**:`Authorization: Bearer <sk- 开头的 API key>`;
- **管理面 `/admin` 与画布面**:管理员登录拿到的 `Authorization: Bearer <JWT>`。

错误形状:中转面统一 OpenAI error object(`401 invalid_api_key`、`429 insufficient_quota`、`404 model_not_found`、`400 model_not_priced`、`502 upstream_error` 等,code 区分原因);管理/画布面统一 `{"error":{"code","message"}}`。

中转面任意请求可带 `X-InfiniteChance-Source: <标记>` 审计注记(画布侧写 `canvas=<id> task=<ct_…> node=<节点id>`,直连流量可不带)。

---

## 一、健康检查

```bash
curl $GATEWAY/healthz     # 200 {"status":"ok"} / 503 {"status":"degraded",...}
curl $CANVAS/healthz
```

## 二、认证(/auth,公开)

```bash
# 是否已初始化(首启引导依据)
curl $GATEWAY/auth/status
# → {"initialized":false}

# 首次初始化:创建唯一管理员,返回 JWT(仅未初始化时可用)
curl -X POST $GATEWAY/auth/init -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"a-strong-password"}'
# → {"token":"eyJ...","expires_at":"2026-10-06T09:00:00Z","username":"admin"}

# 登录(HS256 JWT,7 天有效)
curl -X POST $GATEWAY/auth/login -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"a-strong-password"}'
# → 同上形状

# 当前会话
curl $GATEWAY/auth/me -H "Authorization: Bearer $TOKEN"
# → {"username":"admin","expires_at":"2026-10-06T09:00:00Z"}
```

## 三、中转面(/v1,API key)

### 3.1 模型目录

```bash
curl $GATEWAY/v1/models -H "Authorization: Bearer $GWKEY"
# → {"object":"list","data":[{"id":"gpt-4o","object":"model","owned_by":"infinitechance"},...]}
```

### 3.2 聊天补全(缓冲)

```bash
curl -X POST $GATEWAY/v1/chat/completions \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role":"user","content":"用一句话解释熔断器"}]
  }'
# → 标准 OpenAI 形状;usage 按上游实报结算(倍率计价)。
```

### 3.3 聊天补全(流式 SSE)

```bash
curl -N -X POST $GATEWAY/v1/chat/completions \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "stream": true,
    "stream_options": {"include_usage": true},
    "messages": [{"role":"user","content":"写一首俳句"}]
  }'
# → data: {...}\n\ndata: {...}\n\ndata: [DONE]\n\n(逐帧透传;客户端自带
#    include_usage 时末尾收到用量专用块,不带则该块被吞、只用于记账)
```

### 3.4 生图(文生图,OpenAI 兼容渠道)

```bash
curl -X POST $GATEWAY/v1/images/generations \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-1",
    "prompt": "月光下奔跑的猫,赛博朋克风格",
    "n": 1,
    "size": "1024x1024"
  }'
# → {"created":1759142400,"data":[{"url":"https://.../img.png"}]}
#   (上游回 b64 时为 {"b64_json":"..."};按次计价,实交张数结算)
```

### 3.5 生图(文生图,腾讯云 VOD AIGC 渠道)

请求与 3.4 完全同形——渠道类型是服务端配置,调用方无感知:

```bash
curl -X POST $GATEWAY/v1/images/generations \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{
    "model": "og-image-2.5",                  # 公开名,渠道映射到 "OG image2.5_sunburst"
    "prompt": "月光下奔跑的猫,高精度大画幅",
    "n": 2,                                    # 1-8(VOD OutputImageCount 上限)
    "size": "1536x1024"                        # 解析为 VOD Resolution+AspectRatio
  }'
# → {"created":...,"data":[{"url":"https://...vod.../a.png"},{"url":".../b.png"}]}
#   恒返 url(VOD 产物即 FileUrl);内部「提交+轮询」同步封装,最长 10 分钟。
```

带 `image`(参考图 URL,单个字符串或数组)即图生图(22 号票);`ratio` 显式宽高比优先于 `size` 推导:

```bash
curl -X POST $GATEWAY/v1/images/generations \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{
    "model": "og-image-2.5",
    "prompt": "图1是同一位女性的正面特写,图2是她的全身照。保持五官、发型、肤色与参考图一致,穿着图2的黑色长裙,构图参考图2,横版半身近景,直视镜头,真实摄影质感,无文字水印。",
    "n": 1,
    "ratio": "16:9",
    "size": "2048x1152",
    "image": [
      "https://your-bucket.oss-cn-hangzhou.aliyuncs.com/ref-1-face.png",
      "https://your-bucket.oss-cn-hangzhou.aliyuncs.com/ref-2-full.png"
    ]
  }'
# image 条目须是 http(s) 直链且腾讯公网可拉取(data: 内联 400 拒;上限 9 张);
# 数组顺序即参考序,提示词「图N」按下标对应;计费与文生图同轨按张。
```

### 3.6 生图(图生图,edits,multipart)

```bash
curl -X POST $GATEWAY/v1/images/edits \
  -H "Authorization: Bearer $GWKEY" \
  -F model=gpt-image-1 \
  -F prompt="让画面下雪" \
  -F size=1024x1024 \
  -F image=@./reference.png
# → 同 generations 响应形状;tencent-vod 渠道会把上传图片转成 Base64 参考图。
```

### 3.7 生视频(异步任务:提交 → 轮询 → 取消)

```bash
# 提交(计费按模型所配轨道:second 轨 = 每秒单价 × 分辨率系数 × 秒数预扣;
# token 轨 = 输出单价 × 估算 tokens 预扣,成功按厂商实报 completion_tokens
# 多退少补;均仅成功计费)
curl -X POST $GATEWAY/v1/videos/generations \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.5",      # 须是 second 或 token 轨计价的公开模型
    "prompt": "一只猫从月光下跑过雪地",
    "seconds": 5,                  # 缺省 5,范围 1-100
    "size": "720p",                # 分辨率档位串(second 轨系数 / token 轨估算表用)
    "ratio": "16:9",               # 显式宽高比,可选,不校验枚举交上游裁
    "image": "https://…/first.png",       # 可选首帧(http(s) 直链;data: 400 拒)
    "last_image": "https://…/last.png",   # 可选尾帧
    "references": [                       # 可选多模态参考,≤9 条
      {"url": "https://…/ref.png", "kind": "image"},
      {"url": "https://…/clip.mp4", "kind": "video"},
      {"url": "https://…/voice.mp3", "kind": "audio"}
    ]
  }'
# → {"task_id":"vt_1a2b3c","status":"queued","model":"seedance-2.5",
#    "created_at":1759142400,"seconds":5,"size":"720p"}

# 轮询(五态:queued/running/succeeded/failed/canceled)
curl $GATEWAY/v1/videos/tasks/vt_1a2b3c -H "Authorization: Bearer $GWKEY"
# → {"task_id":"vt_1a2b3c","status":"succeeded",...,"video_url":"https://.../v.mp4"}
#   失败时带 "error":{"message":"..."}

# 取消(网关本地取消并全额退预扣;幂等,终态后回放账本事实)
curl -X POST $GATEWAY/v1/videos/tasks/vt_1a2b3c/cancel -H "Authorization: Bearer $GWKEY"

# 配价示例:Seedance 视频走 token 轨(折算率按火山公布的每秒 token 数填)
curl -X PUT $GATEWAY/admin/prices -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "public_model":"seedance-2.5","unit":"token",
    "input_cny_per_mtokens":0, "output_cny_per_mtokens":0.28, "ratio":1.0,
    "size_tokens_per_second":{"480p":8664,"720p":21465,"1080p":48299},
    "default_tokens_per_second":21465
  }'

# 配价示例:MiniMax H3 走 second 轨(官方按秒计价,系数键 = 官方档位串
# 大写 768P/2K,基准价按中国站人民币价折算;调用与上完全同款,只换 model)
curl -X PUT $GATEWAY/admin/prices -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "public_model":"minimax-h3","unit":"second",
    "cny_per_call":0.57, "size_factors":{"768P":1.0,"2K":1.63}
  }'
# 阿里云百炼渠道(46 号票)沿用公开名 minimax-h3 时无需另配价 —— 价格
# 按公开模型一份,原生(minimax 渠道)与百炼(dashscope 渠道)同价共用。
```

### 3.8 音色定制与台词合成(四端点,钉渠道)

```bash
# 音色定制统一说明:四端点请求体必带 channel_id(音色绑定创建渠道的工作
# 空间,调度钉死该渠道不换道);渠道须启用、带 audio 能力、模型映射含
# voice-enrollment(定制)与合成模型名。上游错误透传 OpenAI error object
# (code=upstream_error);渠道不存在 channel_not_found(404)、未启用或
# 熔断中 model_unavailable、映射未命中 model_not_found(404)。

# 创建音色(design = 文本描述设计;mode=clone 时必填 audio 且仅收 http(s)
# 地址,data: 内联 400 audio_inline_unsupported)。计费走 voice-enrollment
# 次轨(预扣→实结,失败全额退款);preview_text 15-200 字;prefix 缺省自动
# 生成(≤10 位字母数字);响应含 Base64 试听音频。
curl -X POST $GATEWAY/v1/audio/voices \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{
    "model": "voice-enrollment",      # 定制调度与计费键(上游同名)
    "target_model": "qwen-audio-3.1-tts-plus",  # 音色绑定的合成模型
    "mode": "design",                 # design(voice_prompt)或 clone(audio)
    "channel_id": 3,                  # 钉死百炼渠道
    "voice_prompt": "低沉磁性青年男声,语速平稳",  # design 必填,≤500 字
    "preview_text": "这一段台词用来试听音色的整体效果如何。",  # 15-200 字
    "prefix": "zn56"                  # 可选,≤10 位字母数字
  }'
# → {"voice_id":"qwen-audio-3.1-tts-plus-vd-zn56-xxxx","target_model":"...",
#    "status":"DEPLOYING","preview_audio":{"data":"<base64>","sample_rate":24000,"format":"wav"}}
#   生命周期靠 query 轮询:DEPLOYING → OK(可用)/ UNDEPLOYED(审核未通过)

# 查询音色状态(管理动作:不计费、不落用量行)
curl -X POST $GATEWAY/v1/audio/voices/$VOICE_ID/query \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{"channel_id": 3}'
# → {"voice_id":"...","status":"OK"}

# 音频模型目录(57 号票):启用 + audio 能力渠道的映射并集 ∩ 字符轨配价,
# 价格随行走 PriceSummary(38 号票);voice-enrollment(次轨定制键)不进目录。
# 画布服务的 /audio-models(JWT)以服务级 key 代理本端点。
curl $GATEWAY/v1/audio/models -H "Authorization: Bearer $GWKEY"
# → {"models":["qwen-audio-3.1-tts-plus"],"prices":{"qwen-audio-3.1-tts-plus":
#    {"unit":"character","cny_per_10k_chars":1.4}}}

# 删除音色(管理动作,204;删除前先停用引用是调用方职责)
curl -X DELETE $GATEWAY/v1/audio/voices/$VOICE_ID \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{"channel_id": 3}'

# 台词合成(同步):计费走字符轨 —— 计费字符数服务端自算(text +
# instruction 之和;汉字与全角计 2、其余 rune 计 1),预扣 = ⌈字符数 ×
# 单价 / 10000⌉,预扣即实结、失败全额退款;text 服务端上限 600 字符;
# 输出固定 WAV 24kHz;返回的是上游约 24h 临时地址,转存由调用方负责。
curl -X POST $GATEWAY/v1/audio/speech \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{
    "model": "qwen-audio-3.1-tts-plus",
    "voice": "qwen-audio-3.1-tts-plus-vd-zn56-xxxx",  # 自定义 voice_id 或系统音色名
    "text": "你好,世界。",             # 1-600 字符
    "instruction": "用河南话,带笑意",   # 可选演绎指令(计入计费字符)
    "channel_id": 3                    # 须是音色所在渠道
  }'
# → {"audio":{"url":"https://...oss.../out.wav"},"usage":{"chars":16}}

# 配价示例:合成模型走字符轨(1.4 元/万字符,qwen-audio-3.1-tts-plus 档)
curl -X PUT $GATEWAY/admin/prices -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "public_model":"qwen-audio-3.1-tts-plus","unit":"character",
    "cny_per_10k_chars":1.4
  }'

# 配价示例:音色定制走次轨(定价页 0.01 元/个,上游指南称免费,以账单为准)
curl -X PUT $GATEWAY/admin/prices -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "public_model":"voice-enrollment","unit":"call","cny_per_call":0.01
  }'
```

### 3.9 配乐 / 歌曲(异步两面:提交 → 轮询,无取消;60 号票;62 号票歌曲)

```bash
# 配乐统一说明:契约照 /v1/videos 的两面(受理回任务句柄、轮询五态),
# 但无取消端点(MPS 取消接口未查证,超时由计费时限收尾退款)。调度常规
# 语义 —— 受理时按优先级/权重选渠道、上游临时失败可换道、受理后钉死;
# 计费按次(unit=call,一任务一首):提交预扣、成功实结、失败全额退款。
# 渠道类型 tencent-mps(腾讯云 MPS 媒资 AIGC)或 tencent-vod(62 号票歌曲,
# VOD AIGC 聚合的 CreateAigcAudioTask),须启用 + music 能力 + 模型映射
# (上游名填 "ModelName ModelVersion" 空格串,如 MiniMaxMusic 3.0、
# Mureka instrumental_9.5、Mureka song_9);MPS 要求输出 COS 桶,渠道
# config 带 secret_id/secret_key/bucket/region,VOD 走 sub_app_id、产物是
# 临时 URL(约 7 天)。错误形状照 /v1 OpenAI error object。
#
# 62 号票歌曲参数面(同一提交端点):instrumental(bool,缺省 true = 纯
# 音乐,60 号票既有调用完全兼容;false = 人声歌曲)与 lyrics(可选字符串,
# ≤3500 字符,留空由模型自动写词)。互斥校验:instrumental:true 且携带
# lyrics → 400 invalid_request_error(显式拒绝优于静默忽略)。VOD 侧模型
# 公开名带 vod- 前缀(vod-minimax-music-3.0、vod-mureka-song-9);MiniMaxMusic
# 翻译 is_instrumental + lyrics,Mureka 只透传 lyrics。已知边界:MPS 侧模型
# (非 vod- 前缀)收到 instrumental:false + lyrics 会静默产出纯音乐(MPS
# adaptor 写死 is_instrumental:true),选型以 vod- 前缀为准。

# 提交配乐任务(风格描述 1-2000 字,对齐 MPS Prompt 上限)
curl -X POST $GATEWAY/v1/audio/music \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{"model":"minimax-music","prompt":"轻快的电子爵士,带复古合成器,适用于城市夜跑场景"}'
# → {"id":"mt_1a2b…","status":"queued","model":"minimax-music","created_at":1767000000}

# 提交歌曲任务(62 号票:instrumental=false 即人声;lyrics 选填,≤3500 字)
curl -X POST $GATEWAY/v1/audio/music \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{
    "model": "vod-minimax-music-3.0",
    "prompt": "city night run, synthwave",
    "instrumental": false,
    "lyrics": "夜色慢慢降落\n霓虹在雨里闪烁"
  }'
# → {"id":"mt_3c4d…","status":"queued","model":"vod-minimax-music-3.0","created_at":1767000000}
#   instrumental:false 不带 lyrics = 模型自动写词,同样合法。

# 互斥 400 用例:纯音乐(instrumental:true,或缺省)携带歌词直接拒绝,
# 不拨上游:
curl -X POST $GATEWAY/v1/audio/music \
  -H "Authorization: Bearer $GWKEY" -H "Content-Type: application/json" \
  -d '{"model":"vod-minimax-music-3.0","prompt":"x","instrumental":true,"lyrics":"多余的歌"}'
# → 400 {"error":{"code":"invalid_request_error","message":"'lyrics' cannot be
#   combined with instrumental:true; omit 'lyrics' or set instrumental:false
#   for a vocal song.","type":"invalid_request_error"}}
#   lyrics 超过 3500 字符同码拒绝。

# 轮询任务(五态 queued/running/succeeded/failed/canceled;成功带 audio.url
# —— 上游写进渠道指定 COS 桶的临时地址,约 24h 过期,转存由调用方负责)
curl $GATEWAY/v1/audio/music/tasks/mt_1a2b… -H "Authorization: Bearer $GWKEY"
# → {"id":"mt_1a2b…","status":"succeeded","model":"minimax-music",
#    "created_at":1767000000,"audio":{"url":"https://music-xxx.cos.ap-guangzhou…/a.mp3"}}
#   失败时 {"status":"failed","error":{"message":"…厂商原因…"}}

# 配乐模型目录:启用 + music 能力渠道的映射并集 ∩ 按次轨配价,价格随行
# PriceSummary;/v1/audio/models(TTS 字符轨)不受影响。画布服务的
# /music-models(JWT)以服务级 key 代理本端点。
curl $GATEWAY/v1/audio/music-models -H "Authorization: Bearer $GWKEY"
# → {"models":["minimax-music"],"prices":{"minimax-music":{"unit":"call","cny_per_call":0.5}}}

# 配价示例:配乐按次轨(单价未公开时按资源包折算估价录入;未配价一律
# model_not_priced 拒绝)
curl -X PUT $GATEWAY/admin/prices -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "public_model":"minimax-music","unit":"call","cny_per_call":0.5
  }'
```

## 四、管理面(/admin,JWT)

### 4.1 渠道管理

```bash
# 列表(密钥永不回显,只有 has_key + 尾 4 位)
curl $GATEWAY/admin/channels -H "Authorization: Bearer $TOKEN"
# → {"channels":[{"id":1,"name":"openai-main","type":"openai",
#     "base_url":"https://api.openai.com/v1","has_key":true,"key_hint":"…9876",
#     "model_map":{"gpt-4o":"gpt-4o-2024-11-20"},"capabilities":["chat"],
#     "priority":10,"weight":1,"enabled":true,...}]}

# 新建 OpenAI 兼容渠道
curl -X POST $GATEWAY/admin/channels -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "name":"openai-main","type":"openai",
    "base_url":"https://api.openai.com/v1","api_key":"sk-vendor-...",
    "model_map":{"gpt-4o":"gpt-4o-2024-11-20"},
    "capabilities":["chat","images"],
    "priority":10,"weight":1,"enabled":true
  }'

# 新建腾讯云 VOD AIGC 渠道(凭据进 config;api_key/base_url 留空)
curl -X POST $GATEWAY/admin/channels -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "name":"tencent-vod","type":"tencent-vod","base_url":"","api_key":"",
    "config":{
      "secret_id":"AKIDxxxxxxxx","secret_key":"xxxxxxxx",
      "sub_app_id":"1500000000","region":"ap-guangzhou"
    },
    "model_map":{"og-image-2.5":"OG image2.5_sunburst"},
    "capabilities":["images"],
    "priority":10,"weight":1,"enabled":true
  }'
# → config 里敏感键(名字含 secret/key)回显为空串 + config_hints 尾 4 位:
#   "config":{"secret_id":"","secret_key":"","sub_app_id":"1500000000","region":"ap-guangzhou"},
#   "config_hints":{"secret_id":"…3456","secret_key":"…7890"}

# 更新(api_key/config 敏感键留空 = 保留已存;整块不带 config 也保留整份已存)
curl -X PUT $GATEWAY/admin/channels/2 -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"tencent-vod","type":"tencent-vod","base_url":"","api_key":"",
       "config":{"secret_id":"","secret_key":"","sub_app_id":"1500000001","region":""},
       "model_map":{"og-image-2.5":"OG image2.5_sunburst"},
       "capabilities":["images"],"priority":20,"weight":1,"enabled":true}'

# 删除 / 一键连通测试
curl -X DELETE $GATEWAY/admin/channels/2 -H "Authorization: Bearer $TOKEN"
curl -X POST $GATEWAY/admin/channels/2/test -H "Authorization: Bearer $TOKEN"
# → openai 渠道:{"ok":true,"latency_ms":231,"detail":"HTTP 200,发现 58 个模型"}
# → tencent-vod 渠道:{"ok":true,"detail":"HTTP 200,凭据与网络正常(腾讯云应答 ResourceNotFound...)"}

# 新建阿里云百炼渠道(46 号票;BaseURL = 工作空间域名根,不含路径)
curl -X POST $GATEWAY/admin/channels -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "name":"bailian-main","type":"dashscope",
    "base_url":"https://ws1234567890.cn-beijing.maas.aliyuncs.com",
    "api_key":"sk-bailian-xxx",
    "model_map":{"minimax-h3":"MiniMax/MiniMax-H3"},
    "capabilities":["videos"],
    "priority":10,"weight":1,"enabled":true
  }'
# → 连通测试:{"ok":true,"detail":"HTTP 404,凭据与网络正常(DashScope 应答 NotFoundTaskId)"}
```

### 4.2 API key 管理

```bash
# 发放(完整 key 只在创建响应出现一次)
curl -X POST $GATEWAY/admin/keys -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"画布服务","initial_quota_cny":20,"expires_at":null}'
# → {"key":"sk-AbCdEf...43位","id":1,"name":"画布服务","prefix":"sk-AbCdEfgh",
#    "quota_cny":20,"status":"active",...}

# 列表 / 充值 / 吊销(幂等)/ 流水
curl $GATEWAY/admin/keys -H "Authorization: Bearer $TOKEN"
curl -X POST $GATEWAY/admin/keys/1/topup -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{"amount_cny":10}'
curl -X POST $GATEWAY/admin/keys/1/revoke -H "Authorization: Bearer $TOKEN"
curl "$GATEWAY/admin/keys/1/quota-log" -H "Authorization: Bearer $TOKEN"
# → {"entries":[{"id":3,"delta_cny":-0.04,"balance_cny":19.96,"reason":"settle",
#    "created_at":"..."},...]}(reason: initial/topup/estimate/settle/refund)
```

### 4.3 模型计价(未配价的模型一律拒绝服务)

```bash
# token 轨(聊天:输入/输出每百万 token 单价 + 倍率)
curl -X PUT $GATEWAY/admin/prices -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "public_model":"gpt-4o","unit":"token",
    "input_cny_per_mtokens":2.5,"output_cny_per_mtokens":10,"ratio":1.2
  }'

# 次轨(生图:按张;未配置的尺寸系数恒 ×1.0)
curl -X PUT $GATEWAY/admin/prices -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "public_model":"og-image-2.5","unit":"call",
    "cny_per_call":0.04,"size_factors":{"1024x1024":1,"2048x2048":2}
  }'

# 秒轨(生视频:每秒单价 × 分辨率系数)
curl -X PUT $GATEWAY/admin/prices -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "public_model":"wan-video","unit":"second",
    "cny_per_call":0.02,"size_factors":{"480p":0.5,"720p":1,"1080p":2}
  }'

# 列表 / 删除
curl $GATEWAY/admin/prices -H "Authorization: Bearer $TOKEN"
curl -X DELETE $GATEWAY/admin/prices/gpt-4o -H "Authorization: Bearer $TOKEN"
```

### 4.4 技能(原提示词模板,29 号票;API 路径不动)

```bash
curl -X POST $GATEWAY/admin/prompt-templates -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"产品文案","description":"围绕主题写一条小红书文案","template":"围绕主题 {topic} 写一条小红书文案","target":"any","enabled":true}'
# template 必须含 {topic} 占位符,否则拒绝;description 可空,target ∈ image|video|any
# (缺省 any,纯展示 + 画布技能浮层筛选,后端零行为)。
curl $GATEWAY/admin/prompt-templates -H "Authorization: Bearer $TOKEN"
curl -X PUT $GATEWAY/admin/prompt-templates/1 -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"产品文案","template":"围绕 {topic} 写抖音口播","enabled":false}'
curl -X DELETE $GATEWAY/admin/prompt-templates/1 -H "Authorization: Bearer $TOKEN"
```

### 4.5 存储设置(19 号票)

```bash
curl $GATEWAY/admin/settings/storage -H "Authorization: Bearer $TOKEN"
# → {"driver":"local","oss":{"endpoint":"","bucket":"","public_base_url":"",
#    "has_access_key":false,"has_secret":false},"updated_at":"..."}

curl -X PUT $GATEWAY/admin/settings/storage -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{
    "driver":"oss",
    "oss":{
      "endpoint":"oss-cn-hangzhou.aliyuncs.com",
      "bucket":"infinitechance-assets",
      "public_base_url":"https://infinitechance-assets.oss-cn-hangzhou.aliyuncs.com",
      "access_key":"LTAI...","secret_key":""
    }
  }'
# AK/SK 只写不读;PUT 携带空 secret = 保留原密,半套密钥拒绝。
```

### 4.6 用量审计

```bash
# 明细(过滤参数全部可选;from/to 为 RFC3339 含头不含尾;limit≤500)
curl "$GATEWAY/admin/usage/logs?from=2026-09-01T00:00:00Z&to=2026-10-01T00:00:00Z&model=og-image-2.5&limit=50" \
  -H "Authorization: Bearer $TOKEN"
# → {"logs":[{"id":9,"key_id":1,"channel_id":2,"channel_name":"tencent-vod",
#    "public_model":"og-image-2.5","upstream_model":"OG image2.5_sunburst",
#    "unit":"call","tokens_in":0,"tokens_out":0,"count":1,
#    "price_snapshot":{"cny_per_call_micros":40000,"request":{"size":"1024x1024","n":1}},
#    "charge_micros":40000,"duration_ms":23450,"status":"success",
#    "source":"canvas=3 task=ct_x1 node=n5","upstream_error":"","created_at":"..."}],
#    "total":1}

# 汇总(三种桶)
curl "$GATEWAY/admin/usage/summary?by=day"  -H "Authorization: Bearer $TOKEN"
curl "$GATEWAY/admin/usage/summary?by=model"   -H "Authorization: Bearer $TOKEN"
curl "$GATEWAY/admin/usage/summary?by=channel" -H "Authorization: Bearer $TOKEN"
```

## 五、画布面(CANVAS 服务,JWT)

### 5.1 画布 CRUD 与整图保存

```bash
curl -X POST $CANVAS/canvases -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{"name":"新画布"}'
# → {"id":1,"name":"新画布","version":0,"created_at":"...","updated_at":"..."}

curl $CANVAS/canvases -H "Authorization: Bearer $TOKEN"          # 列表
curl $CANVAS/canvases/1 -H "Authorization: Bearer $TOKEN"        # 详情(带 graph)

# 整图保存(编辑器自动保存;version 带乐观并发控制)
curl -X PUT $CANVAS/canvases/1/graph -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"version":7,"graph":{"nodes":[...],"edges":[...]}}'
# → {"version":8}

curl -X DELETE $CANVAS/canvases/1 -H "Authorization: Bearer $TOKEN"
```

### 5.2 画布任务(文生图 / 图生图 / 视频生成 / 配乐)

```bash
# 文生图
curl -X POST $CANVAS/canvases/1/tasks -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"node_id":"n_image_1","kind":"image","prompt":"月光下奔跑的猫",
       "model":"og-image-2.5","size":"1024x1024"}'
# → {"id":"ct_1a2b3c","canvas_id":1,"node_id":"n_image_1","kind":"image",
#    "prompt":"...","model":"...","size":"1024x1024","ratio":"","seconds":0,
#    "status":"queued","attempts":1,"asset_id":0,"image_url":"","video_url":"",
#    "error":"","created_at":"..."}

# 视频生成(24 号票对话框范式):video_refs 结构化参考,元素 {url,kind,role};
#   role ∈ first_frame/last_frame/reference_image/reference_video/reference_audio,
#   首帧/尾帧/参考图须 image、参考视频须 video、参考音频须 audio(素材引用按
#   素材行 kind 校验),数量上限 1/1/4/1/1;seconds 缺省 = 自动(不传,厂商
#   缺省;1-100 显式可传);size 为分辨率档位串(480p/720p/1080p);ratio 显式
#   宽高比。无参考 = 文生视频;单串 image_url(12 号票旧形态)仍兼容。
curl -X POST $CANVAS/canvases/1/tasks -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"node_id":"n_video_1","kind":"video","prompt":"猫向镜头跑来",
       "model":"wan-video","seconds":5,"size":"720p","ratio":"16:9",
       "video_refs":[
         {"url":"http://localhost:8081/assets/3/content","kind":"image","role":"first_frame"},
         {"url":"https://cdn.example/ref.png","kind":"image","role":"reference_image"},
         {"url":"http://localhost:8081/assets/9/content","kind":"audio","role":"reference_audio"}]}'

# 列表 / 单查 / 重试(失败任务原地回队)/ 取消(仅视频)
curl $CANVAS/canvases/1/tasks -H "Authorization: Bearer $TOKEN"
# → {"tasks":[...同上形状...]}
curl $CANVAS/canvases/1/tasks/ct_1a2b3c -H "Authorization: Bearer $TOKEN"
curl -X POST $CANVAS/canvases/1/tasks/ct_1a2b3c/retry  -H "Authorization: Bearer $TOKEN"
curl -X POST $CANVAS/canvases/1/tasks/ct_1a2b3c/cancel -H "Authorization: Bearer $TOKEN"

# 配乐(60 号票):kind=music,prompt 是风格描述(1-2000 字)、model 取
# /music-models 目录;无专属表单,无取消态(网关无取消面,前端不出取消
# 按钮)。产物为 kind=audio 素材行,前端按 asset_id 回填音频节点并记录
# prompt/model 配对(来源判别 source='music')。
curl -X POST $CANVAS/canvases/1/tasks -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"node_id":"n_audio_1","kind":"music","prompt":"轻快的电子爵士","model":"minimax-music"}'
```

### 5.3 同步动作(不进任务队列)

```bash
# 生成提示词(Agent 会话,29 号票:template_id 可选,缺省用内置通用
# 「提示词书写」指令;history 为此前的对话轮次,不含本轮 topic ——
# 服务端最终 messages = 技能渲染的首条指令 + 历史 + 本轮输入;
# 32 号票起 user 轮可带 media[]:素材内容寻址路径或厂商 http(s) 地址,
# data: URI 拒绝(media_inline_unsupported),单条消息 ≤4 图 + ≤1 视频
# (混合允许);assistant 轮带 media = 400;有媒体的轮 content 拼多模态
# 分节(媒体在前文本在后),历史媒体全量重发;内容寻址引用由服务端解出
# 真实地址并校验 kind,素材已删 404 asset_not_found(文案指路开新会话);
# 52 号票起可选 character_ids[](按选择序,≤20 个):服务端校验角色存在
# (404 character_not_found)与 IP 归属(character.ip_id == canvas.ip_id,
# 400 character_ip_mismatch)后,把「【角色设定】」段拼在技能文本之后、
# 仍属首条指令,只带角色级字段;历史固化(52 号票):响应随行带
# character_section = 本轮实际注入的设定段,同一会话的后续轮次把它原样
# 回传(此时不再带 character_ids,服务端逐字复用、不取角色目录 —— 角色
# 后改不影响已发生对话;空 = 首轮按 character_ids 取最新数据渲染);结果
# 写回 Agent 节点文本区并沿连线投递下游媒体节点)
curl -X POST $CANVAS/canvases/1/generate-prompt -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"node_id":"n_agent_1","template_id":1,"topic":"参考这个角色再改一版","model":"gpt-4o",
       "media":[{"ref":"/api/assets/6/content","kind":"image"}],
       "character_ids":[11,12],
       "history":[{"role":"user","content":"智能家居",
                   "media":[{"ref":"/api/assets/5/content","kind":"video"}]},
                  {"role":"assistant","content":"...上一轮提示词..."}]}'
# → {"text":"...","character_section":"【角色设定】\n名字:..."}(未注入时
#   character_section 为空串)

# 生成提示词 · 流式(31 号票,请求体与同步端点完全同形(含 32 号票
# media/history[].media 与 52 号票 character_ids/character_section),
# 响应为 SSE):注入角色时流首帧 data: {"character_section":"…"}(与同步
# 响应同形),其后 data: {"delta":"…"} 增量 → data: [DONE] 收尾;流中途
# 失败发 data: {"error":{code,message}}。校验前置:没过完校验(含媒体与
# 角色校验)不给流,失败仍是普通 JSON 错误形状。响应带 X-Accel-Buffering:
# no,反代无需另配。
curl -N -X POST $CANVAS/canvases/1/generate-prompt/stream -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"node_id":"n_agent_1","template_id":1,"topic":"把色调改暖","model":"gpt-4o"}'
# → data: {"delta":"a ne"}\n\ndata: {"delta":"on city"}\n\ndata: [DONE]\n\n

# 视频反推提示词(video_url 接受厂商 http(s) 地址或素材内容寻址路径;
# 结果落为新 Agent 节点并与视频节点连线)
curl -X POST $CANVAS/canvases/1/reverse-prompt -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"node_id":"n_v1","video_url":"/assets/5/content","model":"qwen-vl-max"}'
# → {"text":"..."}

# 分析(图片/视频的结构化理解;分镜表或四节画面描述)
curl -X POST $CANVAS/canvases/1/analyze -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"node_id":"n_i1","media_url":"/assets/3/content","media_kind":"image",
       "model":"qwen-vl-max"}'
# → {"text":"..."}
```

### 5.4 模型与技能目录(编辑器下拉/浮层用)

```bash
curl $CANVAS/image-models     -H "Authorization: Bearer $TOKEN"   # 次轨(call)模型
curl $CANVAS/video-models     -H "Authorization: Bearer $TOKEN"   # 秒轨(second)模型
curl $CANVAS/audio-models     -H "Authorization: Bearer $TOKEN"   # 字符轨 TTS 模型(57 号票)
curl $CANVAS/music-models     -H "Authorization: Bearer $TOKEN"   # 按次配乐模型(60 号票)
curl $CANVAS/prompt-templates -H "Authorization: Bearer $TOKEN"   # 启用中的技能(带 description/target)
curl $CANVAS/prompt-models    -H "Authorization: Bearer $TOKEN"   # token 轨聊天模型
```

### 5.5 素材库

```bash
# 上传本机文件(≤256MiB;kind 声明由魔数嗅探裁决,不符 400)
curl -X POST $CANVAS/assets/upload -H "Authorization: Bearer $TOKEN" \
  -F kind=image -F file=@./photo.png
# → {"id":12,"kind":"image","canvas_id":0,"model":"","prompt":"",
#    "url":"","object_key":"uploads/20260929/uuid.png",
#    "content_type":"image/png","size_bytes":204800,...}

# 列表 / 删除
curl "$CANVAS/assets?kind=image&limit=50" -H "Authorization: Bearer $TOKEN"
curl -X DELETE $CANVAS/assets/12 -H "Authorization: Bearer $TOKEN"

# 内容寻址(刻意不挂 JWT:<img>/<video> 带不了 Authorization 头)
curl $CANVAS/assets/12/content
curl -OJ "$CANVAS/assets/12/content?download=1"   # 强制下载(attachment)
```

---

## 附:端口对照

| 服务 | compose 端口 | dev 端口 |
|---|---|---|
| gateway(本表 /auth /admin /v1)| 8080 | 8080 |
| canvas(画布面)| 8081 | 8081 |
| admin-web 前端 | 8090 | 5173 |
| canvas-web 前端 | 8091 | 5174 |

桌面版:gateway 与 canvas 同进程装配,中转面仍监听固定本机端口(以应用设置为准)。

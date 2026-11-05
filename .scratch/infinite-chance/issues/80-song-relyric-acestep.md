# 80 同曲换词通路二落——ACE-Step 1.5 自托管 cover 任务(Mureka 通路搁置)

Type: feat
Status: decided(2026-11-05 grill-with-docs 定案;实现另起 implement-spec 流程)

## Question

79 号票已实现的 Mureka Song Edit 通路按次 $0.10/编辑区域,用户裁决成本过高,真 key 验证不跑;Plan B 直接转正:**ACE-Step 1.5 开源自托管**,部署在用户另一台 Windows(RTX 4060 8GB)机器,画布同曲换词创作态改落 acestep 通路。物种语义不变,换的是通路。

## 事实底座(2026-11-05 调研;`[官方]` = 厂商文档;笔记已并入票面,临时调研件不引用)

- **ACE-Step 1.5 `[官方]`**:独立仓库 `ace-step/ACE-Step-1.5`;架构 = 5Hz LM planner(0.6B/1.7B/4B,Qwen3 系)+ DiT 2B(**turbo 蒸馏 8 步** / sft / base)+ XL 4B;HF 主仓 `ACE-Step/Ace-Step1.5` ≈ 10.1GB(planner/DiT 分仓另计);代码 MIT、权重无 NC,个人自用无合规约束。注意仿冒域名警告,只认 github.com/ace-step。
- **cover/remix 任务官方一等支持「参考音频 + 全文新词保旋律」`[官方]`**:`audio_cover_strength` 文档推荐起点 0.5–0.6,turbo 可用,LM 自动跳过;GitHub discussion **#398** 采纳答案 = cover + 全文新词、**#1132** turbo+Remix 实证成功(漏唱行靠音节对齐 + batch 多抽缓解)。**79 号票「全曲换词社区无成熟工作流」的旧记载已过时**(当时仅看到 flow-edit 视角)。
- **服务形态 `[官方]`**:官方自带异步 REST(**acestep-api**,:8001,`/release_task` → `/query_result` → `/v1/audio`,可选 Bearer key,非 OpenAI 兼容)+ Gradio(7860)+ Python pipeline + CLI;基准 A100 <2s、3090 <10s 一曲;8GB 档建议 turbo。
- **flow-edit(edit 任务)**已移植回 1.5 但**只支持 sft/base/xl(turbo 不支持)**且 REST `release_task` 参数未透出(`flow_edit_*` 走 Python GenerationParams)——不进首票。**repaint** = 3–90s 局部、全曲歌词不稳(issue #1116 有改纯伴奏的失败案例)——不选。
- **参考音频 `[官方]`**:自动归一 stereo 48kHz、<30s 循环补足、**无需人工分离前置**;输出默认 mp3、10–600s(LM 档钳 8min);GFW 无忧——官方原生 ModelScope 下载源。
- **Apple Silicon `[官方]`** 一等支持(MLX 后端,macOS 脚本自动)——**本票不用**:已知坑 MLX DiT 忽略 cover_noise_strength(#1346 open)、MLX 内存泄漏(#1097/#1362),仅存档。
- **宿主机事实**:本机 M1 Pro / 16GB / **磁盘仅剩 706MiB**(权重 10.1GB 起,硬阻断)不自托管;用户另一台 **Windows + RTX 4060(8GB VRAM)** 与 Mac **同一局域网**(IP 未固定);gateway(Docker 栈/桌面版)在 Mac。
- **代码底座(78/79 号票已铺)**:`/v1/audio/music` + `audio_ref` + lyrics 契约;prompt 选填豁免(谓词 =「候选渠道全部 mureka」);sentinel 三枚请求级先例(缺词/缺参考/上传链);右键第 10 条 `source:'relyric'` 创作态全链;`/music-models` 目录 = music 能力 ∩ call 配价;worker 重提显式携带三事实。

## 定案

1. **物种与词条**:物种沿用「同曲换词」(旋律锁定换词重唱,验收下限**旋律级**不变);通路从 Mureka Song Edit 转向 **ACE-Step 1.5 cover 任务**(参考音频 + 全文新词 + 旋律保持强度锁旋律)。**79 号票 Mureka 代码保留休眠**(不建渠道 = 零流量零成本;票 79 注记搁置(成本)、留复活权,不删代码不回滚)。
2. **宿主机与部署形态**:Windows 4060 机器**裸机 Python** 部署(uv venv + 官方脚本,不用 WSL2/Docker——GPU 直跑省一层);**turbo 档**(8 步蒸馏,8GB 友好);与 Mac 同局域网直连。部署文档钉:DHCP 保留/静态 IP(IP 漂移 = 渠道失联)、防火墙放行 8001、ModelScope 下载源、一键启动脚本;开机自启留增量。**服务在线性由用户保证**——离线时提交走既有换道/exhausted 同步失败链,网关零特殊处理。
3. **渠道与公开名**:新渠道类型 **`acestep`**(普通 HTTPS,可选 Bearer key 存 `api_key` 列;BaseURL 必填 = `http://<win-ip>:8001`),adaptor 照 adaptor 模式新增文件直连官方 acestep-api REST,**首票不写 wrapper**(真机若发现 `release_task` 缺 cover 参数再议 wrapper 另票);公开名 **`acestep-relyric`**(model_map → 任意非空档名占位——cover 提交体是否收 model 未证实,不透传;公开名即调度单元,同曲换词只落配置了该名的渠道);admin-web 渠道表单加 acestep 类型(music 能力位 + 单 key 字段,照 mureka 形,密钥只写不读尾 4 位);连通测试照假任务套路(GET 必不存在的任务 id,404/400 业务错 = 全链通、401/403 = 凭据失败)。
4. **relyric 语义映射**:同曲换词请求落 acestep 渠道 → **cover 任务**:`src_audio` = 已解析参考地址(URL 直传,上游自拉)、`lyrics` = 全文新词、风格 prompt(选填)→ tags/风格描述参数透传、`audio_cover_strength` = 请求携带值(缺省不传 = 上游缺省);`instrumental:false` 恒定(词是身份必需);**时长不传**(cover 跟随 src_audio,真机验证点);一次提交 = 一 call,计价照 call 轨,**配价录 ¥0**(自托管无边际成本;硬约束 3 仍生效——未配价即 `model_not_priced` 拒,审计流水可见)。
5. **旋律保持强度(cover_strength)**:新领域参数——**越高越贴原曲旋律**(重组自由度越低);画布 relyric 表单滑杆 **0.3–0.9、步进 0.05、缺省 0.55**(文档推荐起点带内,两端防呆:过低 ≈ 没锁旋律、过高 ≈ 重组受限),文案注明语义;网关 `/v1/audio/music` 请求体加**可选 `cover_strength`**(float,0–1 越界 → 400 `invalid_request_error`);usage 快照不强制记录(最小面)。
6. **网关契约**:复用 `/v1/audio/music` + `audio_ref`,不新建端点;acestep adaptor 语义 = cover(区别于 mureka 的 Edit):**sentinel 三枚照 79 同构**——audio_ref 非空而歌词空 → 400(词是身份必需)、audio_ref 空 → 400(文案指路普通音乐公开名)、参数形状非法 → 400 形;均请求级(不换道、不记熔断账、预扣原退)。**上游拉取参考 URL 失败 = 上游异步任务失败** → 走既有 failed 退款链(提交时网关不预检可达性)。**prompt 选填豁免谓词从「全 mureka」扩为类型集合(mureka ∪ acestep)**——79 遗产的关键复用点,canvastask 侧按形状判(audio_ref+lyrics 齐备)不动。
7. **schema**:canvas_tasks 加 `cover_strength` 列(**MySQL + SQLite 双方言**,REAL/FLOAT NULL;NULL = 未携带走上游缺省)——**79 的「零 schema」被打破**(UI 参数必须随行持久化,否则 worker 重提丢参数);worker `runMusic` 重提显式携带 cover_strength(62/78 重提纪律);网关 music_tasks 零迁移(strength 只随请求上送)。SQLite 加列迁移须先于索引建(48 号票教训)。
8. **画布面**:右键第 10 条与 `source:'relyric'` 第四创作态结构零改动;relyric 表单**加滑杆**(定案 5 参数);`AudioNodeData` 配对扩 `{ref_asset_id, prompt, model, lyrics, strength}`(创作事实 + 产物回填同形,78/79 先例);audioSyncPatch 回填补 strength;提交 payload 带 `cover_strength`(表单值恒带,缺省 0.55 也显式送——与 79 的「prompt 非空才送」不同,strength 是数值型没有空串歧义);模型下拉全量 `/music-models` 不过滤照旧(acestep-relyric 配价后自动进目录,「不按模型特判」纪律)。
9. **参考与产物**:URL 直传为主——`src_audio` 收公网可拉地址,**素材存储切 COS 驱动**(23 号票驱动已在:settings 切驱动行 + 桶配置;**新素材落 COS,存量素材不迁**——老素材参考时 URL 是 Mac 侧局域网直链,两机同网可达,content 端点故意不挂鉴权);COS 桶公网可拉形态(公开读直链 or 签名 302 跟随)真机验证;产物 = 上游 mp3 URL(LAN 可达)→ canvas worker `archiveAudio` 照旧转存 `canvases/` 档,**转存链零新逻辑**。
10. **部署欠账**:①Windows 侧按部署文档装机(权重 ≈10.1GB 起 + python 环境);②IP 固定(DHCP 保留/静态);③admin 建 acestep 渠道(BaseURL + music 能力位 + 可选 Bearer);④model_map 配 `acestep-relyric`;⑤call 轨配价 ¥0;⑥**素材存储切 COS**(settings 驱动行 + 桶 + 公网可拉核验)。
11. **验收方法**:色谱对撞实验复用(带同曲阳性对照,**0.3 / 0.55 / 0.9 三档 strength 各一曲**)+ 人耳终审,旋律级下限判级(照 79 定案 11);达标即定 ACE-Step 为正式通路;**不达旋律级 → 分轨 MIDI/SVS 管线另票(Plan C,非本票)**。
12. **文档回写(实现时执行)**:CONTEXT.md 同曲换词词条补实现结论(acestep 通路 + cover_strength)、渠道词条补 acestep 类型、docs/api.md 补 acestep-relyric 用例(audio_ref + lyrics 必填、cover_strength 选填、¥0 配价注记);无 ADR(单渠道 adaptor 接入,可逆;MIT license 无合规负担)。

## 验证点(真机,按序)

1. **cover 任务参数面**:`release_task` 是否收 `src_audio` 远程 URL(还是仅本机路径/文件上传——不支持 URL 则 wrapper 另票)、`audio_cover_strength`/tags/lyrics 字段名与取值范围、cover 任务的人声表达(instrumental 参数有无/恒人声)、是否收 model 参数。
2. **旋律保持度**:色谱对撞三档 strength(CUDA 后端 strength 生效性实证——#1346 是 MLX 特有,CUDA 预期正常)+ 人耳终审,判级见定案 11。
3. **歌词跟随度**:全文新词的漏唱/塌段(#1132 已知边界:音节对齐 + batch 多抽缓解)——验收注记「batch 多抽后人工挑」为可接受工作流。
4. **可达性链**:Windows 拉 COS URL(公开读 or 签名 302 跟随);Mac worker 拉 Windows 产物 URL(防火墙放行核验)。
5. **性能**:4060 turbo 档单曲耗时(基准 3090 <10s,4060 预估 15–60s;网关 30min 计费时限、worker 15min 轮询时限充裕,无需改)。

## Out of Scope

- flow-edit 局部换词模式(仅 sft/base 支持、REST 未透出;确有需求另票评估)。
- repaint 局部重绘(3–90s 局部、全曲歌词不稳)。
- 分轨 MIDI/SVS 管线(UVR 人声分离 + basic-pitch 旋律转 MIDI/f0 + f0 引导 SVS + 混音)——ACE-Step 不达旋律级才另票(Plan C)。
- Mureka 通路复活或删除(79 代码休眠保留)。
- Mac/MLX 本机托管(磁盘 706MiB 硬阻断 + strength 失效坑;存档不立项)。
- 强度参数进 usage 审计快照、开机自启、存量素材迁 COS、选段/局部编辑 UI、batch 多抽自动化(多抽人工挑即可)。
- 78 号票翻唱通路改动;79 号票 mureka 代码改动(休眠不动)。

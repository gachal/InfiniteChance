# 88 模型级能力登记(目录端点按模型真能力过滤,修「渠道有能力 → 名下模型全亮」混列)(2026-11-13 定案)

Type: feat
Status: implemented(2026-11-14 implement-spec 全链落地 feat/88-model-capabilities 并 main `4b75bd1`;双轴评审修复轮收 5 项,make test/lint/build 三绿)

## Question

画布生成对话框的模型下拉混入无该能力的模型:图片下拉出现 `hunyuan-world-panorama`(¥5/张)、`vod-minimax-music-2.0-cover`(¥0.25/张)、`vod-minimax-music-3.0-cover`(¥1/张)乃至 `voice-enrollment`;视频下拉同理混入按秒计费的音效模型。选了只能 404/失败,还污染选型。

**病根(实查坐实,与最初猜测不同)**:画布三个目录端点根本不读渠道能力位,是「计价表按轨道的纯投影」——`/image-models` = 全部 `call` 轨(`internal/canvastask/handler.go:979-996`,注释原文「渠道侧的 capabilities 在网关调度时兜底,这里不重复校验」)、`/video-models` = `second` 轨 + 带视频折算表的 token 轨(`handler.go:950-973`)、`/prompt-models` = token 轨非视频价(`internal/promptgen/handler.go:1176-1194`)。音乐走次轨 call、全景与音色定制也走 call,于是全被当图片模型。渠道能力位(6 值:`chat/images/videos/audio/music/sfx`)只在 `/v1` 调度兜底(`internal/relay/schedule.go:18-29`:能力 ∩ model_map)。**「从渠道能力 ∩ model_map 推导模型能力」救不了本场景**:同一渠道既有生图能力又挂音乐模型时推导照样全亮。

诉求:管理后台增加**模型级能力设置**,让各节点下拉只显示真有该能力的模型。

## 定案(2026-11-13,grill-with-docs 逐题定案,Q1–Q7 全按推荐)

### 数据模型:能力挂全局模型级(Q1a / Q4a)

- `model_prices`(public_model 主键)双方言加列 **`capabilities`**(MySQL `JSON NULL` / SQLite `TEXT NULL`,与渠道 `capabilities` 列同款先例):**价格行即模型注册行**,能力是公开模型自身的客观属性、与挂在哪个渠道无关。渠道能力位**原样保留**、语义零改动,继续当 `/v1` 调度准入粗筛。
- 模型能力枚举 **7 值**:`chat / images / videos / audio / music / sfx / panorama`。前 6 与渠道能力同词;第 7 `panorama` 为模型层独有——全景模型走 `images` 渠道 + `scene_type=3d_panorama` 参数,但只进全景节点下拉、不进普通图片下拉(Q4a,截图诉求)。
- **轨道允许集**(写入校验依据,Q7a):`token → {chat, videos}`、`character → {audio}`、`call → {images, music, panorama}`、`second → {videos, sfx}`。校验规则:**capabilities 非空时必须是该行轨道允许集的子集**,越轨组合 admin PUT 400 拒绝(管理面统一错误形状 `{"error":{"code","message"}}`)。**空合法** = 不出现在任何下拉(Q6a 显式开启原则;Q7 的「非空」语义据此修正为「非空时校验」——空是合法状态,越轨才拒绝)。跨轨道多能力模型(既聊天又生图)在现行「一模型一行价、一行一轨道」计费下本不成立,校验拦下的全是配置手误。
- 不做自动探测:能力由管理员在后台手工配置,连通测试不猜模型能力。

### 目录端点:六端点统一「渠道能力 ∩ 计价轨道 ∩ 模型能力」三重交集(Q3a / Q5a)

- 画布直连三端点(今天纯轨道投影):
  - `GET /image-models`:**缺省档 = images**(call 轨 ∩ images ∩ 渠道能力),新增查询参数 **`?capability=panorama`** 返回全景档(call 轨 ∩ panorama ∩ 渠道能力);未知值 400。参数只此一档——call 轨下图片/全景是仅有的两个兄弟域。
  - `GET /video-models`:second 轨 + 视频折算 token 轨 ∩ **videos** ∩ 渠道能力(音效模型就此出局)。
  - `GET /prompt-models`(promptgen):token 轨非视频价 ∩ **chat** ∩ 渠道能力。
- 网关代理三端点(今天已是渠道 ∩ 轨道):`/v1/audio/models`(audio ∩ character)、`/v1/audio/music-models`(music ∩ call)、`/v1/audio/sfx-models`(sfx ∩ second)各再 ∩ 模型能力,口径统一(`internal/relay/models.go:74-191`)。
- **`/v1` 调度语义零改动**:`eligibleChannels` 仍渠道能力 ∩ model_map,选错模型照样 404 兜底——目录过滤只影响「列不列」,不影响「路由不路由」。
- **不变量(硬约束)**:各端点响应里的 **`prices` 摘要映射维持「该端点轨道投影」、不按能力收窄**——旁路消费者 `enrollmentPrice = imagePrices['voice-enrollment']`(`CanvasEditorView.vue:373`,音色定制预估)等依赖它;只许收窄 `models` 名单,`models` 字段形状不变(38 号票旧消费者不受扰纪律)。
- 渠道交集数据面:canvas server 与 promptgen 与网关同库,构造 channel store 读启用渠道即可(渠道 `enabled=false` 的模型不列——模型没渠道可路由就不该出现,回填后能力为空的模型同理从所有目录消失,这是修复的预期行为不是回归)。

### admin 录入面:价格页每行勾选(Q1a 落点)

- `admin-web/src/views/PricesView.vue`(CRUD UI 已齐,36 号票):新建/编辑表单(按轨道四套)各加「能力」复选框组(7 值,按当前表单轨道只显示允许集内的项),列表加能力列;`ModelPriceInput` 增 `capabilities []string`,PUT 随行。
- 新建价格行能力缺省空(= 暂不列任何下拉,配好才亮)。

### 回填:migrations 增量自动跑一次(Q2a)

- `internal/migrations` 首个 88 号增量(命名照 84 号票先例,如 `migrations/261113_model_prices_capabilities` 成对留底)+ 双方言 `EnsureSchema` 同步加列 + 形状对账两侧绿(82 号票纪律;加列存在性检查按表参数化)。
- 回填逻辑(幂等,只对 `capabilities` 为空/NULL 的价格行生效):对每个价格行,取「model_map 含该公开模型名的**启用渠道**的能力位并集」回填;**特判 `hunyuan-world-panorama` 回填为仅 `["panorama"]`**(单一精确名特判,非启发式;它走 images 渠道,照并集会得 images,违背 Q4a)。不在任何渠道 model_map 的价格行保持空 = 不列任何下拉(Q6a)。
- 回填后预期:`og-image-2.5` → images;`vod-minimax-music-*-cover` → music;`hunyuan-world-panorama` → panorama;`voice-enrollment` → audio(它在 dashscope audio 渠道 model_map 内);qwen 等聊天模型 → chat。行为零变化地「先全量继承、后人工收窄」——你随后只需在价格页逐行核对微调。

### 前端消费面(改动最小化)

- `packages/api`:`listImageModels` 加可选 `capability` 参数(缺省不传 = images 档,既有调用零改动)。
- `PanoramaNode`(`PanoramaNode.vue:37` 今天直收 `imageModels` 全量):改吃 `capability=panorama` 档名单(`CanvasEditorView.vue` 拉取处分流一个 ref);缺省物化 watch(`models.includes(PANO_DEFAULT_MODEL) ? PANO_DEFAULT_MODEL : models[0]`)不变。
- composer 图片/视频下拉、AssetDrawer 设定图/合集、分析/反推 chat 选择器、歌词翻译 `promptModels[0]`:全部无改动,自动吃过滤后名单;各处「不在册回落首项」既有行为兜住模型消失的过渡。

### 顺路 docs 修正

- AGENTS.md「模型价格页(`/admin/prices` 有 API 无 UI,配价走 curl)」注记已过时(PricesView CRUD UI 36 号票已齐)——随本票 docs 提交修正(不带票号)。

## 验证点

- `make test`(go vet + go test 双方言;MySQL 集成 3307 自动 skip 保持绿;migrations 形状对账 MySQL 侧可 skip、SQLite 永不 skip)+ `make lint` + `make build` 全绿。
- 目录点验(dev 或部署后):`/image-models` 不再含 `hunyuan-world-panorama`/`vod-minimax-music-*`/`voice-enrollment`,仍含 `og-image-2.5`;`?capability=panorama` 含且仅含全景模型;`/video-models` 不含音效模型;`/prompt-models` 只含 chat 能力模型;music/sfx/audio 三个下拉存量内容不变;音色定制预估价(enrollmentPrice)不因 voice-enrollment 离开 models 名单而失效。
- admin 点验:价格页勾选越轨组合(如 call 轨勾 chat)被 400;空能力保存成功且该模型从所有下拉消失;勾回后恢复。
- 调度面回归:同一批模型 `/v1` 直连调用行为与过滤前逐字节一致(目录过滤不改变路由/计费)。
- 回填幂等:迁移重复执行 no-op;桌面版 SQLite 装配同增量可跑(桌面自有渠道数据,回填无害)。
- 部署欠账:`make up --build` 重建六服务 + nginx 旧 IP 陷阱留意(`docker compose restart admin-web canvas-web` 即愈)。

## Out of Scope

- `/v1/models` 目录(`internal/relay/models.go:39-66`,model_map 并集、无能力语义)不动。
- 调度/熔断/计费语义零改动;模型能力不参与预扣与校价。
- 跨轨道多能力模型(一个模型既 token 轨又 call 轨):现行一模型一行价下不成立,将来真有多面模型另票(可能需要按端点配价)。
- 模型能力的变更审计(渠道能力位同款无审计,不单开)。
- 自动探测模型能力(上游 /models 拉取推断):不做,人工配置。
- `packages/api` 的 `CHANNEL_TYPES` 清单漂移(5 种 vs 实际 7 种)另票。
- 前端 `durationRangeFor`/`resolutionPresetsFor` 等按模型名的档位表:与能力无关,不动。

## 实现注记(2026-11-14,implement-spec)

- **落地**:feat/88-model-capabilities 8 提交(6 feat 按域 + 2 评审修复)→ `--no-ff` 并 main `4b75bd1`;`make test`(24 Go 包 + MySQL 3307 集成真跑)/`make lint`/`make build` 三绿。28 文件 +1780/−107。
- **回填执行位(实现期最大裁量)**:ADR 0008 下 migrations 文件从不执行,回填挂 pricing `EnsureSchema` **加列分支**(列刚加才跑、绝不重放,价格页人工收窄不被启动覆盖);`migrations/261113_model_prices_capabilities.{,sqlite.}sql` 成对留底。NULL 能力渠道按 07 号票生效集口径贡献 `chat`(同 `channel.HasCapability`),**非 token 轨剥掉该默认项**防越轨回填;panorama 单名特判照票面。
- **评审修复轮 5 项**:F1 SQLite Upsert 守卫改 NULL 安全不等 `IS NOT`(三值逻辑坑:NULL `<>` 值得 NULL,存量行勾/清能力静默不落库,桌面版直受)——三方向回归测试钉死;F2 即上述生效集口径;F3 特判常量改名 `panoramaModelName` + 分支正读;F4 价格页能力复选框中文标签与列表列同源;F5 `rowQuerier` 删未用方法、渠道 SELECT 删未用列。
- **裁量记录**:①`?capability` 只接受空(缺省 images 档)与 `panorama`,其余一律 400(票面「参数只此一档」严格读法);②网关三端点 `prices` 自此为**纯轨道投影**(88 前是渠道∩轨道,「口径统一」读法只增不减,按名取价消费者无感);③既有目录测试夹具(fakePrices、relay 三端点)随「空=不列」新语义补能力位,否则全变空名单。
- **装配**:三入口(canvas/gateway/desktop)`channels.EnsureSchema` 均先于 `prices`(回填读 channels 表的前置);`CanvasStores` 增 `Channels` 只读面,目录内存过滤启用渠道。
- **遗留**:部署 `make up --build` + nginx 旧 IP 陷阱(`docker compose restart admin-web canvas-web`);价格页逐行核对回填结果并人工微调;浏览器点验六下拉与音色预估价。

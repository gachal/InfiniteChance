# 45 号票:网关新增 MiniMax H3 视频渠道(调用契约与 Seedance 2/2.5 兼容)

## 定案要点(未获用户答复,按推荐项执行,票内标注)

1. **范围:网关 + 画布全链**。对外 `/v1/videos/generations` 契约**零变化**——H3 与 Seedance 用同一套调用方法(prompt/seconds/size/ratio/image/last_image/references),差异全部关在 adaptor 翻译层内(这正是现有 adaptor 模式的设计意图)。画布前端只做「分辨率档位/时长区间按模型区分」的适配,不动提交链路。
2. **计价:second 轨**。官方按秒计价(768P $0.08/s、2K $0.13/s,中国站另有人民币价),与网关 second 轨「预扣=按秒×分辨率系数、结算=预扣零差额」天然对齐;配价后画布 `/video-models` 目录自动收录(canvastask/handler.go:754 只认 second 轨全部 + token 轨 HasVideoRates)。
3. **站点:BaseURL 不锁站**。渠道必填 BaseURL(与 volcengine-ark 同款),票内示例默认中国站 `https://api.minimaxi.com/v2`,国际站 `https://api.minimax.io/v2` 同样可配;两站 /v2 路径同构(同一套官方文档)。
4. **size 档位串以官方为准、纯透传**:H3 键 `480P`/`768P`/`2K`(官方大写),Seedance 保持 `480p`/`720p`/`1080p`,adaptor 不做归一(计价 FactorMicros 键与上游枚举天然一致,不造映射魔法)。

## 已实证的 MiniMax H3 契约(官方 platform.minimax.io 文档)

- 创建:`POST {base}/v2/video_generation`,Bearer 鉴权,请求体 `{model, content[], resolution, duration, ratio}`;应答 `{"task_id": "<纯数字串>"}`。
- 查询:`GET {base}/v2/query/video_generation/{task_id}`,`task` 对象含 `status`(queued/running/succeeded/failed/cancelled)、`content.url`、`error{code,message}`(OpenAI 形错误体)、`usage{completion_tokens,…}`。**实现第一步先抓该文档页实证响应是平铺还是 `{task:{…}}` 包裹,再定归一结构体**。
- 取消:`DELETE {base}/v2/video_generation/{task_id}`——queued→cancelled(不收费)、succeeded/failed→deleted、running 报错;与网关「非 2xx 不致命、照常本地取消退预扣」语义兼容。
- content 角色枚举 `first_frame`/`last_frame`/`reference_image`/`reference_video`/`reference_audio` **与网关 VideoRefRole 完全同款**;content 必须含非空 text(网关 prepareVideo 已强制 prompt 非空);duration 必填整数 4–15(网关 seconds 缺省 5,天然满足);纯文生视频 ratio 必填且不能 adaptive(上游 4xx 透出,与 Seedance 编辑型契约同款处理);参考视频 ≤3/参考音频 ≤3(画布限 1,更严,天然兼容)。

## 改动清单

### A. 决策票与文档(先票后码)
1. 新建 `.scratch/infinite-chance/issues/45-minimax-h3-channel.md`:定案要点、字段映射表、配价示例、真 key 验证点(照 25 号票模式)。
2. CONTEXT.md 回写:渠道条目(类型枚举 +1 `minimax`)、新增「MiniMax H3 渠道」条目(单 Bearer key、BaseURL 含 /v2、second 轨计价、公开名 `minimax-h3`→上游 `MiniMax-H3` 走 ModelMap)、任务条目补 MiniMax 归一面。
3. docs/api.md:视频配价示例旁补 H3 second 轨示例(轻量)。

### B. 渠道域(internal/channel)
4. `channel.go`:常量 `TypeMiniMax = "minimax"`(注释:单 Bearer key 存 APIKey 列、BaseURL 必填含版本路径 /v2)并入 `SupportedTypes`。Normalize 无需新分支(通用 http(s) 校验已覆盖)。
5. `tester.go`:`Test` 加分支 + `testMiniMax`(照 testVolcengineArk 套路):GET `{base}/v2/query/video_generation/infinitechance-probe-not-a-task`;404/业务错=连通全通,401/403=凭据失败,其余原样回报。

### C. 网关 adaptor(internal/relay)
6. 新建 `minimax.go`(照 ark.go 结构,内嵌 openAIAdaptor 复用 ErrorSummary/Normalize):
   - `minimaxTranslateSubmit`:prompt→content[0] text 分节;image/last_image→image_url 分节带 first_frame/last_frame;references 按 kind→image_url/video_url/audio_url 分节带 reference_* 角色;seconds→duration、size→resolution、ratio→ratio 全透传。
   - `VideosSubmit`:POST `/v2/video_generation`;2xx 无 task_id → 502 形拒绝(照 Ark 防御);有则原样 relay(应答本就是 `{"task_id":…}` 契约形状)。
   - `VideosQuery`:GET `/v2/query/video_generation/{id}`,归一为内部任务面 `{task_status, video_url, error.message, usage.completion_tokens}`(包裹形状以官方文档实证为准);不可解析→502 形归一(轮询不改状态不动账)。
   - `VideosCancel`:DELETE `/v2/video_generation/{id}`。
   - Chat/Images 四方法:不支持桩(同 Ark,MVP 仅 videos)。
7. `relay.go` `adaptorFor`:加 `minimax` 一行。
8. 新建 `minimax_test.go`(照 ark_test.go):翻译(全字段/空可选字段/参考三 kind)、查询归一(各状态/error/usage/坏 JSON)、submit 无 task_id 防御、cancel 路径。

### D. 管理台(admin-web + packages/api)
9. `packages/api/src/index.ts`:CHANNEL_TYPES 加 `'minimax'` + 注释。
10. `admin-web/src/views/ChannelsView.vue`:类型下拉加「MiniMax(H3)」;isMiniMax 提示「单 Bearer key、当前仅实现生视频(videos)」。

### E. 画布前端(canvas/web)
11. `composer.ts`:
    - `durationRangeFor` 加分支:`minimax-h3`→{4,15}、`minimax-h3-max`→{5,15}(toLowerCase includes 匹配,同时覆盖上游名 MiniMax-H3 形态)。
    - `VIDEO_RESOLUTION_PRESETS` 改造为 `resolutionPresetsFor(model)`:minimax-h3→自动/768P/2K、minimax-h3-max→自动/480P/768P、其余(Seedance)→自动/480p/720p/1080p(保持现值,向后兼容)。
12. `GenerationComposer.vue`:分辨率下拉(L449 附近)改用 `resolutionPresetsFor(props.model)`;模型切换时若当前选中档不在新预设里回落到「自动」。
13. `composer.test.ts`:补 durationRangeFor(H3/H3-Max/两种命名形态)与 resolutionPresetsFor 用例。

### F. 部署侧动作(写进票,不在代码内)
- 建渠道:type=minimax、BaseURL=`https://api.minimaxi.com/v2`、APIKey、ModelMap `minimax-h3→MiniMax-H3`、勾 videos;连通测试点验。
- 配价(second 轨、微人民币):PUT /admin/prices,per_call=每秒基准价,factor_micros 以 `768P`/`2K` 为键(官方串);美元价按中国站人民币价折算。
- `make up --build` 重建后真 key 全链实测(票内验证点)。

## 验证

- `go test ./internal/relay/ ./internal/channel/`(新测试 + 既有回归)。
- `cd canvas/web && npx pnpm@11.28.2 test`(composer 用例)。
- `make lint`(go vet + pnpm lint)。
- 真 key 实测留票:提交/轮询/取消具体错误码、查询响应包裹形状、文生视频不选比例的 4xx 文案(照 25 号票「具体错误码以真 key 实证为准」纪律)。

## 明确不做

- 不动 relay 骨架/video.go/videotask/调度熔断(adaptor 模式零侵入)。
- 不做 480p→480P 之类档位归一映射(官方串即键,透传)。
- 不做 H3-Context-IR(prompt 增强)与 Regeneration(超分)两个辅助端点——adaptor 模型无关,H3-Max 只需配渠道+价格即可用;辅助端点将来另票。
- 不做渠道密钥之外的任何 config 项(单 Bearer key,与 Ark 同款)。
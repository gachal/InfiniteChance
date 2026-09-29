# 20 腾讯云 VOD AIGC 接入(OG image 2.5 Sunburst)

Type: grilling
Status: implemented（2026-09-29 按草案实现:`internal/tc3` TC3 签名助手（黄金向量对拍参考实现,含 canonical headers x-tc-action 小写规范）+ 渠道类型 `tencent-vod` 与通用 `config` JSON 列（MySQL ensureColumn/SQLite PRAGMA 双方言迁移,敏感键只写不读 + 尾 4 位提示,PUT 空值保留已存）+ 类型化连通探测（DescribeTaskDetail 假任务:业务错=通,AuthFailure=败）+ `internal/relay/vod.go` adaptor（generations/edits 翻译、10min+5s 轮询同步封装、FileUrl→data[].url、错误码→换道语义映射）+ `adaptorFor(ch)` 按渠道类型分发 + admin-web 按类型出 config 表单 + packages/api 类型。测试:tc3 黄金向量、adaptor 单测、channel Normalize/掩码/探测、MySQL config 往返、relay 端到端 generations+edits,go test 17 包全绿,vue-tsc/vitest/eslint 全过。Open questions 中 ModelVersion 精确枚举值仍以控制台为准(模型映射串即时可改)）

## Question

腾讯云点播(VOD)的 AIGC 聚合服务怎么接入?它不是 OpenAI 兼容上游(TC3 签名、异步任务、私有参数),怎么让调用方仍以 OpenAI 生图格式访问?首个支持的模型定为哪个?多凭据(SecretId/SecretKey/SubAppId)存哪?异步任务怎么对外呈现?同步等待预算、连通测试、计价轨道怎么定?

## Answer

1. **接入形态:新渠道类型 `tencent-vod` + Adaptor 内翻译,对外零新端点**。实现既有 `relay.Adaptor` 接口;客户端照常调 `POST /v1/images/generations`(JSON)与 `POST /v1/images/edits`(multipart),VOD adaptor 翻译为 `CreateAigcImageTask`(域 `vod.tencentcloudapi.com`,API 版本 `2018-07-17`,X-TC-Action 头),按 01 号票既有结论把异步「提交 → 轮询 `DescribeTaskDetail`」封装成同步响应,产物 `Output.FileInfos[].FileUrl` 转成 OpenAI images 响应体 `{created, data:[{url}]}`。relay 骨架(调度/熔断/换道/按次计费/用量留痕)原样复用;`Handlers.adaptor()` 升级为按 `ch.Type` 分发。MVP 范围:**文生图 generations + 图生图 edits**;聊天/视频方法返回明确不支持(该渠道能力只声明 `images`,调度本也不会把聊天/视频请求落上来)。

2. **首个模型:OG image2.5 Sunburst**(OpenAI GPT-Image-2.5 的 Sunburst 变体,2026-09-08 发布,经 VOD AIGC 聚合)。模型映射复用 ModelMap,上游串 `"OG image2.5_sunburst"` 空格连接——adaptor 按第一个空格拆 `ModelName`/`ModelVersion`,与 VOD 的模型命名(`OG image2_low/medium/high`、`GEM 3.0`、`Jimeng 4.0`)同构。质量档位若以版本后缀表达,则**一个档位一个公开模型**,不翻译 OpenAI `quality` 参数(避免映射×参数二维组合)。

3. **凭据:渠道表新增通用 `config` JSON 列**(双方言建表 + MySQL `ensureColumn` 原地加宽 + SQLite `PRAGMA table_info` 检测加列)。`map[string]string` 平铺;tencent-vod 存 `{secret_id, secret_key, sub_app_id, region}`。**掩码规则:键名含 `secret` 或 `key`(不区分大小写)的值只写不读**——响应回显非敏感键原值 + 敏感键尾 4 位提示;PUT 携带空敏感值 = 保留已存值(handler 读旧行合并,api_key 同款语义)。创建时 tencent-vod 要求 config 含 secret_id+secret_key,api_key(Bearer 字段)对该类型不要求;**BaseURL 对该类型可空**,空 = 缺省 `https://vod.tencentcloudapi.com`(国际站/代理可显式填)。此列为可灵(AccessKey+SecretKey)、fal(Key)等多凭据厂商铺路。

4. **参数翻译**:OpenAI `size`("WxH")→ 解析 `Resolution`(短边 ≥2048 → `2K`,否则 `1K`)+ `AspectRatio`(宽高比就近取 `21:9/16:9/4:3/1:1/3:4/9:16`);缺省/`auto` → `1K` + `adaptive`。`n` → `OutputImageCount`(OG 上限 8,超出 adaptor 侧 400)。`response_format` 忽略,恒返 url(VOD 产物即 FileUrl;画布转存链 url/b64 都吃)。`output_format`(png/jpeg)、`StorageMode`(`Temporary` 缺省——持久化交给 14 号票既有转存链,`Permanent` 留待需要 VOD 侧超分时再评估)MVP 不暴露。edits 的 multipart image 文件 → 参考图 `FileInfos` 仅 `{"Type":"Base64","Base64":…}`(生图 FileInfos 无 Category/Usage 字段),mask 忽略。

5. **同步等待预算:adaptor 内 10 分钟总时限 + 5 秒轮询**,不受全局 buffered 5min 上限约束;超时按临时失败处理(可换道、退款),收尾时尽力取消 VOD 任务(文档未见取消接口则接受泄漏)。运维注意:TC3 签名依赖系统时钟,漂移会 AuthFailure;VOD AIGC 默认 1 并发,泄漏任务会占住并发额度;网关退款不等于腾讯侧免计费。

6. **连通测试:按渠道类型分流**。tencent-vod 的一键测试改为 TC3 签名调 `DescribeTaskDetail`(不存在的 TaskId):HTTP 200 + 业务级错误(任务不存在)= 网络/签名/凭据全通,判成功;`AuthFailure.*`、SubAppId 类错误 = 失败并回显错误码。openai 类型行为不变(GET /models)。

7. **计价:次轨原样复用**(`unit=call`,usd_per_call + size_factors,VOD 生图本就按模型按张平价,size_factors 缺省 ×1.0 即可),预扣 × n、按实交 `data` 张数结算、失败退款全部走 07 号票既有机制。usage log `unit=call`、request{size,n} 快照照旧。

8. **Tencent 业务错误映射**(HTTP 恒 200,错误在 `Response.Error`):`AuthFailure.*` → 401(不换道);`InvalidParameter.*`/`MissingParameter`/参数超限 → 400(不换道);`LimitExceeded`/并发限制 → 429(临时,换道);`InternalError`/`FailedOperation.*`/未知 → 502(临时,换道)。轮询的暂态失败(网络/5xx)连续容忍若干次后按临时失败收尾,超时同上。

9. **产出面**:admin-web 渠道表单类型下拉加「腾讯云 VOD AIGC」,按类型出 `secret_id/secret_key/sub_app_id/region` 字段(敏感键留空保留);packages/api 类型同步;桌面 SQLite 方言建表/加列同落地。VOD AIGC 的生视频(CreateAigcVideoTask)/音乐(MPS 域)不在本票,与 DashScope adaptor 同列待补。

## Open questions(实现期查证)

- `CreateAigcImageTask` 对 OG image2.5 的 `ModelVersion` 精确枚举值(以控制台/接入指南为准;模型映射串随文档即时可改,adaptor 不硬编码)。
- VOD AIGC 是否提供任务取消接口(超时收尾的尽力取消)。
- `StorageMode=Temporary` 的 FileUrl 有效时长(转存链已覆盖,仅文档化)。

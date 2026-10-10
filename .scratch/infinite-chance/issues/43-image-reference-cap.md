# 43 图生图参考图上限放宽:4→9

Type: follow-up
Status: implemented(2026-10-11 实现;42 号票同族后续,用户直接指令「检查当前图片节点,看为什么参考图只能放 4 张,开放最大值」,单点无歧义免 grilling)

## Question

图片节点(生成对话框图片模式/图生图)的参考图上限 4 张是 21 号票的自限保守值,比上游真实能力窄。42 号票放宽视频参考图时显式留了「图生图上限不动」;本票把它也放开。

## Answer

**`maxImageRefs` 4→9,前后端同值**。9 的依据:

- **VOD adaptor 的厂商侧参考图配额就是 9**(`internal/relay vodMaxImageRefs = 9`,generations 的 URL 型参考已按 9 校验);edits 路径每个文件 part 转 Base64 FileInfo,循环无数量假设。
- **openai 型渠道无数量自限**:网关 `rebuildMultipart` 逐字节复刻全部 `image[]` 分节,gpt-image 官方口径(16)更宽;超限由上游 4xx 透出(既有原则)。
- **与视频参考图 9 同值**,用户心智一致;但两常量独立演进(42 号票解耦先例),同值是巧合不是耦合。
- **字节面守卫与数量无关,不动**:worker 单张 8MiB / 合计 24MiB、网关 32MiB 请求体上限照旧拦截胖参考,9 张小图与 4 张大图在字节面一视同仁。

## 显式 out of scope

按模型区分上限(公开模型一档一名不做 per-model cap)、edits 直带素材行/对象键避免公网自取(code review 遗留,见 CONTEXT.md 待办)、Agent 消息附件 4 张上限(32 号票,另一码事)。

## 产出面

- `internal/canvastask` `handler.go`:`maxImageRefs = 9` + 注释重写(数量守卫与字节守卫的关系);`maxVideoRefImages` 注释同步(两常量今日同值)。
- `internal/canvastask` `client.go`:`maxEditRefsTotalBytes` 注释改数量无关表述。
- `canvas/web` `composer.ts`:`MAX_COMPOSER_REFS = 9`;`CanvasEditorView.vue` 注释同步。
- `packages/api` `index.ts`:`CreateCanvasTaskInput.image_urls` 注释 ≤4→≤9;顺带修 42 号票漏改的 `CanvasVideoRef` 注释(参考图 4→9)。
- 测试:`handler_test.go` 超限用例 5 条→10 条(「超过九条」400);`composer.test.ts` 上限用例加 `MAX_COMPOSER_REFS` 硬编码断言 9(防静默漂移)。
- **网关零改动**(本就无数量限制);CONTEXT.md 回写(画布任务 + 生成对话框条目,后者含 42 号票漏改的「参考图 ≤4」)。

## 验证点

第九张参考图可提交;第十张前端拒收(上传按钮满员禁用)且服务端 400「image_urls 最多 9 条」;合计字节超 24MiB 仍被 worker 可读拒绝(与数量正交);`make test` + `make lint` + 前端构建。

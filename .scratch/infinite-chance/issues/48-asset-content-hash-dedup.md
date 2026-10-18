# 48 素材内容哈希去重:相同字节只留一份对象

Type: feat
Status: implemented(2026-10-18 实现;口径注记:①票面「调用方零改动」按字面不可达——素材行由调用方构造,worker/persist 各加一行 ContentHash 字段传递,哈希计算与决策仍全收在 asset 包内;②删除防护计数失败按防护处理整单 500 拒删,不冒删共享字节的险;③回填改指成功后对象删除失败留孤儿、记日志不重试——重跑时该行已指保留键不会再触发删除,孤儿回收靠日志人工处理)

## Question

COS(及本地卷/OSS)上的图片/视频存在重复字节:同一文件重复上传、把画布产物或直连转存图下载后再上传等。期望:相同文件在存储里只保留一份对象字节。现状障碍:

- assets 表无哈希列、无唯一索引;三条写入路径(uploads 用户上传 / canvases 任务产物 / relay 直连转存)均未算过哈希;
- 删除是「按行直删对象」,全仓库无引用计数——一旦多行指向同一对象,删任一行会物理删共享字节,去重必须配删除防护。

「哈希认证」定案口径:SHA-256 内容指纹用于判同去重,不是下载完整性校验。

## Answer

**schema**:assets 双方言加列 `content_hash VARCHAR(64)`(空 = 未算,历史行态);`object_key` 加普通索引(删除防护计数用;utf8mb4 VARCHAR(512) 全列 2048 字节,低于 InnoDB 3072 上限,可全列)。**不加唯一索引**——多行同哈希指向同一对象正是去重的合法结果。

**三条写入路径全算哈希落列**(音频一并,不限图片视频):upload 嗅探后「先算后写」——先流式算哈希再决策,命中复用即零字节上云(multipart 句柄可回卷);Transfer / TransferRelay 字节全量在手,写前算。哈希计算收在 asset 包内部,调用方(relay persist、canvastask worker)零改动。任务重试覆盖同键时哈希随新行重算,天然一致。

**字节复用仅 uploads 上传路径**:算哈希后按 content_hash 查最老已有行(ORDER BY id LIMIT 1),命中即新行指向该行 object_key、不再写对象;照常落行不拒收(两次上传两条素材记录、字节一份)。**全前缀口径**(grilling 定案 b):复用查找不限 uploads/——uploads 行可指向 canvases/ 或 relay/ 的对象(「下载再上传」高频场景),规则一句话:同哈希即复用,保留最老对象。canvases / relay 写入路径只落哈希不复用(产物与厂商转存字节唯一性极高,复用收益近零)。

**跨前缀指向的语义代价**:对象键前缀不再必然等于行的来源语义。AGENTS.md 硬性约束 7 增补条款:「内容去重的行级指向允许跨前缀,删除防护按对象键计数」;CONTEXT.md「素材」词条同步。

**删除防护(语义升级)**:`Handlers.Delete`(用户删素材)删行前按 object_key 计数兄弟行(`WHERE object_key = ? AND id <> ?`),>0 只删行;=0 才走既有「先删对象、成功后删行」。上传/转存的失败回收路径删的是本次调用刚写的独占键,保持直删不改。已知窄缝竞态接受:两条同哈希并发上传在查重后各自写对象落行(彼此不可见),下次上传或存量回填自然收敛,不加锁。「素材删除后节点显示不可用」契约随之更精细:共享对象下删其中一行,其余行预览不坏;删光才不可用。

**存量回填与去重**(本票内做,手动触发):网关管理端新增(挂 JWT,与 `/admin/settings/storage` 同域;gateway 已装配 asset store + Dynamic):
- `POST /admin/assets/hash-backfill`:启动后台任务,幂等(在跑则 no-op 回当前进度);`GET /admin/assets/hash-backfill`:进度(已扫行 / 已算哈希 / 已改指行 / 已删对象 / 是否在跑)。
- 任务:分批取无哈希行 → 按 object_key 经 Dynamic 读对象(云优先,ErrNotExist 回退本地;无 object_key 的历史行——仅厂商地址或 data: URI——跳过)→ SHA-256 落列 → 按 content_hash 分组,组内保留最老行(最小 id)的对象,其余行 UPDATE object_key 改指 → 删冗余对象(Dynamic 双端幂等)。**行不合并**(canvas/task/prompt 出处语义各自保留)。
- 断点续跑:已有哈希的行跳过,重跑安全;无独立取消(重跑安全即兜底)。
- admin-web `StorageView.vue`(网关管理区存储设置页)加「哈希回填与去重」按钮 + 进度轮询。
- 已知代价:全量读一次 COS 字节的流量成本;分批推进,批间不加刻意 sleep(单管理员库小)。

**桌面版**:SQLite 方言同列同步(硬性约束 11);回填端点同样可用(本地卷对象)。

**导出可见性**:哈希不出管理 API/素材列表(纯内部指纹),`AssetRecord` 不加字段。

## 产出面

- `internal/asset`:`upload.go` 算哈希 + 复用分支;`transfer.go` 哈希落列;store 双方言加列 + content_hash / object_key 索引 + FindOldestByHash / CountOthersOnObjectKey;`handler.go` Delete 防护。
- `internal/relay/persist.go` / `internal/canvastask`:调用链零改动(哈希收在 asset 包内)。
- gateway admin:hash-backfill 触发 + 进度端点(wiring / gateway/server 挂路由)。
- `admin-web/StorageView.vue`:按钮 + 进度;`packages/api`:两个请求函数。
- `AGENTS.md` 硬性约束 7 增补;`CONTEXT.md`「素材」词条。
- 测试:同文件两次上传(一对象两行、互删安全)、跨前缀复用、删除防护两端、回填合并与幂等重跑、无 object_key 行跳过、双方言 schema。

## 部署侧动作(代码外)

实现合入后,管理台存储设置页手动触发一次回填(观测 COS 读流量与合并量);此后无需周期跑(增量路径已覆盖新上传)。

## 验证点

- 同一文件两次上传:素材库两条记录、存储一个对象、两行 content endpoint 均可读;删其一另一条预览不坏;删光后对象字节消失。
- 下载画布产物再上传:增量上传即命中 canvases 对象复用(全前缀口径),零字节上云。
- 回填:构造历史重复(两行同字节不同键)→ 触发 → 哈希落列、其余行改指最老对象、冗余对象删除;重跑幂等无动作。
- 仅厂商地址 / data: URI 的历史行跳过不报错。
- MySQL 与 SQLite 双方言 `go test`;`make lint`;桌面版冒烟。

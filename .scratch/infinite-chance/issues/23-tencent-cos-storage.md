# 23 腾讯云 COS 存储驱动与直连生图转存

Type: grilling
Status: implemented（2026-09-29 按草案实现:`internal/objectstore/cos.go`(cos-go-sdk-v5 驱动,404→fs.ErrNotExist)+ `dynamic.go` 按 driver 分发工厂与 `Cloud()` 同快照接口;`internal/settings` cos 块校验/合并 + relay_persist 前置校验 + `ActivePublicBase` 活跃块;`internal/asset/transfer.go` 抽出 `fetchArtifact` 并新增 `TransferRelay`(键 relay/{yyyymmdd}/{uuid}.{ext});`internal/relay/persist.go` ImagePersist 转存回写(尽力而为,b64 不动,行落库失败回收字节),images 成功路径在结算后接入;gateway/server main 与 desktop 装配注入 asset store + Dynamic;admin-web StorageView 三选驱动 + COS 表单 + relay_persist 勾选;packages/api 类型扩展。测试:go 17 包全绿(新增 cos 驱动 httptest 往返含 CRC64 校验头、cos 路由、Cloud 快照、settings cos/relay_persist 五例、TransferRelay 两例、persist 三例;PublicBaseURLProvider 适配活跃块语义),api 包 48 测试过,admin-web build(vue-tsc)过。部署提示:网关镜像需重建才带上 relay 转存与 COS 驱动）

## Question

存储设置(`settings` 行 `storage`)新增腾讯云 COS 支持的驱动路线(原生 SDK vs 通用 S3 兼容——若通用,OSS 是否一并切)?COS 连接块的字段形状?Dynamic 路由与 `public_base_url` 如何泛化到多驱动?另外确认:对应接口返回的地址(如腾讯 image 2.5 / VOD AIGC 的 `FileUrl`)能否存进存储设置的桶——直连中转面(`/v1/images/*`)返回的临时 URL 要不要也转存落桶,产物如何可再调用、完整 URL 如何记录?

## Answer

0. **先核实的事实(非决策)**:画布生成链路**已经**会把厂商产物地址转存进配置的桶——`canvastask/worker.go` 任务成功时调 `asset.Transfer` 下载 http(s) 产物 URL 写入 `objectstore.Dynamic`,后者按 settings 的 `driver` 路由(local/oss,本票后含 cos);VOD `FileUrl` 是 http(s) 临时直链,服务端可匿名下载,转存无障碍。**COS 驱动上线后,画布侧腾讯 image 2.5 产物与 `/api/assets/upload` 上传自动落 COS 桶,零额外改动。**直连中转面(`/v1/images/*`)现状不落桶(07 号票透传契约,20 号票「持久化交由既有素材转存链」)——是否扩展见第 5 条。

1. **驱动路线:原生 SDK × N(维持 19 号票哲学,再次否决通用 S3 兼容层)**。评估结论记录:本仓 `Store` 接口只有 Put/Open/Delete 三个核心操作(恰为各家 S3 兼容度最高的子集),通用 S3 路线(minio-go 一套配置打 OSS/COS/MinIO/R2/OBS/TOS…)与原生路线技术上都可行;通用胜在常数成本(新平台零代码),原生胜在一等公民错误语义与厂商专有能力余量。定案走原生:COS 用腾讯官方 `tencentyun/cos-go-sdk-v5`,OSS 维持 `aliyun-oss-go-sdk` 不动,未来平台各配原生驱动文件(14 号票接缝本就为此设计);通用 S3 兼容层在 23 号票复确认不取,若未来某平台无原生 SDK(MinIO/R2 类)再按需评估。不立 ADR:键布局与接口不变,反悔成本低,同 19 号票判据。

2. **COS 连接块:五字段,与 `oss` 块同构**。`storage` 行扩为:
   ```json
   {"driver":"local|oss|cos",
    "oss":{…19 号票原样…},
    "cos":{"endpoint":"https://cos.ap-guangzhou.myqcloud.com",
           "bucket":"mybucket-1250000000",
           "public_base_url":"https://…",
           "secret_id":"…","secret_key":"…"}}
   ```
   `endpoint` 填 COS 地域域名,`bucket` 填带 APPID 后缀的完整桶名(COS 命名规则),驱动内把 bucket 插入 endpoint 主机名拼出 SDK 要的 bucket URL(`https://<bucket>.cos.<region>.myqcloud.com`)。密钥字段名用腾讯控制台词汇 `secret_id`/`secret_key`(与 20 号票 VOD 渠道 config 列键名先例对齐);**只写不读**(响应仅 `has_secret_id`/`has_secret_key` + 尾 4 位)、**半套拒绝、成对空值保留原密**、`driver=local` 时 cos 块惰性保留(切回本地不清连接)——全部照抄 oss 块语义。

3. **Dynamic 泛化:按 driver 名分发的工厂**。`dynamic.go` 现有 `newOSS` 硬编码改为按 `driver` 分发(local 缺省/oss/cos),每驱动一份配置指纹缓存(避免每请求重建连接)。**路由语义不变**:写(转存、上传)按 driver 落位,cos 时新对象只写 COS;读(`content` 路由)云侧 `fs.ErrNotExist` 未命中回退本地卷(单向迁移,历史对象透明);删除两头都试(Delete 幂等契约);settings 读失败/驱动构造失败(endpoint 畸形等)降级 local 并记日志。COS 驱动的 404 形态(`cos.ErrorResponse`)映射 `fs.ErrNotExist`,读回退信号不断。

4. **`public_base_url` 归属:每驱动块各自携带,读活跃驱动的块**。`settings.PublicBaseURL()` 从只读 `cfg.OSS` 改为按 `EffectiveDriver()` 读对应块;driver=local 或块空 → 空串 → 18 号票解析链回落厂商原址,语义不变。切驱动 = 切公网地址基座,两套连接互不干扰。

5. **直连生图产物转存(新增 `relay_persist` 开关)**:诉求 = 产物后续可再调用 + 完整 URL 有据可查(临时 URL 约 24h 过期,直连调用方聊天记录里的图会裂)。
   - **开关与前提**:storage 行新增 `relay_persist` 布尔(admin-web「存储设置」页勾选「直连生图产物转存」),**开启时强制 `public_base_url` 已配置**(半配置拒绝,密钥同款严格)。不做隐式联动——`public_base_url` 已服务 18 号票 LLM 可达地址与 21 号票参考图自取,不再隐式绑定第二个语义。
   - **生效面**:`/v1/images/generations` 与 `/v1/images/edits`,渠道无关(VOD 恒返 url 天然全覆盖;openai 兼容渠道 url 形态同样落)。
   - **回写**:每张产物落桶(键 `relay/{yyyymmdd UTC}/{uuid}.{ext}`,与 uploads 同款日期归档语义区分)后,响应 `data[].url` 改写为 `public_base_url + object_key` 的完整永久地址——调用方拿到的 URL 不过期,可直接再调用(喂回 `image[]` 参考图、二次请求、收藏)。
   - **完整 URL 记录**:每张产物落一条 asset 行(kind=image、canvas_id/task_id 空、**`url` 列 = 回写出去的完整地址**、object_key/content_type/size 三件套照填),管理端素材页可查可删(删行删对象)可审计;厂商临时原址仅服务端日志留痕,不入库(24h 后即死链,无记录价值)。将来 21 号票遗留「素材库挑选当参考图」落地时,直连产物在画布同样可复用。
   - **边界**:`b64_json` 形态不动(不改写 `response_format` 语义);落桶尽力而为——失败记日志、响应回退透传厂商临时 URL,请求不失败、计费照旧按实交 `data` 张数结算,与转存成败解耦;**本票仅生图,视频直连任务产物转存另立票**。
   - 代价如实:每次直连生图多一次下载+上传的同步延迟(MB 级、秒级);桶存储量随直连流量线性涨(asset 行即清理面)。

6. **管理页**:驱动下拉三选 local/oss/cos;COS 表单与 OSS 同构,字段标签用腾讯词汇(SecretId/SecretKey);`relay_persist` 勾选项随 public_base_url 联动校验。

7. **连通测试不做**(与 OSS 同款:连通性由驱动使用时暴露,19 号票口径;需要时另立票)。

8. **桌面版零改动**(SQLite 方言同表,行缺省 local,Wails 装配同 Dynamic)。

9. **待补挂账(两云通用,19 号票遗留延续)**:备份脚本对云侧导出策略、存量本地对象批量迁移脚本、签名下载 URL;新增:视频直连任务产物转存。

## 产出面

`internal/objectstore/cos.go`(新驱动 + 404 映射)+ `dynamic.go`(工厂泛化)+ `internal/settings/{settings,handler}.go`(cos 块校验/合并、`relay_persist`、`PublicBaseURL` 活跃块)+ `internal/relay/images.go`(转存与回写,尽力而为)+ `internal/asset`(relay 键与行落库路径)+ admin-web `StorageView.vue`(三选驱动 + COS 表单 + 转存开关)+ 驱动/路由/relay 测试 + CONTEXT.md「设置」词条扩展(driver 集合 local|oss|cos、relay_persist)。

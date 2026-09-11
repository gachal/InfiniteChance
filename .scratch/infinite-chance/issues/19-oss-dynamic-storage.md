# 19 OSS 对接与动态配置

Type: grilling
Status: proposed（草案——grilling 会话用户未到场,按推荐解落稿,待确认后转定案）

## Question

对象存储驱动选型(阿里云 OSS 原生 SDK vs S3 兼容)?"动态配置"落在哪(settings 表归属、管理面、生效时机、密钥安全)?既有本地卷数据怎么办?公网可达地址怎么给?备份语义如何变?

## Answer

1. **驱动选型:阿里云原生 SDK(`aliyun-oss-go-sdk`)实现 `objectstore.Store`**(`Put/Open/Delete` 签名不变,14 号票接缝)。备选 S3 兼容(aws-sdk-go-v2 打 OSS S3 端点)被否:OSS 的 S3 兼容层在 multipart、ACL、错误码等细节有已知坑,原生 SDK 是一等公民;接口足够窄,将来切 MinIO/其他云 = 新增一个驱动文件 + 配置切换,不触达调用点。不立 ADR:object_key 语义与键布局不变,反悔成本低(换驱动 + 拷数据),不满足「难以逆转」判据。

2. **动态配置:新 `settings` 表**(KV:name 主键 VARCHAR(64),value JSON TEXT,updated_at;双方言建表,照 ADR-0001)。`storage` 一行承载:
   ```json
   {"driver":"local|oss",
    "oss":{"endpoint":"oss-cn-hangzhou.aliyuncs.com","bucket":"…",
           "public_base_url":"https://bucket.oss-cn-hangzhou.aliyuncs.com",
           "access_key":"…","secret_key":"…"}}
   ```
   归属照 `prompt_templates` 先例:**网关挂管理 CRUD、canvas/server 同库只读**——`GET/PUT /admin/settings/storage`(JWT),admin-web 网关管理区新增「存储设置」页。**AK/SK 只写不读**(渠道密钥同款:响应仅 `has_secret` + 尾 4 位;PUT 携带空 secret = 保留原值)。生效时机:**canvas/server 按请求读表、即时生效**(模板同款;调用量小,不加缓存,后续需要再加 relay/cache.go 同款短 TTL)。

3. **驱动工厂与读写语义**:`objectstore` 新增按 settings 构造驱动的工厂(wiring 侧注入 settings 读取器)。**写**(任务产物转存、18 号票上传)按 `driver` 落位;**读**(`/api/assets/{id}/content`)OSS `Open` 未命中 → 回退本地卷——单向迁移语义:启用 OSS 后新对象落 OSS,历史对象留本地,读取对调用方透明;**删除** OSS 与本地都试(Delete 幂等 no-op 已是接口契约)。`driver=local` 时行为与现状逐字节一致。

4. **公网可达地址**:MVP 要求 bucket 公共读(自用工具,不做签名 URL,与 CONTEXT.md「待补」既有口径一致);`public_base_url` 进 settings,17 号票分析、13 号票反推、12 号票图生视频参考图共用 18 号票第 4 条的解析顺序。公网地址是配置出来的而非探测出来的:未配 `public_base_url` 时解析链直接回落厂商原址,行为与现状相同。

5. **存量数据:MVP 不做批量迁移**(读回退已覆盖全部历史行);需要时补迁移脚本(列举本地卷 → Put OSS → 校验大小 → 删本地,可断点续跑),列入待补。

6. **桌面版**:SQLite 方言的 settings 建表随 ADR-0001 落地;桌面装配无 OSS 诉求,settings 行缺省即 `local`,零配置不受影响。

7. **备份语义变化**:`deploy/backup.sh` 的素材卷 tar 只覆盖本地卷;OSS 启用后对象在云侧(依赖 OSS 自身冗余),备份脚本对 OSS 的导出策略列入待补——CONTEXT.md「备份与恢复」已预告「素材卷 tar 需随驱动切换换方式」,本票兑现为待补条目而非 MVP 阻塞。

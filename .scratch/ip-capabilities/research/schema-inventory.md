# 全库 Schema 全景与 EnsureSchema 装配调查(01 号研究票)

日期:2026-11-11。调查方式:纯只读 grep/read 主工作区分支 `research/schema-inventory` 的 worktree;全部结论以仓库源码为一手来源,标注 `文件:行号`。行号对应本分支 HEAD(1c9169d)。

**摘要**:全库共 **22 张表,分属 16 个 store 包**(canvas 包拆 canvas/IPStore 两族,故 EnsureSchema 实现族为 17 个;MySQL + SQLite 双方言共 34 个 EnsureSchema 方法)。schema 的唯一权威出处是各 store 内嵌的 DDL 字符串常量,`internal/wiring` 明确不建 schema(internal/wiring/wiring.go:5-6);没有任何一处文件能看到全量结构。最重要发现三条:①除 `CREATE TABLE IF NOT EXISTS` 外,仓库还有**两处 MySQL 形状迁移**(api_keys.expires_at 改 DATETIME、admin_accounts 去 AUTO_INCREMENT)与**一处启动数据自愈迁移**(ips 默认 IP 收编),文件化 migrations 必须给这三类留位置;②SQLite 侧有 **6 列只活在建表 DDL、没有迁移条目**(assets 4 列、canvas_tasks 2 列),因为桌面库诞生时这些列已在——做基线剥离时迁移清单不能从 MySQL 侧机械照搬;③**全库没有一条 SQL FOREIGN KEY 约束**(grep `FOREIGN KEY|REFERENCES` 零命中),SQLite 的 `foreign_keys(ON)` pragma 实际不约束任何表,引用完整性全靠应用层。

---

## 一、全表清单(22 张 / 16 包)

通用约定:下表「时间对」指 `created_at` + `updated_at`;MySQL 侧时间列一律 `TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)`,updated_at 额外 `ON UPDATE CURRENT_TIMESTAMP(6)`;SQLite 侧一律 `TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))`(毫秒精度,仅作 DDL 兜底——store 全部写入显式给 `sqlitedb.FormatTime` 的定宽纳秒值,见 internal/sqlitedb/sqlitedb.go:18-31,以及 canvas/store_sqlite.go:25-27 的统一注释)。自增主键 MySQL 形如 `BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY`,SQLite 形如 `INTEGER PRIMARY KEY AUTOINCREMENT`。下文仅记录与约定不同的点。

### 网关域(与画布共享的表在两服务入口都 EnsureSchema)

| 表 | 包 | 关键列(差异点) | 索引 | 逻辑外键(应用层,无 SQL 约束) |
| --- | --- | --- | --- | --- |
| `admin_accounts` | auth | **id 无自增、固定为 1**(并发 init 输家撞唯一键,store_mysql.go:19-20 注释);username VARCHAR(64) 唯一;password_hash VARCHAR(255) | `uniq_admin_username(username)` | 无(单行表) |
| `channels` | channel | api_key **TEXT 明文**(需重放签名);model_map JSON NOT NULL;capabilities JSON NULL(07 号票);config JSON NULL(20 号票);priority/weight INT 默认 0;enabled TINYINT(1) 默认 1 | 无二级索引 | 被 usage_logs / video_tasks / music_tasks / voices 的 channel_id 逻辑引用 |
| `api_keys` | apikey | key_hash CHAR(64) 唯一;quota_micros BIGINT 默认 0(微人民币);**expires_at 为 DATETIME(6)**(早期 TIMESTAMP(6) 有 2038 上限,`migrateExpiryToDateTime` 启动迁移,store_mysql.go:32-50);revoked_at 可空 | `uniq_api_key_hash(key_hash)` | 被 usage_logs / video_tasks / music_tasks / api_key_quota_log 的 key_id 逻辑引用 |
| `api_key_quota_log` | apikey | key_id、delta_micros、balance_micros、reason VARCHAR(64) | `idx_api_key_quota_log_key(key_id,id)`(MySQL 表内 KEY,SQLite 独立 CREATE INDEX,apikey/store_sqlite.go:56-57) | key_id → api_keys.id |
| `model_prices` | pricing | **public_model 自然主键**(非自增);unit VARCHAR(16);config JSON NOT NULL——轨道 payload 全在 JSON,「Go 结构即 schema」,07 号票加轨道零迁移(pricing/store_mysql.go:10-12) | 无 | 无 |
| `usage_logs` | usage | key_id、channel_id、channel_name、public_model、upstream_model、unit、prompt/completion_tokens、duration_ms、status、charge_micros;price_snapshot JSON NULL;upstream_error TEXT NULL;**source VARCHAR(255) NULL 为 10 号票加列迁移**(store_mysql.go:42-62) | `idx_usage_logs_key(key_id,id)`、`idx_usage_logs_channel(channel_id,id)`、`idx_usage_logs_model(public_model,id)`、`idx_usage_logs_created(created_at)` | key_id → api_keys.id;channel_id → channels.id |
| `video_tasks` | videotask | **id 为应用生成字符串主键**(VARCHAR(64),非自增);upstream_task_id VARCHAR(255);size、seconds;reserved/charge_micros;price_snapshot JSON | `idx_video_tasks_key(key_id,id)`、`idx_video_tasks_status(status,created_at)` | key_id → api_keys.id;channel_id → channels.id |
| `music_tasks` | musictask | 与 video_tasks 同构,video_url 换 audio_url(musictask/store_mysql.go:15-36);**空起步无迁移** | `idx_music_tasks_key`、`idx_music_tasks_status`(同上两形态) | 同 video_tasks |
| `prompt_templates` | prompttemplate | name VARCHAR(128);template MEDIUMTEXT;**description / target 为 29 号票加列迁移**(store_mysql.go:29-35);enabled TINYINT(1) 默认 1 | 无 | 无(网关与画布共享读写,两服务都 EnsureSchema) |
| `settings` | settings | **name 自然主键**(VARCHAR(64));value JSON NOT NULL(存储驱动行在里,19 号票);updated_at ON UPDATE;upsert 语义两侧不同:MySQL `ON DUPLICATE KEY UPDATE`,SQLite `ON CONFLICT(name) DO UPDATE`(settings/store_sqlite.go:69-72) | 无 | 无 |

### 画布域

| 表 | 包 | 关键列(差异点) | 索引 | 逻辑外键 |
| --- | --- | --- | --- | --- |
| `canvases` | canvas | graph JSON NOT NULL(画布图);version BIGINT UNSIGNED 默认 1;**ip_id 默认 0、type 默认 'animation' 为 50 号票加列迁移**(canvas/store_mysql.go:80-83);存量行由列默认值收编 | 无二级索引 | ip_id → ips.id(0 = 未归属,自愈迁移对象) |
| `ips` | canvas(canvas store 与 IPStore **双保险都建**,store_mysql.go:50-53 注释) | name VARCHAR(255);description VARCHAR(1024) 默认 '';cover_asset_id 默认 0;**style TEXT NULL 为 69 号票加列迁移**(双方言,store_mysql.go:46-48) | 无 | cover_asset_id → assets.id;被 canvases/canvas_tasks/assets/characters/props/scenes 的 ip_id 逻辑引用 |
| `assets` | asset | kind VARCHAR(16);canvas_id(0 = uploads/relay 无画布);task_id 默认 '';url **MEDIUMTEXT**(厂商回 b64 时 data: URI 落库,几 MB,store_mysql.go:16-17);**object_key/content_type/size_bytes 为 14 号票迁移、last_used_at 为 47 号票迁移、content_hash VARCHAR(64) 为 48 号票迁移、ip_id 为 50 号票迁移**(store_mysql.go:53-60);48 号票两条新索引均为**普通索引不加唯一**(多行同哈希指向同一对象是去重的合法结果,48 号票票面;asset/store_mysql.go:62-71 索引迁移用 information_schema.STATISTICS 探测) | `idx_assets_canvas(canvas_id,id)`、`idx_assets_task(task_id)`、`idx_assets_kind(kind,id)`、`idx_assets_last_used(last_used_at,id)`、`idx_assets_object_key(object_key)`、`idx_assets_hash(content_hash)`、`idx_assets_ip(ip_id,id)` | canvas_id → canvases.id(弱);task_id → canvas_tasks.id(弱);ip_id → ips.id;被 character_images/prop_images/scene_images.asset_id、voices.sample/preview_asset_id、ips.cover_asset_id 逻辑引用 |
| `canvas_tasks` | canvastask | **id 应用生成字符串主键**;canvas_id(0 = 70 号票无画布任务);node_id VARCHAR(128);ip_id 默认 0(70 号票迁移);kind VARCHAR(16);**MySQL 侧 16 项迁移链**:seconds/image_ref/image_refs/video_refs(12 号票)、ratio/background(37)、scene_type(81)、video_url/remote_task_id(12)、voice_spec/lyrics/instrumental(62)、ip_id(70)、audio_ref(78)、cover_strength FLOAT NULL(80)(store_mysql.go:64-87);updated_at ON UPDATE | `idx_canvas_tasks_canvas(canvas_id,created_at)`、`idx_canvas_tasks_status(status,created_at)`(status 索引是 worker 队列扫描,store_mysql.go:89-92 注释) | canvas_id → canvases.id(0 = 无画布);asset_id → assets.id;ip_id → ips.id |
| `characters` | character | ip_id NOT NULL;name/alias/description/detail(角色圣经);**voice_id BIGINT UNSIGNED NULL 为 57 号票加列迁移**(store_mysql.go:64-70),语义引用 voices.**id**(台账行主键,经 `voice.Store.Get(ctx, id)` 校验,character/handler.go:43-46、voice/store_mysql.go:104-111) | `idx_characters_ip(ip_id,id)` | ip_id → ips.id;voice_id → voices.id |
| `character_variants` | character | character_id;name/note;sort INT 默认 0 | `idx_character_variants_character(character_id,sort,id)` | character_id → characters.id |
| `character_images` | character | character_id、variant_id、asset_id、label VARCHAR(32)、sort;**无时间列**(character/store_mysql.go:52-62) | `idx_character_images_character(character_id,sort,id)`、`idx_character_images_variant(variant_id)` | character_id → characters.id;variant_id → character_variants.id;asset_id → assets.id |
| `props` | prop | 与 characters 同构减 voice_id(65 号票空起步,无迁移,prop/store_mysql.go:22-24) | `idx_props_ip(ip_id,id)` | ip_id → ips.id |
| `prop_images` | prop | prop_id、asset_id、label、sort;无时间列 | `idx_prop_images_prop(prop_id,sort,id)` | prop_id → props.id;asset_id → assets.id |
| `scenes` | scene | 与 props 同构(65 号票空起步) | `idx_scenes_ip(ip_id,id)` | ip_id → ips.id |
| `scene_images` | scene | scene_id、asset_id、label、sort;无时间列 | `idx_scene_images_scene(scene_id,sort,id)` | scene_id → scenes.id;asset_id → assets.id |
| `voices` | voice | **voice_id VARCHAR(255) 唯一**(上游音色 ID;MySQL 表内 `UNIQUE KEY uk_voices_voice_id`,SQLite 独立 `CREATE UNIQUE INDEX IF NOT EXISTS`,两侧索引名一致,voice/store_sqlite.go:25-27、44-46);channel_id、target_model、source_mode、source_prompt 可空;sample_asset_id / preview_asset_id 可空;status VARCHAR(16)(57 号票空起步,无迁移) | `uk_voices_voice_id(voice_id)` | channel_id → channels.id;sample/preview_asset_id → assets.id;被 characters.voice_id 逻辑引用 |

计数复核:网关域 10 张 + 画布域 12 张 = **22 张**;CREATE TABLE 语句逐条出处见 grep 结果(settings×2、canvas_tasks、props、prop_images、assets、admin_accounts、characters×3、video_tasks、usage_logs、canvases、ips、channels、api_keys×2、music_tasks、voices、prompt_templates、scenes、scene_images、model_prices——两侧合计 44 条,每表两条方言 DDL)。

**数据库归属**:gateway 与 canvas 在 Docker 栈共用同一个 MySQL 库 `infinitechance`(docker-compose.yml:15、38),`admin_accounts`/`channels`/`model_prices`/`prompt_templates`/`settings`/`assets` 等表跨服务共享,两入口各自幂等 EnsureSchema(canvas/server/main.go:30-32、74-76 注释)。桌面版同理单库共享(desktop/runtime.go:89 注释)。

## 二、EnsureSchema 运行机制

**谁调用、什么顺序**——全部在 server 入口 / 桌面壳的装配代码里,不在 wiring:

1. **网关**(gateway/server/main.go:26-71):`app.Run` 回调内逐个 new store → EnsureSchema → 失败 `log.Fatalf`。顺序:auth → channels → apikey → pricing → usage → videotask → musictask → prompttemplate → settings → assets。
2. **画布**(canvas/server/main.go:29-100):同款逐个模式。顺序:auth → canvases → assets → **ips → characters → scenes → props → voices** → channels → prices → canvas_tasks → prompt_templates → settings。**唯一显式声明的顺序依赖:canvases → assets → ips**(canvas/server/main.go:45-47 注释:ips 的自愈迁移依赖 `canvases.ip_id` 列先就位)。
3. **桌面**(desktop/runtime.go:115-124):17 个 store 装进切片,一个 for 循环依次 EnsureSchema,任一失败关库退出;「列表顺序即装配顺序」(runtime.go:103 注释),顺序与画布服务同构(auths, channels, keys, prices, usageLogs, videoTasks, musicTasks, templates, settingsStore, canvases, assets, ipStore, characters, scenes, props, voices, tasks)。

**wiring 的契约**:「Schema creation is NOT wiring's job — every store's EnsureSchema must have run before its bundle is passed in」(internal/wiring/wiring.go:5-6)。wiring 只收接口型 bundle,GatewayStores/CanvasStores 双形态(MySQL 服务与桌面 SQLite)共用同一套路由树。

**幂等手法三件套**:

1. `CREATE TABLE IF NOT EXISTS`——所有 44 条建表 DDL 的兜底;但它**永不加宽老表**,这是全仓库迁移代码存在的根本理由(channel/store_mysql.go:36-38 注释明确此决策)。
2. **MySQL 探测**:加列用 `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`(channel/store_mysql.go:50-63 为通用形态);加索引用 `information_schema.STATISTICS`(asset/store_mysql.go:96-110);**形状迁移**读 `DATA_TYPE`(apikey expires_at,store_mysql.go:35-50)与 `EXTRA`(auth 去 AUTO_INCREMENT,store_mysql.go:44+)。
3. **SQLite 探测**:`PRAGMA table_info(表名)` 逐列比对列名(channel/store_sqlite.go:53-78 注释「PRAGMA table_info is SQLite's information_schema」;canvastask/store_sqlite.go:117、canvas/store_sqlite.go:119 等)。

**非 DDL 的启动迁移(文件化 migrations 时必须归置的三类)**:

- `apikey.migrateExpiryToDateTime`:expires_at 列类型改写(仅 MySQL;SQLite 侧明确「没有对应物」,apikey/store_sqlite.go:45-48)。
- `auth.migrateLegacyAutoIncrement`:id 去 AUTO_INCREMENT 并把幸存行归一到 id=1(仅 MySQL;SQLite 侧同注,auth/store_sqlite.go:30-32)。
- `canvas` IP 自愈迁移 `selfHeal`(双方言各一份,canvas/ip_store_mysql.go:34-56、ip_store_sqlite.go:37+):仅当「存在 ip_id=0 画布且 ips 表为空」时建「默认 IP」并收编,条件不看名字所以重跑幂等;新库不预建。

**已知坑(48 号票教训的实际落点)**:

- **SQLite:列迁移必须先于引用该列的索引**。asset 包是正确范例:EnsureSchema 顺序为建表 → 列迁移循环 → 索引循环,注释明说「存量表没有 content_hash 列时,带列的 CREATE INDEX 会先炸」(asset/store_sqlite.go:68-70)。
- **例外**:canvastask 反序(建表 → 索引 → 列迁移,store_sqlite.go:107-116)但安全,理由是**索引引用的列都在建表 DDL 里、迁移加的列不被索引引用**;81/80 号票的注释把这条纪律写成了口诀:「列不被两条索引引用,新装库随建表 DDL 先于索引存在,存量库走迁移循环」(canvastask/store_sqlite.go:85-88、99-102)。prop/scene 的 EnsureSchema 也把「若后续加列须排在索引之前」写进了注释(prop/store_sqlite.go:54-56、scene/store_sqlite.go:54-56)。
- 48 号票票面(.scratch/infinite-chance/issues/48-asset-content-hash-dedup.md:17)记录了 schema 决策本身:`content_hash VARCHAR(64)` 空串 = 未算(历史行态)、`object_key` 加普通索引、**不加唯一索引**。
- **SQLite 侧 6 列没有迁移条目**:assets 的 object_key/content_type/size_bytes/last_used_at、canvas_tasks 的 seconds/image_ref 只活在建表 DDL(asset/store_sqlite.go:63-66 迁移列表仅 content_hash/ip_id 两列)。git 考古:桌面 SQLite store 诞生于 2026-09-10(ded34d8 「Wails v3 shell with SQLite dual-dialect stores」),12 号票(2026-09-04,e75b8b0)加视频列时 SQLite store 尚不存在,故桌面建表 DDL 生而含全部列;21 号票(cb82555)起才确立「新列双方言都要进迁移列表」的纪律,注释「此前视频列未迁移是因为当时桌面尚无存量」(canvastask/store_sqlite.go:74-77)。`image_refs/video_refs` 在 21 号票补了防御性迁移条目,`seconds/image_ref` 没有。
- **测试保障**:双方言都有 EnsureSchema 幂等重跑测试(如 prop/prop_mysql_test.go:225、prop/prop_sqlite_test.go:196、auth/store_mysql_test.go:70、canvas/store_mysql_test.go:292、character/character_sqlite_test.go:266、canvas/ip_sqlite_test.go:304「存量库加 style 列后自愈」)。

## 三、桌面 SQLite 路径

- **建库**:desktop/runtime.go:82 经 `sqlitedb.Open(filepath.Join(dataDir, "app.db"))` 打开单文件库。驱动 `modernc.org/sqlite` 纯 Go、免 cgo(ADR 0001,docs/adr/0001-sqlite-dual-dialect-stores.md:6)。
- **连接 pragma**:DSN 携带 `_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)`,挂 DSN 上是为了每个池化连接都生效(连接级 pragma 经 db.Exec 只会命中第一条连接,曾导致假性 SQLITE_BUSY,sqlitedb/sqlitedb.go:33-42);`SetMaxOpenConns(4)` 封顶防止双服务 + worker 突发把文件锁打崩(sqlitedb.go:62-66)。注意:**foreign_keys(ON) 对本库实际不约束任何表**——全库无 FOREIGN KEY 声明,grep `FOREIGN KEY|REFERENCES` 在 internal/ 零命中。
- **落盘位置**:OS 应用数据目录(macOS `~/Library/Application Support/InfiniteChance`,Windows %AppData%,Linux $XDG_CONFIG_HOME),`INFINITECHANCE_DATA_DIR` 可整体覆写(desktop/config.go:13、39-57)。
- **装配差异面(与 Docker 栈)**:
  - 无 Redis:桌面 pingers 只有 `sqlite`(desktop/runtime.go:173);Redis 在服务器栈仅承担健康检查 ping,无业务逻辑(ADR 0001:21-22)。
  - 单进程双服务:gateway/canvas 各一个 loopback 监听,共享同一 `*sql.DB` 与同一批 store 实例(runtime.go:43-45、89-114),schema 一次建齐(「全部 IF NOT EXISTS,顺序无关」是对各 store 内部而言,族间仍有 canvases→assets→ips 依赖)。
  - 数据互不通用:桌面 SQLite 与 Docker MySQL 无迁移关系(AGENTS.md 硬性约束 11);MySQL-only 的三处迁移(apikey expires_at、auth AUTO_INCREMENT、以及 usage 的 information_schema source 加列)在 SQLite 侧均无对应物,桌面新装库直接建出含全部列的表(usage/store_sqlite.go:47-49、apikey/store_sqlite.go:45-48、auth/store_sqlite.go:30-32)。
  - 首启服务键:`ensureServiceKey` 在 EnsureSchema 之后、装配之前跑(runtime.go:126-129),API key 语义与服务器一致(SHA-256 哈希落库)。
- **服务端栈参照**:MySQL 8.4(compose 内 3306、宿主机映射 3307)、Redis 7(宿主机 6380)(docker-compose.yml:32-57);备份走 `mysqldump --single-transaction --routines --triggers --databases infinitechance` + redis RDB + 素材卷 tar(deploy/backup.sh:27-37)——**备份体系目前以 MySQL 为权威全量**,桌面 app.db 不在备份脚本射程内。

## 四、基线(database.sql)选项盘点——只列事实与选项,不拍板

**方言 DDL 差异清单**(单份基线必须附带翻译规则才能覆盖两侧):

1. 自增主键:`BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY` vs `INTEGER PRIMARY KEY AUTOINCREMENT`;另有三种非自增 PK 形态(自然键 settings.name、model_prices.public_model;应用字符串 id:video_tasks/music_tasks/canvas_tasks;固定值 admin_accounts.id=1)。
2. 时间列:`TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6)`(+updated_at 的 `ON UPDATE`)vs `TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%f000Z','now'))`(毫秒兜底 + 写入方定宽纳秒);**ON UPDATE 语义 SQLite 无对应物**,靠写入方显式给值(apikey/store_sqlite.go:61-63 等)。
3. JSON 列:MySQL 原生 `JSON` 共 9 处(channels.model_map/capabilities/config、usage_logs.price_snapshot、canvases.graph、settings.value、video_tasks/music_tasks/canvas_tasks 若干 price_snapshot)vs SQLite 一律 `TEXT`。
4. 大文本:MySQL `TEXT`/`MEDIUMTEXT`/`VARCHAR(n)`/`CHAR(64)` vs SQLite 一律 `TEXT`(MEDIUMTEXT 的存在理由是 data: URI 几 MB,asset/store_mysql.go:16-17)。
5. 布尔:`TINYINT(1)` vs `INTEGER`;浮点:`FLOAT` vs `REAL`(canvas_tasks.cover_strength)。
6. 引擎子句:`ENGINE=InnoDB DEFAULT CHARSET=utf8mb4` 仅 MySQL。
7. 唯一约束落点:MySQL 表内 `UNIQUE KEY` vs SQLite 独立 `CREATE UNIQUE INDEX IF NOT EXISTS`(名字两侧一致:uniq_api_key_hash、uniq_admin_username、uk_voices_voice_id)。
8. 二级索引落点:MySQL 建表内 `KEY` 子句 vs SQLite 独立 `CREATE INDEX IF NOT EXISTS`(名字沿用 MySQL 版,asset/store_sqlite.go:46-49 注释)。
9. upsert 语句族:`ON DUPLICATE KEY UPDATE` vs `ON CONFLICT … DO UPDATE`(ADR 0001:19-20;非 DDL 但同属方言翻译面)。
10. MySQL-only 形状迁移两处 + 数据自愈迁移一处(见第二节),SQLite 基线天然不含前两者。
11. MySQL 有 `utf8mb4` 索引长度约束意识(48 号票:VARCHAR(512) 全列 2048 字节低于 InnoDB 3072 上限,issue 48:17);SQLite 无此概念。

**选项与各自的事实约束**:

- **选项 A:MySQL 权威单份** database.sql,SQLite 侧继续靠 EnsureSchema 自愈(或由翻译器生成、不入库)。
  事实约束:备份/恢复体系已以 MySQL 为权威(deploy/backup.sh);桌面与服务器数据互不通用,SQLite 没有「从基线重建」的运行时诉求;但「schema 单一出处」的目标对桌面侧不成立,SQLite 漂移(如 6 列无迁移条目的历史包袱)依旧只能靠代码审阅发现;且需要维护一份 MySQL→SQLite 的翻译规则来核对两侧一致性。
- **选项 B:双方言两份** database.sql + database.sqlite.sql。
  事实约束:两侧 DDL 现全部在代码里,可机械导出拼装;SQLite 基线可直接取各 store 建表 DDL(生而含全部列),迁移剥离时**不能照搬 MySQL 迁移清单**(6 列差异,见第二节);两份基线的等价性需要一条自动化对账手段(表名/列名/索引名集合比对)否则会重演「两侧漂移无出处」的老问题;与 ADR 0001「schema 演进双处同步」的既有纪律同构,只是从代码内搬到文件。
- **共同约束(无论 A/B)**:①现有存量库升级路径不能断——文件化 migrations 落地后,存量库首轮要么继续走 EnsureSchema 加宽、要么以「基线 + 补齐迁移」收编,双轨过渡期必须有明确判据;②三处非 DDL 迁移(列类型改写、主键改写、数据自愈)在纯 SQL 文件范式下如何表达是 02 号票必须回答的形状问题;③「SQLite 列迁移先于索引」的排序纪律(asset 正确范例)与「索引引用列必须在基线 DDL 内」的不变量,是迁移执行器要继承的排序约束。

## 给 02 号票的事实输入

1. **规模与出处**:22 张表、16 包、34 个 EnsureSchema 方法;schema 唯一权威出处是各 store 的 DDL 字符串常量,wiring 明确不建 schema(internal/wiring/wiring.go:5-6);三个入口各自装配,gateway 10 个 store、canvas 13 个、桌面 17 个循环,唯一族间顺序依赖 canvases → assets → ips(canvas/server/main.go:45-47)。
2. **幂等手法与迁移形态**:全部基于 `CREATE TABLE IF NOT EXISTS` + information_schema/PRAGMA 逐列探测,无版本表;除加列/加索引外还有两处 MySQL 形状迁移(apikey、auth)与一处双方言数据自愈(ips selfHeal)——文件化 migrations 的执行器要能表达这三种非「ADD COLUMN」形态。
3. **SQLite 历史包袱**:6 列(assets.object_key/content_type/size_bytes/last_used_at、canvas_tasks.seconds/image_ref)只在建表 DDL、无迁移条目,桌面库诞生即含;从代码导出基线时,SQLite 基线 = 各建表 DDL 直接拼接,MySQL 基线 = 建表 DDL + 迁移链收敛后的最终形状,**两侧迁移清单不对称**。
4. **方言差异 11 类**(见第四节清单),其中「ON UPDATE CURRENT_TIMESTAMP」「JSON 列」「UNIQUE KEY/KEY 落点」是等价性对账时最容易漏的三类;全库无 SQL 外键,引用完整性全在应用层,基线不需要(也不该)补 FK。
5. **排序不变量**:SQLite 迁移执行顺序必须「列迁移先于引用该列的索引」(asset/store_sqlite.go:68-70);canvastask 的反序安全依赖「索引引用列必须在建表 DDL 内」这一前提——migrations 文件化后这两条要变成执行器约束或文件内显式排序,不能继续靠各包注释口口相传。

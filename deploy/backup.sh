#!/usr/bin/env bash
# 全栈数据备份(16 号票):MySQL 逻辑转储 + Redis RDB + 素材卷整卷打包。
#
# 前提:compose 栈在运行(备份借助容器内的 mysqldump/redis-cli/tar,
# 脚本与数据均不依赖宿主机装了什么)。一致性边界:MySQL 转储是
# --single-transaction 一致性快照,Redis 经 SAVE 落盘,素材 tar 在 canvas
# 停服窗口内打包;跨存储间无全局一致点(自用栈可接受)。产物是一个目录:
#   mysql-infinitechance.sql.gz  一致性转储(--single-transaction,含建库语句)
#   redis-dump.rdb               SAVE 落盘后的 RDB 快照
#   assets.tar.gz                素材卷(asset-data)整卷打包
#   MANIFEST.txt                 时间戳、git 提交、大小与 SHA-256 校验
# 恢复见同目录 restore.sh。定期备份示例(每天凌晨 3 点):
#   0 3 * * * cd /path/to/InfiniteChance && deploy/backup.sh >> backups/backup.log 2>&1
#
# 用法:deploy/backup.sh [输出目录]     # 缺省 backups/YYYYmmdd-HHMMSS
set -euo pipefail

cd "$(dirname "$0")/.."

OUT="${1:-backups/$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$OUT"

echo "== InfiniteChance 备份 → $OUT =="

# 1. MySQL:InnoDB 一致性快照;--databases 带上 CREATE DATABASE,可恢复进空实例。
#    密码引用容器自身的 MYSQL_ROOT_PASSWORD 展开,脚本侧不接触明文。
docker compose exec -T mysql sh -c 'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --routines --triggers --databases infinitechance' \
  | gzip > "$OUT/mysql-infinitechance.sql.gz"

# 2. Redis:SAVE 阻塞落盘(自用数据集很小,秒级完成)后读走 RDB。
docker compose exec redis redis-cli SAVE >/dev/null
docker compose exec -T redis cat /data/dump.rdb > "$OUT/redis-dump.rdb"

# 3. 素材卷:canvas 停服窗口内整卷打包(与恢复的停服语义对称),避免活动
#    写入撕裂 tar;worker 在途任务随 start 后的重启恢复机制照常重跑。
docker compose stop canvas
docker compose run --rm --no-deps canvas tar czf - -C /data/assets . > "$OUT/assets.tar.gz"
docker compose start canvas

# 4. 清单与校验和(Linux 用 sha256sum,macOS 退回 shasum)。
if command -v sha256sum >/dev/null 2>&1; then
  SHA="sha256sum"
else
  SHA="shasum -a 256"
fi
(
  echo "created_at: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  echo "git_commit: $(git rev-parse HEAD 2>/dev/null || echo unknown)"
  echo
  ls -l "$OUT/mysql-infinitechance.sql.gz" "$OUT/redis-dump.rdb" "$OUT/assets.tar.gz"
  echo
  $SHA "$OUT/mysql-infinitechance.sql.gz" "$OUT/redis-dump.rdb" "$OUT/assets.tar.gz"
) > "$OUT/MANIFEST.txt"

echo "完成:"
ls -lh "$OUT"

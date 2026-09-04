#!/usr/bin/env bash
# 从 backup.sh 的备份目录恢复全栈数据:MySQL → Redis → 素材卷(16 号票)。
#
# 前提:compose 栈在运行。恢复会覆盖现有数据:
#   MySQL   直接重放转储(表级 DROP/CREATE,库缺省自建)
#   Redis   停服替换 RDB 后再启动(绕开 SIGTERM 落盘覆盖新文件的竞态)
#   素材卷  停服清空后整卷解包(canvas worker 随之重跑在途任务,与重启同语义)
#
# 用法:deploy/restore.sh <备份目录> [-y]     # -y 跳过确认(供脚本化演练)
set -euo pipefail

DIR="${1:-}"
if [ -z "$DIR" ]; then
  echo "用法: deploy/restore.sh <备份目录> [-y]" >&2
  exit 1
fi
cd "$(dirname "$0")/.."

SQL="$DIR/mysql-infinitechance.sql.gz"
RDB="$DIR/redis-dump.rdb"
ASSETS="$DIR/assets.tar.gz"
for f in "$SQL" "$RDB" "$ASSETS"; do
  if [ ! -f "$f" ]; then
    echo "缺少 $f —— 确认这是 backup.sh 产出的备份目录" >&2
    exit 1
  fi
done

if [ "${2:-}" != "-y" ]; then
  printf '将覆盖当前 MySQL、Redis 与素材卷数据(备份目录:%s)。继续? [y/N] ' "$DIR"
  read -r answer
  case "$answer" in
    y|Y|yes|YES) ;;
    *) echo "已取消"; exit 1 ;;
  esac
fi

echo "== 恢复 MySQL =="
# 密码引用容器自身的 MYSQL_ROOT_PASSWORD,与 backup.sh 同一约定。
gunzip -c "$SQL" | docker compose exec -T mysql sh -c 'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD"'

echo "== 恢复 Redis =="
# stop(SIGTERM 落盘的是旧数据,无妨)→ 借同服务的临时容器把 RDB 写进数据卷
# → start 后 redis 加载新 RDB。run --no-deps 复用服务的镜像与卷,免解析卷名。
docker compose stop redis
docker compose run --rm --no-deps --entrypoint sh redis -c 'cat > /data/dump.rdb' < "$RDB"
docker compose start redis

echo "== 恢复素材卷 =="
docker compose stop canvas
# && 串联:清空失败就不要继续解包,避免把备份盖在残留文件上。
docker compose run --rm --no-deps --entrypoint sh canvas \
  -c 'find /data/assets -mindepth 1 -delete && tar xzf - -C /data/assets' < "$ASSETS"
docker compose start canvas

echo "== 完成,抽查 =="
# admin_accounts 行数应 ≥ 1(管理员账号恢复成功);DBSIZE 是 Redis 键数。
docker compose exec -T mysql sh -c 'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -N -e "SELECT COUNT(*) FROM infinitechance.admin_accounts"' \
  | sed 's/^/admin_accounts 行数: /'
docker compose exec redis redis-cli DBSIZE | sed 's/^/Redis 键数: /'

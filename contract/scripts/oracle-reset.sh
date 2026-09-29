#!/usr/bin/env bash
# 重建 oracle 库（只动 vinx_oracle）：drop/create → 旧仓库 prisma migrate deploy → seed，然后重启 oracle API。
#
# 环境变量：
#   OLD_REPO      旧仓库根目录（默认 ~/projects/vinx-vocab）
#   ORACLE_DB     库名（默认 vinx_oracle；出于安全只允许 vinx_oracle*）
#   ORACLE_TMUX   运行 oracle API 的 tmux 目标（默认 vinx-oracle；设为空则不重启）。
#                 oracle 须作为该 pane 的启动命令运行（tmux new-session -d -s vinx-oracle "<命令>"），
#                 重启用 respawn-pane -k 按原命令重新执行
#   ORACLE_PORT   oracle API 端口（默认 4100，用于等待重启完成）
#
# 连接参数（用户、密码、主机、端口）取自旧仓库 apps/api/.env 的 DATABASE_URL，只替换库名，不写入任何文件。
set -euo pipefail

OLD_REPO="${OLD_REPO:-$HOME/projects/vinx-vocab}"
ORACLE_DB="${ORACLE_DB:-vinx_oracle}"
ORACLE_TMUX="${ORACLE_TMUX-vinx-oracle}"
ORACLE_PORT="${ORACLE_PORT:-4100}"
API_DIR="$OLD_REPO/apps/api"

case "$ORACLE_DB" in
  vinx_oracle*) ;;
  *) echo "拒绝操作：ORACLE_DB 必须以 vinx_oracle 开头（当前 $ORACLE_DB）" >&2; exit 1 ;;
esac

base_url=$(grep -E '^DATABASE_URL=' "$API_DIR/.env" | head -1 | cut -d= -f2- | tr -d '"'"'")
[ -n "$base_url" ] || { echo "读不到 $API_DIR/.env 的 DATABASE_URL" >&2; exit 1; }
# postgresql://user:pass@host:port/db?params → 换库名
prefix="${base_url%%\?*}"; prefix="${prefix%/*}"
query=""; [[ "$base_url" == *\?* ]] && query="?${base_url#*\?}"
ORACLE_URL="$prefix/$ORACLE_DB$query"
ADMIN_URL="$prefix/postgres$query"

echo "== 重建库 $ORACLE_DB"
psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -q \
  -c "DROP DATABASE IF EXISTS \"$ORACLE_DB\" WITH (FORCE)" \
  -c "CREATE DATABASE \"$ORACLE_DB\""

echo "== 迁移与 seed"
cd "$API_DIR"
DATABASE_URL="$ORACLE_URL" npx prisma migrate deploy
DATABASE_URL="$ORACLE_URL" npx tsx prisma/seed.ts

if [ -n "$ORACLE_TMUX" ] && tmux has-session -t "${ORACLE_TMUX%%:*}" 2>/dev/null; then
  echo "== 重启 oracle API（tmux $ORACLE_TMUX）"
  # oracle 以 pane 的启动命令运行：respawn-pane -k 结束旧进程并按原命令重新启动（连接池换到新库）
  tmux respawn-pane -k -t "$ORACLE_TMUX"
  sleep 2
  for _ in $(seq 1 60); do
    if ss -ltn | grep -q ":$ORACLE_PORT "; then
      curl -s --max-time 2 "http://localhost:$ORACLE_PORT/health" >/dev/null && { echo "oracle 已就绪 :$ORACLE_PORT"; exit 0; }
    fi
    sleep 1
  done
  echo "oracle 未在 60 秒内就绪，请查看 tmux $ORACLE_TMUX" >&2
  exit 1
fi

#!/usr/bin/env bash
# 起三个 Go 单文件实例跑 e2e：seed-demo 数据（E2E_PORT，默认 3250）+ 全新数据目录（E2E_FRESH_PORT，默认 3251）
# + 另一份 seed-demo 数据、以 --dev 启动（E2E_DEV_PORT，默认 3252，只给开发模式切换账号的用例用）。
# 用法：bash e2e/run.sh [playwright 参数…]（先 make build 生成 dist/vinx-vocab；BIN 可指定别的可执行文件）
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${BIN:-$ROOT/dist/vinx-vocab}"
PORT="${E2E_PORT:-3250}"
FRESH_PORT="${E2E_FRESH_PORT:-3251}"
DEV_PORT="${E2E_DEV_PORT:-3252}"
[ -x "$BIN" ] || { echo "找不到 $BIN，先 make build" >&2; exit 1; }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/vinx-e2e.XXXXXX")"
PIDS=()
cleanup() {
  for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

# 端口已被占用就不跑（避免连到别的服务上）；用 ss 看，不去连接（本机连没人监听的端口可能挂起）
for p in "$PORT" "$FRESH_PORT" "$DEV_PORT"; do
  if ss -ltnH "sport = :$p" | grep -q .; then echo "端口 $p 已被占用" >&2; exit 1; fi
done

"$BIN" seed-demo --data "$WORK/demo" >"$WORK/seed.log" 2>&1
"$BIN" seed-demo --data "$WORK/dev" >>"$WORK/seed.log" 2>&1
# 与旧版 e2e 环境一致：班级版、开放注册；AI 由用例自己在系统设置里指向假服务
env -u VINX_EDITION "$BIN" --data "$WORK/demo" --port "$PORT" >"$WORK/demo.log" 2>&1 &
PIDS+=($!)
env -u VINX_EDITION "$BIN" --data "$WORK/fresh" --port "$FRESH_PORT" >"$WORK/fresh.log" 2>&1 &
PIDS+=($!)
# 开发模式（spec 0002）：任何人都能免密切换账号，只用临时的 seed-demo 数据
env -u VINX_EDITION "$BIN" --dev --data "$WORK/dev" --port "$DEV_PORT" >"$WORK/dev.log" 2>&1 &
PIDS+=($!)

for p in "$PORT" "$FRESH_PORT" "$DEV_PORT"; do
  for _ in $(seq 1 100); do
    ss -ltnH "sport = :$p" | grep -q . && break
    sleep 0.1
  done
  curl -sf --max-time 2 "http://127.0.0.1:$p/api/health" >/dev/null || { echo "实例 :$p 没有起来" >&2; cat "$WORK"/*.log >&2; exit 1; }
done

cd "$ROOT/e2e"
[ -d node_modules ] || pnpm install --frozen-lockfile
E2E_BASE_URL="http://localhost:$PORT" E2E_FRESH_URL="http://localhost:$FRESH_PORT" E2E_DEV_URL="http://localhost:$DEV_PORT" npx playwright test "$@"

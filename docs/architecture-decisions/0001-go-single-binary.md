# 0001 单文件：Go + SQLite + 嵌入 Preact 前端

状态：已采纳（「以旧版为规格」一条已被 [0004](0004-single-binary-is-source-of-truth.md) 取代）

## 背景

服务器版（Node + Fastify + Prisma + PostgreSQL + refine/antd）需要服务器和 Docker，普通家庭用户装不起来。要求同一套功能以一个可执行文件运行：Windows 双击即用，Linux 版做班级服务器，旧数据能完整迁移。需求与取舍见 [spec 0001](../specs/0001-go-single-binary.md)。

## 决定

- 后端 Go，纯 Go 依赖（`modernc.org/sqlite`，不需要 CGO），交叉编译 windows/amd64 与 linux/amd64。
- 数据库 SQLite：表名、列名、字符串 id 与旧 Prisma 一致；写事务一律 `BEGIN IMMEDIATE`，WAL，`busy_timeout`，`foreign_keys=ON`；嵌入式迁移链，有待执行迁移时先备份。
- 前端 Preact + Vite，自写组件库复刻 antd 5 的 DOM 与样式；构建产物用 `go:embed` 嵌入。
- 以旧版为规格：API 契约用同一套黑盒契约测试先对旧版、再对 Go 版跑绿；E2E 沿用旧版用例；新旧截图逐页对比。
- FSRS 逐行移植 ts-fsrs（`go-fsrs` 与 ts-fsrs 结果不一致），用 ts-fsrs 生成的固定序列对拍。

## 后果

- 分层与约定见 [architecture.md](../architecture.md)：`internal/core` 纯函数、`service` 装配与事务、`api` 路由与校验。
- 与旧版的有意偏差集中记在 architecture.md 与 decisions.md（K42–K46）；其他地方以旧版行为为准。
- 单进程：AI 后台任务在内存里，重启丢失（沿用 K41）；SQLite 写操作串行，对班级规模足够。
- JS 体积 341.9KB，未达到 200KB 目标（见 spec 0001「结果」）。

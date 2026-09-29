# Vinx Vocab 单文件版 agent 协作约定

适用于所有在本仓库工作的 agent 和贡献者。本文件会公开：只写通用规则，不写本机路径、主机名、个人或客户信息（这些放不提交的 `CLAUDE.local.md`）。

## 定位与事实边界

Vinx Vocab 单文件版是服务器版（Node + Fastify + Prisma + PostgreSQL，下称**旧版**）的 Go 重写：一个可执行文件内含 SQLite 与嵌入的前端，Windows 双击即用，Linux 版可做班级服务器。**功能、接口契约、页面与交互都以旧版为准**；有意的偏差写在 [docs/architecture.md](docs/architecture.md) 与 [docs/decisions.md](docs/decisions.md)。先读 `README.md`，再按任务读 `docs/`。

不得把配置示例称为已部署，不得把拟定的方案称为已实现、已验证，不得依据历史记录判断服务当前仍在运行。

## 代码结构与分层

- `cmd/vinx-vocab/`：子命令 `serve`（默认）/ `seed-demo` / `audio prefetch` / `import` / `version`。
- **业务规则放 `internal/core/` 纯函数并配表驱动单测**（不触库）；`internal/service/` 只做装配与事务；`internal/api/` 只做路由、校验与权限。
- 数据范围（本人 / 本班 / 全部）集中在 `internal/service/access.go`；角色能力表在 `internal/core/access.go`。「看全部数据」用 `service.SeesAll`，不用 `IsAdmin`（个人版下管理员也只看自己的）。
- 班级版专属逻辑在 `internal/school/`；版本（personal / school）每个请求按 `VINX_EDITION` > `AppSetting["edition"]` > 默认 school 判定。
- API 挂在 `/api/` 下，去掉前缀后路径、字段、`null` 与缺省、错误码与中文 message、HTTP 状态码都与旧版一致。响应包络 `{ success, data?, error?: { code, message, details?, requestId? }, timestamp? }`。
- 数据库：SQLite，表名、列名、字符串 id 与旧版 Prisma 一致；迁移只增不改，放 `internal/store/migrations/NNNN_名称.sql`。部分唯一索引 `StudySession_active_uniq` 必须保留。事务回调里只能用 `tx`（见 architecture.md）。
- 时间一律经 `d.Now()`（可注入时钟）；学习日按 `APP_TIMEZONE`（默认 Asia/Shanghai）切分。
- 前端 `web/`：Preact + `preact-iso` + Vite，自写组件库 `web/src/ui/`（DOM 与样式复刻 antd 5），不引入 refine / antd / TanStack Query / axios。

## 文件与数据边界

- `data/vocab/` 是内置词表（嵌入，首次启动导入）；运行时数据目录（`./data/*` 除 `vocab/`、`vinx-data/`）不入库。
- `dist/`、`web/dist/`、`internal/web/dist/*`（占位页除外）、`*.syso` 是构建产物，不手改。
- 测试只用测试里自建的数据或 `seed-demo` 数据，不连接真实部署的数据库。PostgreSQL 导入测试用 `VINX_TEST_PG_URL` 指定的测试库，每个用例建临时库并删除。

## 运行与部署

- 默认监听 `0.0.0.0:3000`。本机同时跑着旧版开发服务时，用 `--port` 换一个端口。
- 认证是 JWT + HttpOnly Cookie（`vinx_token`，SameSite=Lax）。cookie 按主机名归属，同一台设备固定用一个主机名访问。
- 部署、重启服务、改系统配置属于先说明再执行的操作。

## 工作方式

- 动手实现较大的改动前，先写说明、确认方案，不自行偏离既定规划。
- 对不确定的事实先做有限的只读核查。需要安装额外组件、扩大读写范围、处理凭据时，先说明方案与影响，等待确认。
- 外部资源获取受限时停止并报告，不反复尝试未知镜像、关闭校验或更改权限。
- Go 环境变量写在项目内（不入库的 `local.mk` 或命令前缀），不用 `go env -w`。
- 提交信息用 Conventional Commits（`feat:` / `fix:` / `chore:` / `docs:`）。

## 需求与记录

- 较大的改动先在 `docs/specs/` 写一份说明，完成后在同一文件补上结果。
- 产品规则层面的决定追加到 `docs/decisions.md`（K 编号只增不复用，与旧版共用一条编号序列；被取代的标注不删）。
- 这些文件都会公开：不写本机路径、主机名、内网地址、个人或客户信息。

## 验证与交接

区分：类型检查与静态检查（`make vet`、`pnpm -C web exec tsc --noEmit`）、Go 单测（`make test`）、前端单测（`pnpm -C web exec vitest run`）、API 契约测试（`make contract`，对运行中的实例）、E2E（`make e2e`）和真实浏览器验证，只有对应验证完成才报告通过。交接时说明修改的文件、执行过的验证、未验证项。

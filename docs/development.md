# Vinx Vocab 单文件版开发规则

所有开发遵守本规则。实现层面的细则（数据库、HTTP、校验、认证、时间、学习流、导入）见 [architecture.md](architecture.md)，这里只列总规则。

## 0. 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go（版本见 `go.mod`），标准库 `net/http` 路由 |
| 数据库 | SQLite（`modernc.org/sqlite`，纯 Go，不需要 CGO） |
| 前端 | Preact + `preact-iso` + TypeScript + Vite，自写组件库 `web/src/ui/`（复刻 antd 5） |
| 认证 | JWT + HttpOnly Cookie + 角色能力表 |
| 测试 | Go `testing`（表驱动）+ vitest（前端、契约）+ Playwright（E2E） |
| 构建 | Makefile；前端产物 `go:embed` 嵌入；交叉编译 windows/amd64、linux/amd64 |

## 1. 目录与分层

- `cmd/vinx-vocab/` 入口与子命令；`internal/core/` 纯业务规则（不触库，每个文件配表驱动单测）；`internal/service/` 装配与事务；`internal/api/` 路由、校验、权限；`internal/school/` 班级版专属；`internal/store/` SQLite 与迁移；`internal/migrate/` 从 PostgreSQL 导入。
- 依赖方向：`api → service / school / auth → core / store / httpx`；`core` 不依赖任何内部包。
- 前端 `web/src/pages/` 按功能分目录；纯逻辑放 `web/src/lib/` 并配 vitest。
- 根目录保持干净；文档放 `docs/`。

## 2. 与服务器版的一致性

- 服务器版（Node）是规格：API 路径（去掉 `/api` 前缀后）、方法、字段、`null` 与缺省、时间格式、错误码与中文 message、HTTP 状态码、页面文案与交互都一致。
- 有意的偏差必须写进 [architecture.md](architecture.md)（技术）或 [decisions.md](decisions.md)（产品规则），不能只在代码里改。
- 新功能如果服务器版也要做，先在这边写 spec 说明两边怎么对齐。

## 3. API 契约

- 响应包络 `{ success, data?, error?: { code, message, details?, requestId? }, timestamp? }`；列表 `{ items, total, page?, limit? }`。
- 错误码与 HTTP 状态见 `internal/httpx/errors.go`；请求体校验沿用 zod 的语义与英文默认消息（`internal/httpx/validate.go`）。
- 改接口先改 `contract/` 里的契约用例。

## 4. 认证与权限

- Cookie `vinx_token`（HttpOnly、SameSite=Lax）；同一设备固定一个主机名访问。
- 能力表 `internal/core/access.go`（K23）；数据范围集中在 `internal/service/access.go`；「看全部数据」用 `SeesAll`，不用 `IsAdmin`。
- 背景见 [architecture-decisions/0002](architecture-decisions/0002-jwt-httponly-cookie.md)。

## 5. 数据库

- 表名、列名、字符串 id 与旧 Prisma 一致；迁移只增不改（`internal/store/migrations/NNNN_名称.sql`）。
- `StudySession_active_uniq` 部分唯一索引必须保留。
- 事务回调里只用 `tx`；时间一律经 `d.Now()`。

## 6. 测试

| 类型 | 命令 | 说明 |
|---|---|---|
| Go 静态检查、单测 | `make vet`、`make test` | 服务与接口测试用临时 SQLite + 固定时钟 |
| PostgreSQL 导入 | `VINX_TEST_PG_URL=… make test-import` | 需要能 `CREATE DATABASE` 的测试库 |
| 前端 | `pnpm -C web exec tsc --noEmit`、`pnpm -C web exec vitest run` | |
| API 契约 | `make contract` | 对运行中的实例；新模块在 `contract/lib/areas.ts` 登记 |
| E2E | `make e2e` | 自动起 seed-demo 实例和全新实例 |

只有对应验证完成才报告通过。

## 7. Go 环境

- 不用 `go env -w`、不改 shell 配置；本机的工具链、模块缓存、代理写在不入库的 `local.mk`（Makefile 自动读取）。
- 新依赖先报实测下载量；离线编译用 `GOPROXY=off`。

## 8. Git 与提交

- Conventional Commits：`feat:` / `fix:` / `chore:` / `docs:` / `refactor:`，可用中文说明。

## 9. 文档

```
docs/
  product.md                产品现状
  architecture.md           架构与实现约定
  decisions.md              产品决定（K 编号，与服务器版共用一条序列）
  development.md            本文
  deployment.md             运行、部署与验证
  specs/                    需求说明
  architecture-decisions/   架构决策记录
```

- 较大的改动先在 `docs/specs/` 写一份需求说明，写法见 [specs/README.md](specs/README.md)。
- 改动完成后，把结论合并进上面几份文档。

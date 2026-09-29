---
status: done
---

# Go 单文件版：一个可执行文件跑完整的 Vinx Vocab

## 问题

现有 Vinx Vocab（Node + Fastify + Prisma + PostgreSQL + refine/antd）需要服务器和 Docker 才能运行，普通家庭用户装不起来。目标是让同一套功能以**一个可执行文件**的形式运行：

- Windows 上双击 `vinx-vocab.exe`，电脑自己用；同一局域网的手机用浏览器访问学习；
- 同一份代码编出 Linux 版 `vinx-vocab`，部署到 NAS 上当班级服务器；
- **功能、操作习惯、样式与现有版本一致**，前端改用更轻的框架实现；
- 现有正式环境（PostgreSQL）的数据可以完整迁移过来。

## 方案

### 1. 架构与项目结构

```
vinx-vocab-go/
├── cmd/vinx-vocab/        main：serve（默认）/ import / audio prefetch / version
├── internal/
│   ├── core/              纯业务规则（对应旧 apps/api/src/lib），全部单测
│   ├── store/             SQLite：嵌入式迁移链、查询、事务
│   ├── service/           装配与事务（对应旧 services/）；数据范围判定集中在 access.go
│   ├── api/               HTTP 路由、校验、能力守卫、统一包络、JWT Cookie、Origin 检查
│   ├── school/            班级版专属（班级、邀请码、批量建号、班级概览）
│   ├── ai/  audio/        AI 供应商（OpenAI 兼容 / Claude）与后台任务；真人发音抓取缓存
│   └── migrate/           从 PostgreSQL 导入
├── web/                   Preact 前端（Vite 构建），产物用 go:embed 嵌入
├── data/vocab/            内置词表（嵌入，首次启动自动导入）
├── e2e/                   Playwright 验收
└── docs/
```

- 交叉编译：`vinx-vocab.exe`（windows/amd64）与 `vinx-vocab`（linux/amd64），纯 Go、不需要 CGO。
- 依赖：标准库 `net/http`（1.22+ 路由）、`modernc.org/sqlite`、`github.com/open-spaced-repetition/go-fsrs`、`github.com/golang-jwt/jwt`、`golang.org/x/crypto/bcrypt`、`github.com/jackc/pgx`（仅导入命令用）。
- 默认监听 `0.0.0.0:3000`；`--port` / `--host` 可改。端口被占用时提示后退出，不自动换端口；检测到本机已有实例在跑时直接打开浏览器，不起第二个。
- 数据目录：Windows 默认 exe 旁边的 `vinx-data\`，Linux 默认 `./data`，`--data` 可改。里面放 `vinx.db`、`audio/`、`secret.key`、可选的 `config.toml`。
- 配置优先级：环境变量 > `config.toml` > 默认值。环境变量名沿用旧版（`VINX_EDITION`、`APP_TIMEZONE`、`AI_*`、`AUDIO_PROVIDER_URL`、`SIGNUP_ENABLED`、`JWT_EXPIRES_IN` 等）。
- Windows 双击运行：控制台打印本机地址、局域网地址、数据目录和「关闭此窗口即停止服务」，自动打开浏览器；首次运行的防火墙提示在控制台说明。

### 2. 数据、认证、迁移

- 旧 Prisma 的 17 张表逐张对应到 SQLite，**表名、字段名、字符串 id 保持不变**，迁移时原样保留。时间存 UTC 文本（RFC3339，毫秒），`Json` 存 TEXT，枚举为 TEXT。
- `StudySession_active_uniq` 用 SQLite 部分唯一索引实现：`UNIQUE (userId, COALESCE(planId,''), kind) WHERE status='active'`。
- 写事务统一 `BEGIN IMMEDIATE`（替代 `FOR UPDATE`）；连接开 WAL、`busy_timeout`、`foreign_keys=ON`。
- 迁移链：嵌入式 SQL 文件按序号执行，记录在 `schema_migrations`；有待执行的迁移时，先把 `vinx.db` 复制为 `vinx.db.bak-<时间>`（保留最近 3 份）。
- 认证与旧版一致：JWT 放 HttpOnly Cookie `vinx_token`（SameSite=Lax，默认 7 天），写请求做 Origin 检查，权限用角色能力表 + 数据范围（本人 / 本班 / 全部）。密码 bcrypt，旧哈希直接可用。
- `JWT_SECRET` / `SETTINGS_SECRET`：未通过环境变量或配置给出时，首次启动随机生成写入 `secret.key`。AI Key 继续 AES-256-GCM 加密，格式与旧版一致。
- **版本**：未指定 `VINX_EDITION` 时，首次打开进入一步向导（「自己用」= 个人版 /「老师带班」= 班级版），结果存 `AppSetting`。管理员可在系统设置里双向切换（切换前弹窗确认）。降级为个人版时隐藏班级、邀请码、布置计划、用户管理入口并关闭注册，数据不删，已有账号仍能登录、只看自己的数据；升级回班级版原样恢复。设了 `VINX_EDITION` 时以环境变量为准，界面上不可切换。
- **从 PostgreSQL 导入**：`vinx-vocab import --from-postgres <URL> [--settings-secret <旧密钥>] [--force]`。只读旧库；按表导入，全程一个 SQLite 事务，失败整体回滚；完成后逐表核对行数并打印对账表；旧 AI Key 用旧密钥解密后用新密钥重新加密，旧密钥不落盘；目标库非空时拒绝，`--force` 先备份再覆盖。

### 3. 前端（Preact）

- Preact + `preact-iso` 路由 + Vite + TypeScript；不用 refine、antd、TanStack Query、axios。数据请求用小的 `useApi` hook（包络解包、缓存、失效重取）；日期用 `dayjs`；图表迁移现有手写 `charts.tsx`。
- 自写组件库 `web/src/ui/`，按现有实际用到的做：Button、Input、Textarea、InputNumber、Select（可搜索）、Checkbox、Radio、Segmented、DatePicker、Modal、Popconfirm、Drawer、Dropdown、Tooltip、Message、Table（排序、分页）、Tabs、Steps、Progress、Upload、Form（布局、校验提示）。外观按现有 antd 主题 token 与 `styles.css` 变量复刻，亮色 / 深色各一套；图标用内联 SVG，只取用到的。
- 33 个页面一一对应：路由路径、菜单结构、按钮位置、文案、交互顺序不变；`engine.ts`、`perms.ts`、`types.ts` 等纯逻辑原样迁移；单词单 A4 打印样式原样保留。
- 目标：JS 打包后 ≤ 200KB（未压缩）。
- 营销页 `apps/public` 不进单文件，未登录直接到登录页。

### 4. AI、发音、后台任务

- AI 供应商：OpenAI 兼容（`/v1/chat/completions`）与 Claude（Messages API），`AI_PROVIDER=auto` 判定规则不变；用标准库发请求，不引 SDK。默认提示词模板、隐藏输出格式、回复解析、出错说明（去掉 Key）、提示词预览与编辑、恢复默认全部逐条移植并配单测。页面保存的配置优先于环境变量，保存后立即生效。
- 后台任务按 K41：进程内存、每人并发上限、完成后 1 小时清理、前端轮询；重启后进行中的任务丢失，前端提示重试。
- 发音：按 `AUDIO_PROVIDER_URL` 抓取并缓存到数据目录 `audio/`，失败回退浏览器合成语音；预缓存命令 `vinx-vocab audio prefetch`（沿用 K37 的参数）。
- 出站请求用系统代理 / `HTTPS_PROXY`；AI 超时读 `AI_TIMEOUT_MS`（默认 180 秒）。
- 托盘图标、开机自启本次不做。

### 5. 实施顺序

按功能竖切，每块含后端 + 前端 + 测试：

1. 骨架：配置、数据目录、迁移链、包络、认证、嵌入前端、组件库基础件
2. 登录、注册、个人资料、首次运行向导、版本切换
3. 词书、词表导入、内置词表
4. 学习计划
5. 今日 + 学习流 + FSRS（含对拍）
6. 检测、错词强化、记录与统计
7. 单词单（含打印）
8. 班级版：班级、成员、邀请码、批量建号、班级概览、老师看学生
9. AI 与后台任务、发音、系统设置
10. PostgreSQL 导入工具与迁移演练
11. E2E 与截图对比全量验收
12. 本地部署运行

## 取舍

- **Bun `--compile` 继续用 TS**：能复用现有代码，但单文件约 100MB，Prisma 原生引擎难以塞进单文件，需要换 ORM，Windows 上的边角问题较多。选 Go：体积小、交叉编译简单、无运行时依赖，长期维护更轻。
- **Rust**：体积更小，但开发和维护成本高，对这个规模的应用没有必要。
- **纯前端 PWA（数据存浏览器）**：不需要服务端，但数据锁在一台设备上，班级版无法实现，AI Key 和发音抓取也受限。
- **分两步（先嵌旧前端，再换前端）**：更稳，但界面不复杂，同时改风险可控；以 API 契约一致 + E2E + 截图对比兜底。
- **Svelte**：同样轻，但模板要全部重写；Preact 与 React 语法相同，页面逻辑可以近乎原样迁移。
- **版本只允许单向升级**：最终选择双向切换，降级只隐藏不删数据。

## 验证

1. `internal/core` 纯函数单测：逐条翻译旧 `apps/api/tests/lib/*` 的用例。
2. FSRS 对拍：用 `ts-fsrs` 生成作答序列与排程结果的固定文件，`go-fsrs` 逐条比对（浮点误差 ≤ 1e-6）。
3. API 流程测试：`httptest` + 临时 SQLite，覆盖旧后端流程测试场景；与旧后端的响应对照字段结构。
4. 前端纯逻辑单测（vitest）。
5. E2E：迁移旧 Playwright 用例，指向单文件运行，含手机视口学习流。
6. 截图对比：新旧两版同一组数据，手机（390×844）和电脑（1280×800）逐页截图，生成本地对比页。
7. 迁移演练：用正式数据的拷贝导入，核对行数，抽样比对今日队列、记录、单词单。
8. 交叉编译出 Windows 与 Linux 两个文件；Linux 版在本机实际运行验收。

## 结果

2026-09-29 完成。实现与约定见 [architecture.md](../architecture.md)，新增的产品决定见 [decisions.md](../decisions.md) K42–K46。

### 验证（2026-09-29，本仓库首次提交前的最终一轮）

| 项 | 结果 |
|---|---|
| `make vet`、`make test` | 通过（17 个包） |
| PostgreSQL 导入测试（`make test-import`） | 13/13 通过 |
| FSRS 对拍 | 与 ts-fsrs 2587 步逐位一致（`go-fsrs` 与 ts-fsrs 有差异，改为逐行移植 ts-fsrs） |
| 前端 `tsc --noEmit`、vitest | 通过，66/66 |
| API 契约测试（先对旧版跑绿，再对构建好的 Linux 可执行文件，含个人版实例） | 16 个文件 120/120 |
| E2E（Playwright，对构建好的可执行文件，含手机视口学习流、首次运行向导与版本切换） | 25/25 |
| 截图对比（新旧两版同一组数据，手机 390×844 与电脑 1280×800，亮 / 深色） | 332 张，除预期差异（设置页新增的版本卡片、学习页退出确认框跟随深色主题）外像素差异 ≤ 0.3% |
| 迁移演练（正式数据拷贝导入后，对每个账号请求全部只读接口，比较新旧两版完整 JSON） | 399 个账号、61,309 组请求；49 类请求中 48 类零差异，剩下的 `usedBy` 排序差异来自旧库查询本身没有排序（PostgreSQL 顺序不稳定） |
| 交叉编译 | `vinx-vocab.exe`（windows/amd64，带图标与版本信息）与 `vinx-vocab`（linux/amd64）各约 19MB |

### 与方案的差异

- **JS 体积未达到 200KB 目标**：实际 341.9KB（未压缩，37 个文件，首屏约 124KB）；旧版约 1798KB（首屏约 1627KB），小约 81%。继续压缩需要删减移植过来的页面代码，所以接受这个结果。
- FSRS 没有用 `go-fsrs`：它的排程结果与 ts-fsrs 不一致，改为在 `internal/core/fsrs` 逐行移植 ts-fsrs 5.4.2 的长期排程路径。
- 全新安装的第一个账号是管理员，全新库忽略 `SIGNUP_ENABLED=false`（K44）；`SETTINGS_SECRET` 不再退回 `JWT_SECRET`（K45）；降级为个人版后学生照常学已安排的计划（K46）。
- `COOKIE_SECURE=auto` 按请求本身是否 HTTPS 判断（旧版按 `NODE_ENV`）；写请求的来源检查默认放行同主机，局域网访问不需要配置白名单。
- Windows 双击运行时，启动失败会等待按回车再关窗口；在已有命令行窗口里运行时不等待。
- 学习页退出确认框里按 Enter 与旧版一样是翻下一张卡，不是确认退出（保持旧版行为）。

### 未验证

- 真实 Windows 上的双击运行、防火墙与 SmartScreen 提示、关闭控制台窗口、失败时按回车关闭：只做了交叉编译和代码阅读。

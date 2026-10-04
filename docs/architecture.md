# 架构与约定（Go 单文件版）

本文件是后端各任务的共同约定。重写阶段以旧版（Node + Fastify + Prisma + PostgreSQL，下称 **oracle**）为规格：路径、方法、请求字段、响应包络、字段名、`null` 与缺省、时间格式、错误码与中文 message、HTTP 状态码都与旧版一致，需求背景见 [spec 0001](specs/0001-go-single-binary.md)。重写完成后**以单文件版为准**（[ADR 0004](architecture-decisions/0004-single-binary-is-source-of-truth.md)）：已有接口保持上述约定不变，新功能按 `docs/specs/` 设计，只增字段、不改已有字段的含义。

## 目录与包职责

```
cmd/vinx-vocab/       main：子命令 serve（默认）/ seed-demo / import / audio / version；端口占用与重复启动处理
assets.go             根包 vinxvocab：go:embed data/vocab/*.txt（go:embed 不能引用上级目录，所以放根目录）
data/vocab/           内置词表（首次启动导入为系统词书）
internal/
  config/             配置：命令行 > 环境变量 > 数据目录的 config.toml > 默认值；数据目录；secret.key
  store/              SQLite：连接参数、嵌入式迁移、事务、id、时间 / JSON 列类型
    migrations/       NNNN_名称.sql，按文件名顺序执行
  httpx/              包络、错误码、请求体解析（Fastify 语义）、zod 风格校验、requestId
  auth/               JWT Cookie、bcrypt、身份解析与守卫
  core/               纯业务规则（旧 apps/api/src/lib 与 packages/shared），不触库，全部表驱动单测
    vocabparser/      词表文本解析
  service/            装配与事务（旧 services/）；access.go 集中数据范围判定
  school/             班级版专属逻辑（旧 src/school）
  api/                路由与处理函数（旧 routes/）：每个模块一个文件
  seed/               内置词书导入、seed-demo 演示数据
  migrate/            从旧版 PostgreSQL 导入（import 子命令）
  web/                go:embed 前端产物（dist/）与 SPA 回退
contract/             黑盒契约测试（vitest），见 contract/README.md
```

依赖方向：`api → service / school / auth → core / store / httpx`；`core` 不依赖任何内部包；`httpx` 不依赖 `store`。

## 启动流程（serve）

1. `config.Load`：解析数据目录（`--data` > `VINX_DATA_DIR` > 默认：Windows 为 exe 旁的 `vinx-data\`，其他系统为 `./data`），创建并检查可写；读 `config.toml`；读或生成 `secret.key`。
2. 监听 `host:port`（默认 `0.0.0.0:3000`）。端口被占用时探测 `http://127.0.0.1:<port>/api/health`：响应带 `X-Vinx-Vocab` 头说明是本程序已在运行 → 打印地址后退出 0（Windows 下打开浏览器）；否则提示换端口并退出 1。
3. `store.Open`：打开数据库，执行待执行的迁移（有待执行迁移且库里已有迁移记录时，先 `VACUUM INTO` 备份为 `vinx.db.bak-<时间>`，保留最近 3 份）。
4. `seed.FirstRunBooks`：库里还没有任何词书时导入内置词书（无所有者）。
5. `api.NewDeps` + `api.Handler` 起 HTTP 服务；打印本机地址、局域网地址、数据目录。

`seed-demo` 额外创建演示账号（`admin` / `teacher` / `student` / `student2@vinx.test`，密码 `dev123456`）、系统词书（所有者为演示管理员）、演示班级 `DEMO01` 与计划「八上 Unit 1–2 每日背词」，与旧 `prisma/seed.ts` 相同，可重跑。导入结果与旧 seed 逐行一致（`internal/seed/testdata/oracle-seed.tsv`）。

## 配置（internal/config）

- 环境变量名沿用旧版：`VINX_EDITION`、`APP_TIMEZONE`、`JWT_SECRET`、`JWT_EXPIRES_IN`、`SETTINGS_SECRET`、`COOKIE_SECURE`、`ALLOWED_ORIGINS`、`SIGNUP_ENABLED`、`AUDIO_DIR`、`AUDIO_PROVIDER_URL`、`AI_PROVIDER`、`AI_API_KEY`、`AI_BASE_URL`、`AI_MODEL`、`AI_TIMEOUT_MS`；新增 `PORT`、`HOST`、`VINX_DATA_DIR`。
- `config.toml` 的键名与环境变量相同（如 `VINX_EDITION = "school"`，`PORT = 3000`）。
- `JWT_SECRET` / `SETTINGS_SECRET` 未给出时取 `secret.key`（TOML：`jwt_secret`、`settings_secret`，首次生成，权限 0600）。与旧版不同：`SETTINGS_SECRET` 不再退回 `JWT_SECRET`。
- `Config` 只放启动时确定的值；运行中可改的设置（版本选择、AI 配置、提示词）存 `AppSetting` 表。
- `Config.Edition` 为空串表示未锁定版本。

## 数据库（internal/store）

- 表名、列名、字符串 id 与 Prisma 一致；SQL 里一律加双引号：`SELECT "passwordHash" FROM "User"`。注意 `"Plan"."order"` 是保留字。
- 类型：id / 字符串 / 枚举 → TEXT；Int → INTEGER；Float → REAL；Boolean → INTEGER 0/1（`database/sql` 直接扫描进 `bool`，写入传 `bool`）；DateTime → TEXT（UTC 毫秒 RFC3339，`2026-09-28T05:51:26.656Z`）；`Json` 与 `String[]` → JSON TEXT。
- 列类型包装：
  - `store.Time` / `store.NullTime`：扫描 TEXT、写入格式化文本、JSON 输出 ISO 毫秒串（`NullTime` 无效时输出 `null`）。
  - `store.JSON[T]`：Json / 数组列；`Null=true` ↔ SQL NULL ↔ JSON `null`。
  - `store.FormatTime(t)` / `store.ParseTime(s)`。
- 写入时间一律用 `store.NewTime(d.Now())`（经可注入时钟），不要依赖列默认值（默认值只是兜底）。`@updatedAt` 列由应用在每次 UPDATE 时写入。
- **SQL 参数里的时间**：`store.Open` 的连接包装（`internal/store/driver.go`）会把 `time.Time`、`*time.Time`、`sql.NullTime` 以及任何 `Value()` 返回 `time.Time` 的参数统一转成规范文本（`FormatTime`），所以 `WHERE "due" <= ?` 直接传 `d.Now()` 就是正确的字典序比较。只有经 `store.Open` 打开的库有这层转换；自己 `sql.Open("sqlite", …)` 的连接没有，不要用。读时间列请扫描进 `store.Time` / `store.NullTime`（扫描进 `time.Time` 会报错）。
- **JSON 列的空值**：`store.JSON[T]` 的 `V` 为 nil 切片 / nil map（且 `Null=false`）时，写库与 JSON 输出都规整为 `[]` / `{}`（旧版这些列永远输出数组），只有 `Null=true` 才是 SQL NULL / JSON `null`。规整只作用于顶层，自己拼的嵌套结构里的切片请初始化为空切片。
- 事务：`db.Tx(ctx, func(tx *sql.Tx) error)`。连接串设了 `_txlock=immediate`，所有 `BeginTx` 都是 `BEGIN IMMEDIATE`（替代 PostgreSQL 的 `FOR UPDATE`，写事务开始即拿写锁）；`busy_timeout` 10 秒；WAL；`foreign_keys=ON`（级联删除与 Prisma 的 `onDelete` 一致）。
- 服务层函数以 `store.Querier` 为参数（`*store.DB`、`*sql.DB`、`*sql.Tx` 都满足），事务内外通用。
- **⚠ 事务回调里只能用 `tx`**：`d.DB.Tx(ctx, func(tx *sql.Tx) error { … })` 里所有读写（包括传给服务层函数的 `Querier`）都必须是 `tx`，不能是 `d.DB`，也不能在回调里再开 `Tx`。原因：`BEGIN IMMEDIATE` 已持有写锁，经 `d.DB`（池里另一条连接）写库会等满 `busy_timeout`（10 秒）后 `SQLITE_BUSY`；读库则看不到本事务未提交的写入。防护：API 路由给每个请求的 ctx 挂了 `store.WithTxGuard`，回调期间用同一个 ctx 调 `d.DB.ExecContext / QueryContext / QueryRowContext / Tx` 会立即 panic（路由 recover 成 500 并记日志）。只检查带 ctx 的方法，所以一律用 `…Context` 版本；命令行 / seed 的 ctx 没挂防护。
- `_txlock=immediate` 让所有事务（包括只读事务）都拿写锁、互相串行；只读查询不要包在 `Tx` 里，直接用 `d.DB`。
- id：`store.NewID()` 生成 cuid 风格（`c` + 24 位小写 base36）。
- 唯一冲突：`store.IsUniqueViolation(err)`；未处理的唯一冲突由 `httpx.Fail` 统一转为 409 `DUPLICATE`「数据已存在，唯一性冲突」（对应 Prisma P2002）。
- `StudySession_active_uniq`：`UNIQUE ("userId", COALESCE("planId", ''), "kind") WHERE "status" = 'active'`，并发开组靠它 + 续做。
- 与 PostgreSQL 的差异要自己处理：`LIKE` 对 ASCII 不区分大小写（Prisma 的 `contains` + `insensitive` 可以直接用 `LIKE`，区分大小写的 `startsWith` 用 `substr(x, 1, n) = ?`）；`ORDER BY` 文本按字节序（旧库按排序规则）；`IN (...)` 用 `store.Placeholders(n)` + `store.Args(xs)`。
- 迁移：新增 `internal/store/migrations/NNNN_名称.sql`，不修改已发布的迁移文件。每个迁移在一个开着 `foreign_keys=ON` 的事务里执行（事务内无法关闭外键），所以**不要**用 SQLite 常见的「建新表 → 拷数据 → DROP 旧表 → 改名」改表方式：DROP 父表会触发 `ON DELETE CASCADE` 清空子表。需要这类改表时先扩展迁移执行器（事务外 `PRAGMA foreign_keys=OFF`，迁移后 `PRAGMA foreign_key_check`）。

## HTTP（internal/httpx、internal/api）

### 挂载与路由

- `api.Handler(d)` 是整个程序的入口：`/api` 与 `/api/*` 去掉 `/api` 前缀后交给 API 路由（路径与旧版一一对应），其余路径交给前端（`internal/web`，存在的文件原样返回，`/assets/` 下带哈希的文件长期缓存（`immutable`），带扩展名但不存在 → 404，其余 → `index.html`（`no-cache`））。`internal/web/dist` 由 `make build-web` 从 `web/dist` 复制，只有占位页 `placeholder.html` 入库（没构建前端时回落到它）。注意：前端路由的最后一段如果含 `.`（例如把拼写或邮箱放进路径）会被当成静态文件而 404；旧前端路由里的参数都是 id，新增路由时避免这种路径，或在 `web.Handler` 里加例外。
- 新模块：建 `internal/api/<area>.go`，写 `func registerXxx(r *Router, d *Deps)`，在 `router.go` 的 `modules` 列表加一行。
- 注册：`r.Get / Post / Put / Patch / Delete(path, handler, 守卫...)`，路径用 Go 1.22 `ServeMux` 语法（`/books/{id}`），处理函数里 `req.PathValue("id")`。
- 班级版专属路由：`r.Feature(core.FeatureClasses).Get(...)`。功能开关**每个请求**按当前版本判定（`d.Edition(ctx)`：`VINX_EDITION` > `AppSetting["edition"]` > 默认 school），版本切换后下一个请求即生效；关闭时返回 404「接口不存在」，与旧版「个人版不注册路由」表现相同。
- 每条路由的处理顺序与 Fastify 一致：
  1. `httpx.Prepare`（先于路由匹配）：分配 `requestId`（`req-<36 进制序号>`）；非 GET/HEAD 读请求体：`application/json` 为空 → 400、非法 JSON → 400，`text/plain` 按字符串，其他类型 → 415，超过 1 MiB → 413（code 均为 `VALIDATION`，message 为 Fastify 英文原文）；
  2. 路由匹配：不存在、方法不对、多斜杠、不规整路径 → 404 `NOT_FOUND`「接口不存在」（不是 405）；HEAD 走 GET 处理函数；
  3. 功能开关；
  4. Origin 检查（写请求带 `Origin` 头时，只放行同主机或 `ALLOWED_ORIGINS` 里的来源，含 `*` 不限；没有 `Origin` 头放行）→ 403「非法请求来源」；
  5. 身份解析（`d.Auth.Authenticate`，不拒绝请求）；
  6. 守卫（`auth.RequireAuth`、`auth.RequireCap(...)`）；
  7. 处理函数。

### 处理函数

```go
func registerBooks(r *Router, d *Deps) {
	r.Get("/books/{id}", func(w http.ResponseWriter, req *http.Request) error {
		actor := auth.ActorFrom(req.Context()) // 挂了守卫就一定非 nil
		...
		httpx.OK(w, view)                        // 200 { success, data, timestamp }
		return nil
	}, auth.RequireCap(core.CapBooksRead))
}
```

- 返回错误即可，路由统一交给 `httpx.Fail` 输出错误包络：`*httpx.Error` 原样输出；唯一冲突 → 409；其余 → 500 `SERVER`「服务器内部错误」并记日志。
- 业务错误：`httpx.NewError(code, message, details)`，HTTP 状态按错误码表（`httpx.ErrorHTTPStatus`，照抄 `packages/shared/src/errors.ts`）；需要别的状态时 `.WithStatus(n)`。常用：`httpx.NotFound(msg)`、`httpx.Forbidden(msg)`、`httpx.Validation(msg)`、`httpx.Unauthorized(msg)`。
- 成功：`httpx.OK(w, data)`；`data` 为 nil 时包络里没有 `data`（旧 `ok()`；旧版 `ok(null)` 输出 `"data": null` 的地方用 `httpx.OK(w, json.RawMessage("null"))`）；`httpx.Created(w, data)` 为 201。列表：`httpx.List(items)` → `{items, total}`，`httpx.Page(items, total, page, limit)` → `{items, total, page, limit}`（`httpx.Paginated[T]`）。nil 切片会输出 `[]`。
- JSON 输出不转义 `<>&`（与 `JSON.stringify` 一致）。
- 包络的 `timestamp` 用包级 `httpx.Now`（真实时间），不经 `Deps.Clock`；固定时钟测试里不要断言 `timestamp` 的值。
- 处理函数 panic 由路由 recover，输出 500 `SERVER` 包络并记日志（含堆栈）。

### 请求体与校验（zod 语义）

`httpx.Decode[T](req)` 对应旧 `parseBody(schema, body)`：

- 无请求体或 `null` 视为 `{}`；根不是对象 → `details.body = "Expected object, received <类型>"`；
- 结构体逐字段解码，类型不符记 `Expected <期望>, received <实际>`，其余字段继续；与 zod 一致，出现类型错误的路径（及其子路径）之后不再记其他问题（`Validate` 里对同一路径的 `v.Add` 被忽略，例如 `units:"bad"` 只报 `Expected array, received string`）；`Opt[[]结构体]` 逐个元素递归解码，元素不是对象记 `<路径>.<i>: Expected object, received <类型>`；
- `*T` 实现 `Validate(v *httpx.V)` 时调用它；
- 有任何问题返回 400 `VALIDATION`「参数校验失败」+ 字段级 `details`（同一路径后写覆盖前写）。

字段用 `httpx.Opt[T]` 区分「缺省 / null / 有值」，在 `Validate` 里用 `V` 的方法取值并校验，默认消息与 zod v3 英文原文一致：

```go
type profileBody struct {
	Name         httpx.Opt[string] `json:"name"`
	CurrentGrade httpx.Opt[string] `json:"currentGrade"`
	name         *string
	currentGrade httpx.Opt[string]
}

func (b *profileBody) Validate(v *httpx.V) {
	b.name = v.OptStr("name", b.Name, httpx.Min(1), httpx.Max(50, "姓名过长"))  // .optional()
	b.currentGrade = v.NullStr("currentGrade", b.CurrentGrade, httpx.Max(20))  // .optional().nullable()
}
```

- 字符串：`v.Str`（必填，缺省 → `Required`，null → `Expected string, received null`）、`v.OptStr`、`v.NullStr`；检查 / 变换按书写顺序执行：`httpx.Trim()`、`Lower()`、`Upper()`、`Min(n, msg?)`、`Max(n, msg?)`、`Email(msg?)`、`Regex(re, msg?)`、`Refine(fn, msg)`。长度按 JS 的 UTF-16 码元计。
- 枚举：`v.Enum(path, o, values, msg)`、`v.OptEnum(path, o, values, msg, def)`（`msg` 为空用 zod 默认消息）。
- 数字：`v.Int(path, o Opt[float64], def *int, httpx.Between(min, max))`；布尔：`v.OptBool`；数组长度：`v.ArrayLen`。需要更多形状时在 `httpx/validate.go` 里补，保持 zod 默认消息。
- 查询参数（旧 `parseQuery`）：`httpx.DecodeQuery[T](req)`，字段一律声明为 `Opt[string]`，数字用 `v.CoerceInt(path, o, def, rng)`（`z.coerce.number()` 语义：空串为 0，非数字 → `Expected number, received nan`）；未声明的参数忽略（旧 `.passthrough()`）。

## 认证（internal/auth）

- JWT HS256，载荷 `{sub, email, name, role, iat, exp}`，有效期 `JWT_EXPIRES_IN`（默认 7d）；Cookie `vinx_token`：`HttpOnly`、`SameSite=Lax`、`Path=/`、`Max-Age=604800`；`Secure` 按 `COOKIE_SECURE`（`auto` 时只有 TLS 请求带）。**与旧版的有意偏差**：旧版 `auto` 跟随 `NODE_ENV=production`；单文件多在本机 / 局域网 http 下运行，所以改为看请求本身是否 TLS。放在做 TLS 终止的反向代理后面、又需要 `Secure` 时，设 `COOKIE_SECURE=true`。也接受 `Authorization: Bearer <token>`（优先于 Cookie）。
- 身份来自令牌（`service.Actor{ID, Role, Name}`），不查库；改角色后需重新登录才生效（与旧版一致）。
- 守卫：`auth.RequireAuth`（登录即可）、`auth.RequireCap(caps...)`（任一能力，否则 403「无权访问」）。处理函数里 `auth.ActorFrom(ctx)`（挂了守卫才保证非 nil）；没挂守卫又需要登录者时用 `auth.MustActor(ctx)`（未登录返回与守卫相同的错误，对应旧 `getActor`）。
- 令牌错误的响应照旧后端实际输出：没带令牌 → 401 `UNAUTHORIZED`「未登录或登录已失效」；无效 / 过期 → 401 但 `code` 为 `VALIDATION`、message 为 @fastify/jwt 英文原文；`Bearer` 格式不对 → 400。
- 密码：`auth.HashPassword`（bcrypt 代价 12）、`auth.VerifyPassword`（兼容旧 bcryptjs 的 `$2a$` / `$2b$` 哈希）。密码策略 `core.ValidatePassword`（至少 6 位）。

## 权限与数据范围

- 能力表：`core.RoleCapabilities`（照抄 `packages/shared/src/access.ts`，顺序即下发给前端的顺序），`core.RoleCan`、`core.CapabilitiesOf`。
- 数据范围全部集中在 `internal/service/access.go`：`AssertCan`、`ManageableClassIDs`（管理员返回 nil 表示不限）、`AssertCanManageClass`、`IsStudentOfTeacher`、`AssertCanViewUser`、`VisibleBookFilter`（返回可拼进 WHERE 的片段与参数）、`AssertCanEditBook`、`IsLearnerOnly`。新增的数据范围规则也写在这里。
- **版本对能力与数据范围的影响**：路由在身份解析后把本次请求的版本写进 `Actor.Edition`。
  - `service.Can(a, cap)` = 角色具备该能力 **且** `core.EditionAllows(edition, cap)`：个人版下 `classes`、`users`、`plans.assign`、`students.view` 视为没有（`RequireCap` 与 `AssertCan` 都经它）。`/auth/me` 下发的 `capabilities` 仍按角色给出（与旧版个人版一致），前端按 `/config` 的 `features` 隐藏入口。
  - 「看全部数据」一律用 `service.SeesAll(a)`（班级版的管理员），不要用 `IsAdmin`：个人版下管理员也只看自己的数据（从班级版降级后库里可能仍有多个账号）。`IsAdmin` 只用于管理权限本身（系统词书标记、代管账号等）。
  - 只拿到 `userID`、没有 `Actor` 的学习流（今日队列 `LoadPlansForLearners`、开组）要知道版本时，读路由用 `service.WithEdition` 挂在请求 ctx 上的版本（没挂按班级版处理）。目前只用于班级的自主开关（spec 0008：个人版不看班级，相当于永远允许）。
  - 个人版的数据范围：计划 = 自己创建的 + 安排给自己的（含所在班级的，降级前布置的计划照常学）；词书 = 系统 + 自己的 + 所在班级老师的；学习记录只能看自己；管理员可编辑系统词书与自己的词书，不能编辑他人的。

## 版本（edition）

- `core.Features` 与旧 `featuresOf` 一致；`service.Editions.Current(ctx)` 返回 `EditionState{Edition, Locked, Chosen}`：`VINX_EDITION` 给出时 `Locked`；否则读 `AppSetting` 的 `edition` 行（JSON 字符串），没有时默认 school、`Chosen=false`。
- `GET /config` 比旧版多两个字段：`editionLocked`（= `Locked`）与 `needsSetup`。
- **`needsSetup` = 未锁定 且 未保存过选择 且 库里还没有任何账号**（`service.NeedsSetup`）。全新安装先进一步向导；已有账号的库（导入的旧数据、`seed-demo`）不进向导，未保存选择时按班级版运行（旧版默认值），管理员可在系统设置里切换。
- `POST /setup/edition {edition}`：无需登录，仅 `needsSetup` 时可用（事务内重新判断，并发只成功一次），否则 403「已完成初始设置，如需切换版本请由管理员在系统设置里操作」；锁定时 403「版本由 VINX_EDITION 指定，不能在界面上切换」。
- `PUT /settings/edition {edition}`：需 `system` 能力；锁定时 403（同上）。双向切换，写 `AppSetting`，下一个请求即生效（功能开关与 `Actor.Edition` 都按请求判定）。降级不删任何数据：班级 / 用户管理路由 404、注册关闭、依赖版本的能力关闭、数据范围收窄为本人（见上「权限与数据范围」）；升级原样恢复。两个接口都返回切换后的 `/config` 数据。
- 注册：`service.SignupEnabled`（个人版只允许一个账号；班级版可用 `SIGNUP_ENABLED` 关闭）。注册事务内重新数账号：个人版并发注册只放行一个；**库里还没有任何账号时，第一个注册的账号为管理员（两个版本都是）**——与旧版的有意偏差：旧版班级版靠 seed 建管理员，单文件全新安装没有 seed，否则无人能建老师账号。

## 时间与学习日

- 处理函数与服务层一律用 `d.Now()`（`Deps.Clock` 可注入，测试用固定时钟覆盖跨午夜）。
- 学习日：`core.DayKeyOf(t, d.Cfg.Location)`，`APP_TIMEZONE` 默认 `Asia/Shanghai`；程序内嵌 `time/tzdata`，Windows 上也能用。

## 学习流（今日 / 学习组 / FSRS / 记录）

- FSRS：`internal/core/fsrs` 是 ts-fsrs 5.4.2 长期排程路径（`enable_short_term=false`、无 fuzz、默认权重）的逐行移植，
  逐步保留 ts-fsrs 的 `roundTo(x, 8)` 与 JS `Math.round` 语义；`testdata/fixtures.json`（`contract/scripts/fsrs-fixtures.mjs` 生成）
  回放结果与 ts-fsrs 逐位一致。业务封装（`ApplyRating`、`CapDue`、`DeriveRating`）在 `core/scheduler.go`。
- 随机：出题、随机顺序、检测抽词经 `core.Rng`；`Deps.Rand` 默认 `core.DefaultRng`，测试注入 `core.SeededRng(n)`（与旧 mulberry32 一致）。
- 服务层入口（`internal/service/learning.go`，单词单的 `kind = "sheet"` 组走同一套）：`StartSession`（按 kind 分派，含单词单）、
  `RecordAnswer`、`SaveProgress`、`DiscardSession`、`CompleteSession` / `CompleteSessionTx`（事务内结算，K22 删除计划时用）、
  `BuildSnapshot`、`ParseSnapshot`、`ComputeToday`、`DrillCandidates`、`NextSheet`；记录在 `records.go`。
- 学习日：开组记 `dayKey`（开组那天），作答记作答当天，结算记 `completedDay` 与 `ReviewLog.dayKey`（结算当天）；新学词的
  `introducedDay` 取开组那天，到期封顶到「结算日的下一个学习日 00:00」。与旧版相同。

## 从 PostgreSQL 导入（internal/migrate）

`vinx-vocab import --from-postgres <URL> [--settings-secret <旧密钥>] [--force] [--edition school|personal] [--data 目录]`

- 源库密码建议用环境变量 `PGPASSWORD` 或 `~/.pgpass` 提供（连接串里省略密码），不写进命令行；旧密钥用 `VINX_IMPORT_SETTINGS_SECRET`。Prisma 风格的 `?schema=` 参数会转成 `search_path`。
- 先停掉本程序（导入持有写锁，运行中的服务也会缓存旧的 AI 配置；程序不检测），用与之后 `serve` 相同的数据目录运行。
- 源库只读：连接参数 `default_transaction_read_only=on`，所有表在同一个 `REPEATABLE READ READ ONLY` 事务里读，旧服务不停也能拿到一致的快照。
- 目标库一个 SQLite 事务：清空 17 张表（包括首次启动自动导入的内置词书，它们的 id 与旧库不同）→ 按父表到子表的顺序插入 → 逐表核对行数（源库 / 读出 / 目标库）与外键 → 提交；任何一步失败或行数不一致都整体回滚，退出码 1。
- 每张表按源库物理顺序（`ORDER BY ctid`）插入，SQLite 的 `rowid` 即插入顺序，依赖 `rowid` 近似旧库自然顺序的查询（如单词的 `usedBy`）与旧库一致。
- 类型：时间 → `FormatTime`；Boolean → 0/1；`String[]` → JSON 数组；`Json` → JSON 文本（只去掉 PostgreSQL 输出的空白，键顺序、数字写法、旧快照缺的字段都原样保留）。源库的每一列本版本都必须有（源库有不认识的列时停止，不丢数据）；本版本新增的可空或有默认值的列源库没有也可以，取默认值；本版本的必填列源库缺少时停止。
- 「目标库已有数据」= 有账号，或有非内置词书（自建或有所有者的词书）；此时不加 `--force` 拒绝，加了先 `VACUUM INTO <库>.before-import-<时间>` 备份（不参与迁移备份的轮换清理）再覆盖。
- AI Key：`--settings-secret`（或环境变量 `VINX_IMPORT_SETTINGS_SECRET`）是**旧实例实际生效的密钥**——旧版设了 `SETTINGS_SECRET` 就是它，没设则是旧的 `JWT_SECRET`（旧版会退回）。解密后用本实例**数据目录里保存的**密钥重新加密（`config.toml` 的 `SETTINGS_SECRET`，没有则 `secret.key`），不取导入时的 `SETTINGS_SECRET` 环境变量（设了且不同会提示已忽略），保证之后不设该环境变量的 `serve` 一定能解开；服务若另设 `SETTINGS_SECRET` 环境变量，需在系统设置里重新填写 Key。旧密钥只在内存里使用。没给时其余照常导入、Key 置空并提示管理员重新填写；给了但解不开则失败回滚（不回显密钥）。
- 版本：写入 `AppSetting["edition"]`（`--edition` 优先，其次源库已有的值，否则 school），导入后 `needsSetup=false`。
- 导入后所有人需要重新登录一次（JWT 密钥不同），密码不变；真人发音缓存可把旧部署的 `audio/` 复制到数据目录。
- 演练：`contract/scripts/diff-readonly.ts` 对每个账号 GET 全部只读接口，比较 oracle 与导入后的 Go 的完整 JSON（见脚本头部说明）。

## 测试

- `internal/core`：表驱动单测，用例逐条翻译旧 `apps/api/tests/lib/*.test.ts`。
- 服务与接口：`internal/api/api_test.go` 的 `newEnv(t, env)` 夹具（临时库 + 固定时钟 + `Handler`），`e.do(method, path, body, headers)`。
- 契约：`contract/`。重写阶段的用例先对 oracle 跑绿再对 Go 跑绿，现在作为回归测试保留；新功能的用例只对 Go 运行（ADR 0004）。新模块在 `contract/lib/areas.ts` 的 `GO_AREAS` 登记后才会在 Go 上启用。
- 导入测试（`internal/migrate`）需要 PostgreSQL：`VINX_TEST_PG_URL=postgresql://…/postgres make test-import`（角色要能 CREATE DATABASE，每个测试建临时库并删除；缺变量时失败而不是跳过）。`make test` 在设了该变量时一并运行，没设时跳过并在末尾打印提示。
- 命令：`make test`（Go 单测）、`make contract`、`make build`（构建前端并嵌入，产出本机 `dist/vinx-vocab`）、`make cross`（`dist/vinx-vocab.exe` windows/amd64 与 `dist/vinx-vocab` linux/amd64，`CGO_ENABLED=0`，版本号经 `-ldflags -X main.version` 注入）、`make e2e`（构建后用 `e2e/run.sh` 起两个实例跑 Playwright，见 `e2e/README.md`）。
- Windows 资源：`make winres` 用 go-winres（`go run github.com/tc-hib/go-winres@v0.3.3`，首次运行从 Go 模块代理下载）把 `cmd/vinx-vocab/winres/`（`icon.png`、`winres.json`）生成 `cmd/vinx-vocab/rsrc_windows_amd64.syso`（不入库，只在 windows 构建时链接）：exe 图标、文件版本（`VERSION` 里的 x.y.z，否则 0.0.0）与产品版本字符串（`VERSION` 原样）。图标由同目录 `gen_icon.go`（`go run gen_icon.go`）生成。

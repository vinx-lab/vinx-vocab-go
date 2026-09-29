# API 契约测试

同一套纯 HTTP 黑盒用例，先在 **oracle**（旧 Node 后端）上跑绿，证明用例本身正确；再在 **Go 单文件**上跑绿，证明接口一致。

## 运行

```bash
pnpm -C contract install

# oracle（旧后端，路径没有 /api 前缀）
BASE_URL=http://localhost:4100 pnpm -C contract test

# Go 单文件（API 挂在 /api 下）
BASE_URL=http://localhost:3200/api pnpm -C contract test

# 另起一个个人版实例时，顺带验证个人版
BASE_URL=... PERSONAL_BASE_URL=http://localhost:3201/api pnpm -C contract test
```

- **只设 `BASE_URL`，一个变量**：以 `/api` 结尾视为 Go（`TARGET=go`），否则视为 oracle。目标解析集中在
  `lib/target.ts`，被 `vitest.config.ts`、`lib/global-setup.ts`、`lib/client.ts` 三处共用，不要在别处重新判断。
- `TARGET=go|oracle` 可以显式指定，但必须和 `BASE_URL` 是否以 `/api` 结尾推断出的目标一致，矛盾时
  **配置加载阶段直接报错**、不会开始跑任何用例（例：`BASE_URL=http://localhost:4100 TARGET=go` 会因为
  "URL 没有 /api 但 TARGET 说是 go" 而中止）。
- 用例本身读的内部变量名是 `CONTRACT_BASE_URL`（因为 Vite 会把自己的 `import.meta.env.BASE_URL`，固定值
  `"/"`，写进测试进程的 `process.env.BASE_URL`，所以真正的 `BASE_URL` 必须在配置加载的主进程里读一次、
  显式透传）。**直接设 `CONTRACT_BASE_URL` 也可以**（会被当作 `BASE_URL` 的等价写法采纳），但如果两个都设
  且不一样，同样在配置加载阶段直接报错，不会有一个悄悄覆盖另一个的情况——这是早期踩过的坑：
  早期版本里 `vitest.config.ts` 会用算出来的默认值（oracle）无条件覆盖 `test.env.CONTRACT_BASE_URL`，
  直接设 `CONTRACT_BASE_URL=<go 地址>` 会被默默吃掉、实际全部打到 oracle，输出却"全绿"，看不出测错了实例。
- 跑任何用例之前，`globalSetup`（`lib/global-setup.ts`）还会做一次活体探测：请求 `${BASE_URL}/health`，
  用 Go 版独有的 `X-Vinx-Vocab` 响应头（见 `internal/api/health.go`）核对"连上的服务器是不是它自称的那个"——
  连不上、状态码不对、或者头的有无和 `TARGET` 对不上，都会在第一条用例跑之前就中止并打印清楚的原因。
  这一步防的是"配置本身自洽，但服务器答非所问"（比如端口写错、连到了另一个残留实例）。
- 用例依赖 seed 数据（`admin` / `teacher` / `student` / `student2@vinx.test`，密码 `dev123456`；演示班级邀请码 `DEMO01`）。Go 端先 `vinx-vocab seed-demo --data <目录>`。
- 用例会留下带时间戳的测试账号与班级；需要干净的 oracle 时运行 `contract/scripts/oracle-reset.sh`（只重建 `vinx_oracle` 库，然后重启 tmux `vinx-oracle` 里的旧后端）。

## 目录

| 文件 | 作用 |
|---|---|
| `lib/target.ts` | `resolveTarget()`：合并 `BASE_URL` / `CONTRACT_BASE_URL` / `TARGET` 三个环境变量、校验一致性（冲突抛错），唯一真相来源 |
| `lib/global-setup.ts` | Vitest `globalSetup`：打印目标、探测 `${BASE_URL}/health`（核对 `X-Vinx-Vocab` 头与 `TARGET` 相符），不符就在跑任何用例之前中止 |
| `lib/client.ts` | `Client`（自带 cookie jar）、`anon()`、`login(email, pwd)`、`call(client, method, url, body)` → `{ status, body }` |
| `lib/shape.ts` | `shapeOf(value)` 把 JSON 转成结构描述（类型、null、数组元素结构、对象键集合，UTC 毫秒时间串记为 `datetime`）；`expectShape(name, value)` 在 oracle 上写 `shapes/<name>.json`，在 Go 上比对 |
| `lib/areas.ts` | 按模块分批移植：`GO_AREAS` 列出 Go 已实现的模块，`ready(...areas)` 为假时（仅 Go）跳过对应用例；`goOnly` 标记只在 Go 上跑的用例 |
| `specs/*.spec.ts` | 用例，由旧 `apps/api/tests/*.test.ts` 改写 |
| `shapes/` | oracle 录下的响应结构快照（跑 oracle 时自动刷新，入库） |
| `scripts/oracle-reset.sh` | 重建 oracle 库并 seed |
| `scripts/diff-readonly.ts` | 迁移演练：oracle 与「导入了同一份数据的 Go」逐账号比对全部只读接口的完整 JSON（用法见文件头）。导入本身的 Go 测试需要 PostgreSQL：`VINX_TEST_PG_URL=postgresql://…/postgres make test-import` |
| `scripts/fsrs-fixtures.mjs` | 用旧仓库依赖里的 ts-fsrs 5.4.2 生成 FSRS 对照数据 `internal/core/fsrs/testdata/fixtures.json`（Go 单测逐步比对）；参数：旧仓库目录，默认与本仓库同级的 `vinx-vocab` |
| `lib/study.ts` | 学习流用例的助手：注册新学生、取 seed 系统词书单元、按快照作答 |

后续任务实现一个模块后：写 `specs/<area>.spec.ts` → oracle 跑绿（生成结构快照）→ 实现 Go → 把模块名加进 `lib/areas.ts` 的 `GO_AREAS` → Go 跑绿。

## 与旧测试的差异（改成黑盒后）

- `health`、`auth.flow`：逐条保留，另补了请求体解析边界（空 JSON、非法 JSON、415、text/plain、zod 默认消息）、令牌错误、修改密码、个人资料、各角色能力清单。
- `edition`：旧测试在同一进程里分别以两个版本构建实例；黑盒只能测正在运行的实例，个人版断言需要另起实例并设 `PERSONAL_BASE_URL`。
- `security.flow`：旧测试直接调用服务函数（`startSession` / `recordAnswer` / `completeSession`）并直接查库，这里改为学生本人走 HTTP：
  - 「批量建号写入了 `createdById`」→ 观察「本班老师能重置该账号密码、`managedByMe` 为真」；
  - 「重考后 `ReviewLog` 条数不变」→ 观察重考那一组 `/records/sessions/:id` 里每个词的 `review` 都是 `null`；
  - 检测组答题时拿不到标准答案，按 `options[0]` 作答（该断言只关心重考不产生复习记录，与对错无关）。
- 令牌无效时旧后端返回 HTTP 401，但 `code` 是 `VALIDATION`、`message` 是 @fastify/jwt 的英文原文（旧 error-handler 的映射表没覆盖 @fastify/jwt 的错误码）。用例按旧后端的实际输出断言，Go 照原样输出。
- Origin 检查：oracle 以开发模式运行、不做 Origin 检查，这部分用 `goOnly` 只在 Go 上验证（外站 Origin 的写请求 403「非法请求来源」，无 Origin 与同主机放行）。

- `learning` / `dont-know`（由旧 `learning.flow`、`dont-know.flow` 改写）：新注册的学生给自己建计划（seed 系统词书），不依赖班级与建号接口；
  检测组进行中拿不到标准答案，用单词历史接口查已学词的拼写与释义后作答；出题与选词含随机，只断言不变量。
  黑盒无法改被测实例的时钟：跨学习日的复习、上海 00:00 前后的学习日边界、K19 由 Go 的 `internal/service/learning_test.go`（固定时钟）覆盖。

- `classes` / `users`（旧版没有对应的流程测试，按 `school/classes.routes.ts`、`routes/users.ts` 新写）：每个用例用管理员新建自己的老师账号与班级，不依赖其他用例的数据。「系统至少保留一个管理员」需要库里恰好一个管理员，共享库上不稳定，由 Go 单测 `internal/api/edition_test.go` 覆盖。
- `edition-switch`（只在 Go 上跑）：运行时版本切换（`PUT /settings/edition`）与首次运行接口（`POST /setup/edition`）是 Go 版新增的，oracle 没有。用例会把实例切到个人版、验证降级后的 404 / 注册关闭 / 数据范围，最后（含失败时）切回班级版；被测实例须未设 `VINX_EDITION`。全新安装走向导、锁定版本、并发选择由 Go 单测覆盖。
- `edition` 的 `/config` 结构快照：Go 多出的 `editionLocked`、`needsSetup` 两个字段只在 Go 上断言类型，比对结构前去掉。

## 未覆盖

- **`StudySession_active_uniq` 部分唯一索引存在**（旧 security.flow K12）：HTTP 观察不到。Go 端由 `internal/store` 单测覆盖（建表后索引存在、同学生同计划同类型第二个进行中的组冲突、`planId` 为空按 `''` 参与判断）。并发开组的 HTTP 行为由 `learning.spec.ts`（同时发 6 个开组请求，只产生一个进行中的组）覆盖。
- **令牌过期**：黑盒拿不到签名密钥，造不出过期令牌。Go 端由 `internal/auth` 与 `internal/api` 单测用固定时钟覆盖（`Authorization token expired`，401）。
- **个人版的「第一个账号成为管理员」**：需要一个空库的个人版实例。Go 端由 `internal/api` 单测覆盖。
- **`SIGNUP_ENABLED=false`**：旧后端用 `z.coerce.boolean()` 解析，字符串 `"false"` 会被当成真（旧版缺陷），无法在 oracle 上关闭注册；Go 端按字面解析，由单测覆盖。
- **生产模式的 Cookie `Secure` 与 Origin 白名单**：oracle 以开发模式运行。Go 端由单测覆盖（`COOKIE_SECURE=auto` 仅 TLS 请求带 `Secure`；`ALLOWED_ORIGINS` 白名单与 `*`）。

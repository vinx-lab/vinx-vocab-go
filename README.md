# Vinx Vocab 单文件版

面向中文母语学习者（初中为主）的英语单词记忆 Web 应用，打包成**一个可执行文件**：不需要安装数据库、Node 或 Docker。

- Windows：双击 `vinx-vocab.exe`，电脑自己用；同一局域网的手机用浏览器访问学习。
- Linux / NAS：`vinx-vocab` 当班级服务器。
- 功能、页面、操作习惯与服务器版（Node + PostgreSQL）一致；旧版的数据可以完整导入。需求与取舍见 [spec 0001](docs/specs/0001-go-single-binary.md)。

老师带班或学生自学，按学习计划每天实时生成「今日」队列；学习流为 认识 → 练习 → 巩固错词，记忆排程用 FSRS。产品规则见 [docs/decisions.md](docs/decisions.md)。

## 快速开始（Windows）

1. 把 `vinx-vocab.exe` 放进一个**自己的文件夹**（例如 `D:\VinxVocab\`）。不要在压缩包里直接双击运行，也不要放进 `C:\Program Files\`：数据默认存在程序旁边的 `vinx-data\`，放在这些位置会写不进去或落进临时目录。
2. 双击运行。第一次可能出现：
   - **SmartScreen「Windows 已保护你的电脑」**：点「更多信息」→「仍要运行」（程序没有代码签名）。
   - **Windows 防火墙**：勾选「专用网络」后允许。不允许的话只有本机能用，手机连不上。
3. 控制台窗口会显示本机地址、局域网地址和数据目录，并自动打开浏览器。**这个窗口就是服务，关掉窗口即停止。** 不要用鼠标在窗口里选文字：Windows 控制台进入选择状态后程序输出会被卡住，按 Esc 恢复。
4. 第一次打开会让你选择用法：「自己用」（个人版）或「老师带班」（班级版）。之后在系统设置里可以切换。
5. **马上注册第一个账号**：全新安装时第一个注册的账号就是管理员。局域网里其他人也能打开注册页，所以启动后先把自己的账号注册好。
6. 手机和电脑连同一个 Wi-Fi，浏览器打开控制台里显示的局域网地址（如 `http://192.168.1.10:3000`）。同一台设备请固定用同一个地址访问，登录状态按地址保存。

再次双击时，如果程序已经在运行，会直接打开浏览器，不会起第二个。端口被别的程序占用时会提示换端口，按回车关闭窗口。

## Linux / NAS

```bash
./vinx-vocab --data /srv/vinx-vocab/data --port 3000
```

- 默认监听 `0.0.0.0:3000`；`--host`、`--port` 可改。
- **一定要指定数据目录**：Linux 默认是**当前工作目录**下的 `./data`。用 systemd 运行时写 `--data`（或设置 `WorkingDirectory`），否则数据会落到 `/data`。
- systemd 示例（按实际路径和用户修改）：

  ```ini
  [Unit]
  Description=Vinx Vocab
  After=network-online.target

  [Service]
  User=vinx
  ExecStart=/opt/vinx-vocab/vinx-vocab --data /var/lib/vinx-vocab --port 3000
  Restart=on-failure

  [Install]
  WantedBy=multi-user.target
  ```

- **放在反向代理后面**：写请求会检查 `Origin` 与请求的 `Host` 是否一致。代理要把原始 Host 传过来（nginx：`proxy_set_header Host $host;`），或者用 `ALLOWED_ORIGINS=https://vocab.example.com` 列出对外地址，否则登录等写请求会得到 403「非法请求来源」。代理做 HTTPS 终止时再设 `COOKIE_SECURE=true`。AI 生成是后台任务，不受代理超时影响。
- 升级：停服务，替换可执行文件，再启动。有数据库结构变化时，启动时会先把 `vinx.db` 备份为 `vinx.db.bak-<时间>`（保留最近 3 份）再迁移。升级后已打开的页面会自动刷新一次。

## 数据目录

| 文件 | 说明 |
|---|---|
| `vinx.db` | SQLite 数据库（WAL 模式，运行中会有 `-wal`、`-shm` 文件） |
| `secret.key` | 首次启动生成的登录签名密钥和设置加密密钥（权限 0600）。**和 `vinx.db` 一起备份**：丢了它，所有人要重新登录，已保存的 AI Key 需要重新填 |
| `audio/` | 真人发音缓存 |
| `config.toml` | 可选配置文件 |

备份：停服务后复制整个数据目录；或者运行中用 `sqlite3 vinx.db ".backup 备份.db"`。

## 配置

优先级：命令行参数 > 环境变量 > 数据目录里的 `config.toml` > 默认值。`config.toml` 的键名与环境变量相同：

```toml
PORT = 3000
VINX_EDITION = "school"
APP_TIMEZONE = "Asia/Shanghai"
```

| 变量 | 默认 | 说明 |
|---|---|---|
| `PORT` / `HOST` | `3000` / `0.0.0.0` | 监听地址 |
| `VINX_DATA_DIR` | 见上 | 数据目录（`--data` 优先） |
| `VINX_EDITION` | 不设 | `personal` / `school`。设了以后以它为准，界面上不能切换；不设时首次运行选择 |
| `SIGNUP_ENABLED` | `true` | 班级版是否开放注册（个人版只允许一个账号）。全新安装时忽略，保证能注册出管理员 |
| `APP_TIMEZONE` | `Asia/Shanghai` | 学习日按这个时区切分 |
| `JWT_SECRET` / `SETTINGS_SECRET` | 取 `secret.key` | 一般不用设 |
| `JWT_EXPIRES_IN` | `7d` | 登录有效期 |
| `COOKIE_SECURE` | `auto` | `auto` 时只有 HTTPS 请求带 Secure |
| `ALLOWED_ORIGINS` | 空 | 额外允许的来源，逗号分隔，`*` 不限 |
| `AUDIO_PROVIDER_URL` | 有道发音 | 真人发音来源，`{word}` 为占位符 |
| `AUDIO_DIR` | 数据目录下 `audio/` | 发音缓存目录 |
| `AI_PROVIDER` / `AI_BASE_URL` / `AI_API_KEY` / `AI_MODEL` / `AI_TIMEOUT_MS` | 不设 | AI 接口；更常用的方式是管理员在「系统设置」页填写（优先于环境变量） |

出站请求（AI、发音）使用系统代理环境变量 `HTTPS_PROXY` / `HTTP_PROXY`。

## 命令

```text
vinx-vocab [serve] [--port 3000] [--host 0.0.0.0] [--data 数据目录]   启动服务（默认）
vinx-vocab seed-demo [--data 数据目录]                               写入演示账号、演示班级 DEMO01 与演示计划
vinx-vocab audio prefetch [--data 目录] [--apply] [--limit N] [--delay 毫秒]
                                                                      全量预缓存真人发音（默认只演练，--apply 才实际抓取）
vinx-vocab import --from-postgres <URL> [--settings-secret <旧密钥>] [--force] [--edition school|personal] [--data 目录]
                                                                      从旧版 PostgreSQL 导入
vinx-vocab version                                                   打印版本
```

`seed-demo` 的演示账号：`admin` / `teacher` / `student` / `student2@vinx.test`，密码 `dev123456`。只用于体验和测试。

## 从服务器版（PostgreSQL）导入

1. **先停掉本程序**（导入期间持有数据库写锁，运行中的服务也会缓存旧的 AI 配置）。旧的服务器版不用停：导入只读旧库，并在一个只读快照里完成。
2. 用之后运行服务时相同的数据目录执行：

   ```bash
   PGPASSWORD=… VINX_IMPORT_SETTINGS_SECRET=… \
     ./vinx-vocab import --from-postgres "postgresql://用户@主机:5432/库名" --data /var/lib/vinx-vocab
   ```

   - 密码用 `PGPASSWORD` 或 `~/.pgpass` 提供，不写在命令行里。
   - `VINX_IMPORT_SETTINGS_SECRET`（或 `--settings-secret`）是**旧实例实际生效的密钥**：旧版设了 `SETTINGS_SECRET` 就用它，没设就用旧的 `JWT_SECRET`。给了才能把旧的 AI Key 带过来；不给则其余照常导入，AI Key 需要管理员重新填写。
   - 目标库已有账号或自建词书时拒绝导入；`--force` 会先备份为 `vinx.db.before-import-<时间>` 再覆盖。
3. 导入在一个事务里完成，结束时打印逐表行数对账；任何一步失败都整体回滚。
4. 导入后所有人需要重新登录一次，密码不变。旧部署的发音缓存可以直接复制到数据目录的 `audio/`。

## 开发

需要 Go（版本见 `go.mod`）、Node 与 pnpm。

```bash
make test         # Go 单测（设了 VINX_TEST_PG_URL 时连 PostgreSQL 导入测试一起跑）
make vet
make build        # 构建前端并嵌入，生成 dist/vinx-vocab
make cross        # 交叉编译 dist/vinx-vocab.exe（windows/amd64，带图标与版本信息）与 dist/vinx-vocab（linux/amd64）
make e2e          # 构建后起两个实例跑 Playwright
make contract     # API 契约测试（CONTRACT_BASE_URL 指向运行中的实例）
pnpm -C web dev   # 前端开发服务器
```

本机的 Go 工具链、模块缓存、代理等可以写在不入库的 `local.mk` 里（Makefile 会自动读取），例如：

```make
export GOPROXY := https://goproxy.cn,direct
GO := /path/to/go/bin/go
```

目录结构、分层与约定见 [docs/architecture.md](docs/architecture.md)；agent 与贡献者约定见 [AGENTS.md](AGENTS.md)。

- `internal/core/`：纯业务规则，表驱动单测
- `internal/service/`：装配与事务；数据范围集中在 `access.go`
- `internal/api/`：路由、校验、权限
- `web/`：Preact 前端（Vite），构建产物用 `go:embed` 嵌入
- `contract/`：API 黑盒契约测试；`e2e/`：Playwright 端到端测试

## 许可证

[MIT](LICENSE)

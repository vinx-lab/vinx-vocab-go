# 0002 认证用 JWT + HttpOnly Cookie

状态：已采纳（沿用服务器版的做法，按单文件的运行环境调整）

## 背景

前端脚本不能拿到凭据；手机和电脑都要能用；单文件多在本机或局域网 http 下运行，也可能放在 HTTPS 反向代理后面。

## 决定

- JWT（HS256）放在 cookie `vinx_token` 里：`HttpOnly`、`SameSite=Lax`、`Path=/`，有效期 `JWT_EXPIRES_IN`（默认 7 天）。登录接口不在响应体里返回 token。
- `Secure`：`COOKIE_SECURE=auto`（默认）时只有 TLS 请求带；反代做 HTTPS 终止时设 `COOKIE_SECURE=true`。（旧版按 `NODE_ENV` 判断。）
- 前端与 API 同源（API 挂在 `/api/` 下），不需要 CORS。
- CSRF 以 `SameSite=Lax` 为主；写请求带 `Origin` 时只放行与 `Host` 相同的来源或 `ALLOWED_ORIGINS` 列出的来源。（旧版生产环境只认白名单；单文件默认放行同主机，局域网访问不需要配置。）
- 签名密钥未给出时取数据目录 `secret.key`（K45）。
- 权限用角色能力表（`internal/core/access.go`，K23）+ 数据范围（`internal/service/access.go`）。

## 后果

- cookie 不设 Domain，按访问的主机名归属：同一台设备固定用一个地址访问。
- 反向代理要传原始 `Host`，否则写请求 403；见 README。
- 身份来自令牌、不查库：改角色后要重新登录才生效。

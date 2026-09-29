// Package auth 认证：JWT（HS256）放 HttpOnly Cookie vinx_token，bcrypt 密码，以及登录 / 能力守卫。
//
// 令牌错误的响应照旧后端（@fastify/jwt + 旧 error-handler）的实际输出：
//   - 没带令牌 → 401 UNAUTHORIZED「未登录或登录已失效」
//   - 令牌无效 / 过期 → 401，code 为 VALIDATION，message 为 @fastify/jwt 英文原文
//   - Authorization: Bearer 格式不对 → 400 VALIDATION
//
// 前端按 HTTP 401 跳登录页，所以 code 的差异不影响使用；保持一致是为了契约测试可以逐字比对。
package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// CookieName 登录 Cookie 名。
const CookieName = "vinx_token"

// CookieMaxAge Cookie 有效期（与旧版一致固定 7 天，与 JWT_EXPIRES_IN 无关）。
const CookieMaxAge = 7 * 24 * 3600

// BcryptCost 新密码哈希的代价（旧版 12；旧哈希任意代价都能校验）。
const BcryptCost = 12

// Claims JWT 载荷：sub / email / name / role（+ iat / exp）。
type Claims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Iat   int64  `json:"iat"`
	Exp   int64  `json:"exp"`
}

func (c Claims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Exp, 0)), nil
}
func (c Claims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Iat, 0)), nil
}
func (c Claims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }
func (c Claims) GetIssuer() (string, error)              { return "", nil }
func (c Claims) GetSubject() (string, error)             { return c.Sub, nil }
func (c Claims) GetAudience() (jwt.ClaimStrings, error)  { return nil, nil }

// Auth 签发与校验令牌、读写 Cookie。
type Auth struct {
	Secret       []byte
	ExpiresIn    time.Duration
	CookieSecure string // auto | true | false
	Now          func() time.Time
}

// New 构造；now 为 nil 时用 time.Now。
func New(secret string, expiresIn time.Duration, cookieSecure string, now func() time.Time) *Auth {
	if now == nil {
		now = time.Now
	}
	return &Auth{Secret: []byte(secret), ExpiresIn: expiresIn, CookieSecure: cookieSecure, Now: now}
}

// Sign 签发令牌。
func (a *Auth) Sign(sub, email, name, role string) (string, error) {
	iat := a.Now().Unix()
	c := Claims{Sub: sub, Email: email, Name: name, Role: role, Iat: iat, Exp: iat + int64(a.ExpiresIn/time.Second)}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(a.Secret)
}

func (a *Auth) secure(r *http.Request) bool {
	switch a.CookieSecure {
	case "true":
		return true
	case "false":
		return false
	}
	return r.TLS != nil
}

// SetCookie 登录成功后种 HttpOnly Cookie。
func (a *Auth) SetCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", MaxAge: CookieMaxAge, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: a.secure(r)})
}

// ClearCookie 登出：清 Cookie（Max-Age=0 + 1970 年 Expires）。
func (a *Auth) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(0, 0)})
}

// SignIn 签发令牌并种 Cookie。
func (a *Auth) SignIn(w http.ResponseWriter, r *http.Request, u *service.UserRow) error {
	token, err := a.Sign(u.ID, u.Email, u.Name, u.Role)
	if err != nil {
		return err
	}
	a.SetCookie(w, r, token)
	return nil
}

var bearerRe = regexp.MustCompile(`(?i)^Bearer\s`)

func tokenInvalid(msg string) *httpx.Error {
	return httpx.Validation("Authorization token is invalid: " + msg).WithStatus(http.StatusUnauthorized)
}

// lookupToken 先看 Authorization: Bearer，再看 Cookie（与 @fastify/jwt 相同顺序）。
func lookupToken(r *http.Request) (string, *httpx.Error) {
	if h := r.Header.Get("Authorization"); h != "" && bearerRe.MatchString(h) {
		parts := strings.Split(h, " ")
		if len(parts) != 2 {
			return "", httpx.Validation("Format is Authorization: Bearer [token]")
		}
		return parts[1], nil
	}
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return c.Value, nil
	}
	return "", httpx.Unauthorized("未登录或登录已失效")
}

// b64 解码 JWT 段（与 Node Buffer.from(x, "base64") 一样宽松：接受 base64url / 标准字母表、有无填充）。
func b64(s string) ([]byte, error) {
	s = strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(s), "=")
	return base64.RawURLEncoding.DecodeString(s)
}

// Verify 校验请求携带的令牌，返回载荷；失败返回照旧后端格式的 *httpx.Error。
func (a *Auth) Verify(r *http.Request) (*Claims, *httpx.Error) {
	token, herr := lookupToken(r)
	if herr != nil {
		return nil, herr
	}
	first, last := strings.Index(token, "."), strings.LastIndex(token, ".")
	if first == -1 || first >= last {
		return nil, tokenInvalid("The token is malformed.")
	}
	var header map[string]any
	if hb, err := b64(token[:first]); err != nil || json.Unmarshal(hb, &header) != nil || header == nil {
		return nil, tokenInvalid("The token header is not a valid base64url serialized JSON.")
	}
	var payload any
	if pb, err := b64(token[first+1 : last]); err != nil || json.Unmarshal(pb, &payload) != nil {
		return nil, tokenInvalid("The token payload is not a valid base64url serialized JSON.")
	}
	if _, ok := payload.(map[string]any); !ok {
		return nil, httpx.NewError(httpx.CodeServer, "服务器内部错误", nil)
	}
	var claims Claims
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return a.Secret, nil },
		jwt.WithValidMethods([]string{"HS256", "HS384", "HS512"}), jwt.WithoutClaimsValidation())
	if err != nil {
		if errors.Is(err, jwt.ErrTokenSignatureInvalid) || errors.Is(err, jwt.ErrTokenUnverifiable) {
			return nil, tokenInvalid("The token signature is invalid.")
		}
		return nil, tokenInvalid("The token is malformed.")
	}
	if claims.Exp != 0 && claims.Exp*1000 <= a.Now().UnixMilli() {
		return nil, httpx.Validation("Authorization token expired").WithStatus(http.StatusUnauthorized)
	}
	return &claims, nil
}

// HashPassword bcrypt 哈希。
func HashPassword(pwd string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pwd), BcryptCost)
	return string(b), err
}

// HashPasswordCost 指定代价（seed 用 10，与旧 seed 一致）。
func HashPasswordCost(pwd string, cost int) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pwd), cost)
	return string(b), err
}

// VerifyPassword 校验密码（兼容旧 bcryptjs 生成的 $2a$/$2b$ 哈希）。
func VerifyPassword(pwd, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pwd)) == nil
}

// ---------------------------------------------------------------------------
// 请求上下文里的身份
// ---------------------------------------------------------------------------

type ctxKey int

const keyAuth ctxKey = iota

type authResult struct {
	actor *service.Actor
	err   *httpx.Error
}

// Authenticate 解析请求携带的令牌（不拒绝请求），结果放进上下文供守卫与 ActorFrom 使用。
func (a *Auth) Authenticate(r *http.Request) *http.Request {
	claims, err := a.Verify(r)
	res := &authResult{err: err}
	if err == nil {
		res.actor = &service.Actor{ID: claims.Sub, Role: claims.Role, Name: claims.Name}
	}
	return r.WithContext(context.WithValue(r.Context(), keyAuth, res))
}

// WithActor 测试或内部调用时直接放入身份。
func WithActor(ctx context.Context, a *service.Actor) context.Context {
	return context.WithValue(ctx, keyAuth, &authResult{actor: a})
}

// ActorFrom 当前登录者；未登录或令牌无效时返回 nil。挂了 RequireAuth / RequireCap 的处理函数里一定非 nil。
func ActorFrom(ctx context.Context) *service.Actor {
	if res, ok := ctx.Value(keyAuth).(*authResult); ok {
		return res.actor
	}
	return nil
}

// MustActor 当前登录者；未登录时返回与 RequireAuth 相同的错误（旧 getActor 的语义，适合没挂守卫的路由）。
func MustActor(ctx context.Context) (*service.Actor, error) {
	if err := authErr(ctx); err != nil {
		return nil, err
	}
	return ActorFrom(ctx), nil
}

func authErr(ctx context.Context) error {
	res, ok := ctx.Value(keyAuth).(*authResult)
	if !ok {
		return httpx.Unauthorized("未登录或登录已失效")
	}
	if res.err != nil {
		return res.err
	}
	return nil
}

// RequireAuth 守卫：登录即可（旧 authenticate / requireAuth）。
func RequireAuth(next httpx.HandlerFunc) httpx.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		if err := authErr(r.Context()); err != nil {
			return err
		}
		return next(w, r)
	}
}

// RequireCap 守卫：要求当前角色具备任一能力（旧 requireCap(...caps)），否则 403「无权访问」。
func RequireCap(caps ...core.Capability) httpx.Middleware {
	return func(next httpx.HandlerFunc) httpx.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			if err := authErr(r.Context()); err != nil {
				return err
			}
			actor := ActorFrom(r.Context())
			if len(caps) > 0 {
				ok := false
				for _, c := range caps {
					if service.Can(actor, c) {
						ok = true
						break
					}
				}
				if !ok {
					return httpx.Forbidden("无权访问")
				}
			}
			return next(w, r)
		}
	}
}

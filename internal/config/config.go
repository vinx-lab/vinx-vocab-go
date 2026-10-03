// Package config 运行配置：命令行参数 > 环境变量 > 数据目录里的 config.toml > 默认值。
//
// 环境变量名沿用旧版（VINX_EDITION、APP_TIMEZONE、JWT_SECRET、AI_* …）；config.toml 的键名与环境变量相同，
// 例如：
//
//	VINX_EDITION = "school"
//	APP_TIMEZONE = "Asia/Shanghai"
//	PORT = 3000
//
// JWT_SECRET / SETTINGS_SECRET 未通过环境变量或 config.toml 给出时，使用数据目录里的 secret.key（首次启动随机生成）。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config 解析后的配置（只读；运行中可变的设置放 AppSetting 表）。
type Config struct {
	DataDir string // 数据目录（绝对路径）
	DBPath  string // <DataDir>/vinx.db
	Host    string // 监听地址，默认 0.0.0.0
	Port    int    // 监听端口，默认 3000

	// Edition 环境变量 / config.toml 指定的版本；空串表示未锁定（运行时从 AppSetting 读取，见 service.Editions）。
	Edition string

	AppTimezone string
	Location    *time.Location

	JWTSecret      string
	JWTExpiresIn   time.Duration
	SettingsSecret string
	CookieSecure   string   // auto | true | false
	AllowedOrigins []string // 额外允许的写请求 Origin；含 "*" 表示不限

	SignupEnabled bool

	// Dev 开发模式（serve --dev）：开放免密切换账号的 /api/dev/* 接口。只认命令行参数，
	// 不读环境变量或 config.toml，避免部署时被配置文件意外打开（spec 0002）。
	Dev bool

	AudioDir        string
	AudioProviderURL string // 空串表示关闭真人发音

	AIProvider  string // auto | openai | anthropic
	AIAPIKey    string
	AIBaseURL   string
	AIModel     string
	AITimeoutMs int
}

// Options 命令行参数与测试注入。
type Options struct {
	DataDir string // --data；空串用默认
	Host    string // --host；空串用默认
	Port    int    // --port；0 用默认
	Dev     bool   // --dev：开发模式（只认命令行参数）

	// Lookup 读环境变量（默认 os.LookupEnv）。
	Lookup func(string) (string, bool)
	// GOOS / ExePath 用于计算默认数据目录（默认 runtime.GOOS / os.Executable）。
	GOOS    string
	ExePath string
}

// DefaultDataDir 默认数据目录：Windows 为 exe 旁的 vinx-data，其他系统为当前目录下的 data。
func DefaultDataDir(goos, exePath string) string {
	if goos == "windows" {
		return filepath.Join(filepath.Dir(exePath), "vinx-data")
	}
	return "data"
}

// source 按优先级取值：环境变量 > config.toml。
type source struct {
	lookup func(string) (string, bool)
	file   map[string]string
}

func (s source) get(key string) (string, bool) {
	if v, ok := s.lookup(key); ok {
		return v, true
	}
	v, ok := s.file[key]
	return v, ok
}

func (s source) str(key, def string) string {
	if v, ok := s.get(key); ok {
		return v
	}
	return def
}

// Load 解析数据目录（必要时创建并检查可写）、读取 config.toml 与 secret.key、合并环境变量。
func Load(o Options) (*Config, error) {
	if o.Lookup == nil {
		o.Lookup = os.LookupEnv
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.ExePath == "" {
		o.ExePath, _ = os.Executable()
	}

	dataDir := o.DataDir
	if dataDir == "" {
		if v, ok := o.Lookup("VINX_DATA_DIR"); ok && v != "" {
			dataDir = v
		} else {
			dataDir = DefaultDataDir(o.GOOS, o.ExePath)
		}
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err := EnsureDataDir(abs); err != nil {
		return nil, err
	}

	file, err := readConfigFile(filepath.Join(abs, "config.toml"))
	if err != nil {
		return nil, err
	}
	src := source{lookup: o.Lookup, file: file}

	c := &Config{DataDir: abs, DBPath: filepath.Join(abs, "vinx.db"), Dev: o.Dev}

	c.Host = o.Host
	if c.Host == "" {
		c.Host = src.str("HOST", "0.0.0.0")
	}
	c.Port = o.Port
	if c.Port == 0 {
		p := src.str("PORT", "3000")
		if c.Port, err = strconv.Atoi(p); err != nil || c.Port <= 0 || c.Port > 65535 {
			return nil, fmt.Errorf("端口 %q 不合法", p)
		}
	}

	c.Edition = strings.TrimSpace(src.str("VINX_EDITION", ""))
	if c.Edition != "" && c.Edition != "personal" && c.Edition != "school" {
		return nil, fmt.Errorf("VINX_EDITION 只能是 personal 或 school（当前为 %q）", c.Edition)
	}

	c.AppTimezone = src.str("APP_TIMEZONE", "Asia/Shanghai")
	if c.Location, err = time.LoadLocation(c.AppTimezone); err != nil {
		return nil, fmt.Errorf("APP_TIMEZONE %q 无法识别：%w", c.AppTimezone, err)
	}

	exp := src.str("JWT_EXPIRES_IN", "7d")
	if c.JWTExpiresIn, err = ParseDuration(exp); err != nil {
		return nil, fmt.Errorf("JWT_EXPIRES_IN %q 不合法：%w", exp, err)
	}

	c.CookieSecure = src.str("COOKIE_SECURE", "auto")
	if c.CookieSecure != "auto" && c.CookieSecure != "true" && c.CookieSecure != "false" {
		return nil, fmt.Errorf("COOKIE_SECURE 只能是 auto / true / false")
	}
	for _, s := range strings.Split(src.str("ALLOWED_ORIGINS", ""), ",") {
		if s = strings.TrimSpace(s); s != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, s)
		}
	}

	c.SignupEnabled = parseBool(src.str("SIGNUP_ENABLED", "true"))

	c.AudioDir = src.str("AUDIO_DIR", "")
	if c.AudioDir == "" {
		c.AudioDir = filepath.Join(abs, "audio")
	}
	c.AudioProviderURL = src.str("AUDIO_PROVIDER_URL", "https://dict.youdao.com/dictvoice?audio={word}&type=2")

	c.AIProvider = src.str("AI_PROVIDER", "auto")
	if c.AIProvider != "auto" && c.AIProvider != "openai" && c.AIProvider != "anthropic" {
		return nil, fmt.Errorf("AI_PROVIDER 只能是 auto / openai / anthropic")
	}
	c.AIAPIKey = src.str("AI_API_KEY", "")
	c.AIBaseURL = src.str("AI_BASE_URL", "")
	c.AIModel = src.str("AI_MODEL", "qwen2.5:7b-instruct")
	t := src.str("AI_TIMEOUT_MS", "180000")
	if c.AITimeoutMs, err = strconv.Atoi(t); err != nil || c.AITimeoutMs < 5000 || c.AITimeoutMs > 600000 {
		return nil, fmt.Errorf("AI_TIMEOUT_MS 应为 5000–600000 的整数（当前 %q）", t)
	}

	// 密钥：环境变量 / config.toml 优先，否则 secret.key
	jwtSecret, hasJWT := src.get("JWT_SECRET")
	settingsSecret, hasSettings := src.get("SETTINGS_SECRET")
	hasJWT = hasJWT && strings.TrimSpace(jwtSecret) != ""
	hasSettings = hasSettings && strings.TrimSpace(settingsSecret) != ""
	if !hasJWT || !hasSettings {
		keys, err := LoadOrCreateSecrets(filepath.Join(abs, "secret.key"))
		if err != nil {
			return nil, err
		}
		if !hasJWT {
			jwtSecret = keys.JWTSecret
		}
		if !hasSettings {
			settingsSecret = keys.SettingsSecret
		}
	}
	c.JWTSecret = jwtSecret
	c.SettingsSecret = strings.TrimSpace(settingsSecret)
	return c, nil
}

// parseBool 布尔开关：false / 0 / no / off / 空串为假，其余为真。
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "false", "0", "no", "off":
		return false
	}
	return true
}

func readConfigFile(path string) (map[string]string, error) {
	out := map[string]string{}
	var raw map[string]any
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, fmt.Errorf("读取 %s 失败：%w", path, err)
	}
	for k, v := range raw {
		switch x := v.(type) {
		case string:
			out[k] = x
		case []any:
			parts := make([]string, len(x))
			for i, p := range x {
				parts[i] = fmt.Sprint(p)
			}
			out[k] = strings.Join(parts, ",")
		default:
			out[k] = fmt.Sprint(x)
		}
	}
	return out, nil
}

// EnsureDataDir 创建数据目录并确认可写；失败时给出明确的中文提示。
func EnsureDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("无法创建数据目录 %s：%v。请用 --data 指定一个可写的目录", dir, err)
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return fmt.Errorf("数据目录 %s 不可写：%v。请用 --data 指定一个可写的目录", dir, err)
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return nil
}

var durationRe = regexp.MustCompile(`(?i)^\s*(-?\d*\.?\d+)\s*(ms|msecs?|milliseconds?|s|secs?|seconds?|m|mins?|minutes?|h|hrs?|hours?|d|days?|w|weeks?|y|yrs?|years?)?\s*$`)

// ParseDuration 解析 vercel/ms 风格的时长（"7d"、"12h"、"30m"、纯数字按毫秒），与旧版 JWT_EXPIRES_IN 一致。
func ParseDuration(s string) (time.Duration, error) {
	m := durationRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("无法识别的时长")
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, err
	}
	unit := strings.ToLower(m[2])
	var mult float64
	switch {
	case unit == "" || strings.HasPrefix(unit, "ms") || strings.HasPrefix(unit, "milli"):
		mult = float64(time.Millisecond)
	case strings.HasPrefix(unit, "s"):
		mult = float64(time.Second)
	case strings.HasPrefix(unit, "m"):
		mult = float64(time.Minute)
	case strings.HasPrefix(unit, "h"):
		mult = float64(time.Hour)
	case strings.HasPrefix(unit, "d"):
		mult = float64(24 * time.Hour)
	case strings.HasPrefix(unit, "w"):
		mult = float64(7 * 24 * time.Hour)
	case strings.HasPrefix(unit, "y"):
		mult = float64(365.25 * 24 * float64(time.Hour))
	}
	d := time.Duration(n * mult)
	if d <= 0 {
		return 0, fmt.Errorf("时长必须为正")
	}
	return d, nil
}

package ai

import (
	"regexp"
	"strings"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core/secretbox"
)

// Provider 实际可用的供应商；none 表示 AI 不可用。
type Provider = string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderNone      Provider = "none"
)

// SettingProvider 页面上可选的供应商：OpenAI 兼容 / Claude / 关闭。
type SettingProvider = string

const (
	SettingOpenAI    SettingProvider = "openai"
	SettingAnthropic SettingProvider = "anthropic"
	SettingOff       SettingProvider = "off"
)

// SettingProviders 合法取值。
var SettingProviders = []SettingProvider{SettingOpenAI, SettingAnthropic, SettingOff}

// SettingProviderLabel 中文名。
var SettingProviderLabel = map[SettingProvider]string{SettingOpenAI: "OpenAI 兼容", SettingAnthropic: "Claude", SettingOff: "关闭"}

// TimeoutSecMin / TimeoutSecMax 超时范围（秒）。
const (
	TimeoutSecMin = 5
	TimeoutSecMax = 600
)

// TimeoutMsMin / TimeoutMsMax 超时范围（毫秒）。
const (
	TimeoutMsMin = 5_000
	TimeoutMsMax = 600_000
)

// EnvInput 环境变量里的 AI 配置。
type EnvInput struct {
	Provider  string // auto | openai | anthropic
	APIKey    string
	BaseURL   string
	Model     string
	TimeoutMs int
}

// StoredSetting AppSetting(key = "ai").value 的结构；Key 只存密文。
type StoredSetting struct {
	Provider  SettingProvider
	BaseURL   *string
	APIKeyEnc *string
	Model     string
	TimeoutMs int
}

// Config 当前生效的配置（服务端内部使用，含 Key 明文，不能直接返回给前端）。
type Config struct {
	Source           string // db | env
	Provider         Provider
	Selected         SettingProvider
	BaseURL          *string
	APIKey           string
	Model            string
	TimeoutMs        int
	KeyUndecryptable bool
	UpdatedAt        *time.Time
}

// CallConfig 调用 AI 真正需要的字段。
type CallConfig struct {
	Provider  Provider
	BaseURL   *string
	APIKey    string
	Model     string
	TimeoutMs int
}

func (c Config) ToCallConfig() CallConfig {
	return CallConfig{Provider: c.Provider, BaseURL: c.BaseURL, APIKey: c.APIKey, Model: c.Model, TimeoutMs: c.TimeoutMs}
}

func effectiveProvider(selected SettingProvider, baseURL *string, apiKey string) Provider {
	switch selected {
	case SettingOpenAI:
		if baseURL != nil && *baseURL != "" {
			return ProviderOpenAI
		}
	case SettingAnthropic:
		if apiKey != "" {
			return ProviderAnthropic
		}
	}
	return ProviderNone
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ConfigFromEnv 环境变量：auto 时有 AI_BASE_URL 走 OpenAI 兼容，否则有 Key 走 Claude。
func ConfigFromEnv(e EnvInput) Config {
	baseURL := strOrNil(strings.TrimSpace(e.BaseURL))
	selected := e.Provider
	if selected == "auto" {
		switch {
		case baseURL != nil:
			selected = SettingOpenAI
		case e.APIKey != "":
			selected = SettingAnthropic
		default:
			selected = SettingOff
		}
	}
	return Config{
		Source:    "env",
		Provider:  effectiveProvider(selected, baseURL, e.APIKey),
		Selected:  selected,
		BaseURL:   baseURL,
		APIKey:    e.APIKey,
		Model:     e.Model,
		TimeoutMs: e.TimeoutMs,
	}
}

// ConfigFromStored 页面保存的配置；Key 解不开时按「未配置 Key」处理。
func ConfigFromStored(s StoredSetting, decrypt func(string) (string, bool), updatedAt *time.Time) Config {
	apiKey := ""
	undecryptable := false
	if s.APIKeyEnc != nil && *s.APIKeyEnc != "" {
		plain, ok := decrypt(*s.APIKeyEnc)
		if ok {
			apiKey = plain
		} else {
			undecryptable = true
		}
	}
	return Config{
		Source:           "db",
		Provider:         effectiveProvider(s.Provider, s.BaseURL, apiKey),
		Selected:         s.Provider,
		BaseURL:          s.BaseURL,
		APIKey:           apiKey,
		Model:            s.Model,
		TimeoutMs:        s.TimeoutMs,
		KeyUndecryptable: undecryptable,
		UpdatedAt:        updatedAt,
	}
}

// ResolveConfig 优先级：数据库 > 环境变量。
func ResolveConfig(env EnvInput, stored *StoredSetting, decrypt func(string) (string, bool), updatedAt *time.Time) Config {
	if stored != nil {
		return ConfigFromStored(*stored, decrypt, updatedAt)
	}
	return ConfigFromEnv(env)
}

// ParseStoredSetting 校验数据库里的 JSON；结构不对时当作没有保存过（返回 nil）。
func ParseStoredSetting(v map[string]any) *StoredSetting {
	provider, _ := v["provider"].(string)
	if provider != SettingOpenAI && provider != SettingAnthropic && provider != SettingOff {
		return nil
	}
	model, ok1 := v["model"].(string)
	timeoutF, ok2 := v["timeoutMs"].(float64)
	if !ok1 || !ok2 {
		return nil
	}
	out := &StoredSetting{Provider: provider, Model: model, TimeoutMs: int(timeoutF)}
	if bu, ok := v["baseUrl"].(string); ok && bu != "" {
		out.BaseURL = &bu
	}
	if enc, ok := v["apiKeyEnc"].(string); ok && enc != "" {
		out.APIKeyEnc = &enc
	}
	return out
}

// ResolveSubmittedKey 提交的 Key：填了就用新的；勾了清除就置空；留空沿用当前生效的 Key。
func ResolveSubmittedKey(typedRaw string, clearApiKey bool, currentKey string) string {
	typed := strings.TrimSpace(typedRaw)
	if typed != "" {
		return typed
	}
	if clearApiKey {
		return ""
	}
	return currentKey
}

// SettingsInput PUT /settings/ai、POST /settings/ai/test 的请求体（已校验的字段）。
type SettingsInput struct {
	Provider    SettingProvider
	BaseURL     *string // nil 表示未提交（沿用/清空按业务逻辑）
	APIKey      *string // 提交的原文（未提交为 nil）
	ClearAPIKey bool
	Model       *string
	TimeoutSec  *int
}

// Candidate 页面提交的值和当前配置合成的一份待保存 / 待测试的配置（Key 为明文）。
type Candidate struct {
	Provider  SettingProvider
	BaseURL   *string
	APIKey    string
	Model     string
	TimeoutMs int
}

var trailingSlashRe = regexp.MustCompile(`/+$`)

// BuildCandidate 见 lib/ai-settings.ts buildCandidate。
func BuildCandidate(input SettingsInput, current Config) Candidate {
	var baseURL *string
	if input.Provider != SettingAnthropic {
		v := ""
		if input.BaseURL != nil {
			v = trailingSlashRe.ReplaceAllString(strings.TrimSpace(*input.BaseURL), "")
		}
		baseURL = strOrNil(v)
	}
	apiKeyRaw := ""
	if input.APIKey != nil {
		apiKeyRaw = *input.APIKey
	}
	model := current.Model
	if input.Model != nil {
		if m := strings.TrimSpace(*input.Model); m != "" {
			model = m
		}
	}
	timeoutMs := current.TimeoutMs
	if input.TimeoutSec != nil && *input.TimeoutSec != 0 {
		timeoutMs = *input.TimeoutSec * 1000
	}
	return Candidate{
		Provider:  input.Provider,
		BaseURL:   baseURL,
		APIKey:    ResolveSubmittedKey(apiKeyRaw, input.ClearAPIKey, current.APIKey),
		Model:     model,
		TimeoutMs: timeoutMs,
	}
}

var httpURLRe = regexp.MustCompile(`(?i)^https?://[^\s/]+`)

// ValidateCandidate 保存前的业务校验；返回错误文案，通过返回空串。
func ValidateCandidate(c Candidate) string {
	if c.TimeoutMs < TimeoutMsMin || c.TimeoutMs > TimeoutMsMax {
		return "超时需在 5–600 秒之间"
	}
	if c.Provider == SettingOff {
		return ""
	}
	if c.Model == "" {
		return "请填写模型"
	}
	if c.Provider == SettingOpenAI {
		if c.BaseURL == nil || *c.BaseURL == "" {
			return "OpenAI 兼容接口需要填写接口地址"
		}
		if !httpURLRe.MatchString(*c.BaseURL) {
			return "接口地址需以 http:// 或 https:// 开头"
		}
	}
	if c.Provider == SettingAnthropic && c.APIKey == "" {
		return "Claude 需要填写 API Key"
	}
	return ""
}

// ToCallConfig 候选配置 → 调用用配置。
func (c Candidate) ToCallConfig() CallConfig {
	return CallConfig{Provider: effectiveProvider(c.Provider, c.BaseURL, c.APIKey), BaseURL: c.BaseURL, APIKey: c.APIKey, Model: c.Model, TimeoutMs: c.TimeoutMs}
}

// SettingsView GET /settings/ai：当前生效的配置。API Key 从不返回，只返回是否已设置和末 4 位。
type SettingsView struct {
	Source           string          `json:"source"`
	Provider         SettingProvider `json:"provider"`
	BaseURL          *string         `json:"baseUrl"`
	Model            string          `json:"model"`
	TimeoutSec       int             `json:"timeoutSec"`
	APIKey           MaskedSecret    `json:"apiKey"`
	KeyUndecryptable bool            `json:"keyUndecryptable"`
	Enabled          bool            `json:"enabled"`
	UpdatedAt        *string         `json:"updatedAt"`
}

// MaskedSecret Key 是否已设置和末 4 位。
type MaskedSecret struct {
	Set   bool    `json:"set"`
	Last4 *string `json:"last4"`
}

// TestResult POST /settings/ai/test 的结果；测试不会保存。
type TestResult struct {
	OK    bool    `json:"ok"`
	MS    int64   `json:"ms"`
	Error *string `json:"error,omitempty"`
}

// ToSettingsView 返回给前端的视图。
func ToSettingsView(cfg Config) SettingsView {
	set, last4 := secretbox.MaskSecret(cfg.APIKey)
	var updatedAt *string
	if cfg.UpdatedAt != nil {
		s := cfg.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z")
		updatedAt = &s
	}
	return SettingsView{
		Source:           cfg.Source,
		Provider:         cfg.Selected,
		BaseURL:          cfg.BaseURL,
		Model:            cfg.Model,
		TimeoutSec:       int((int64(cfg.TimeoutMs) + 500) / 1000), // Math.round
		APIKey:           MaskedSecret{Set: set, Last4: last4},
		KeyUndecryptable: cfg.KeyUndecryptable,
		Enabled:          cfg.Provider != ProviderNone,
		UpdatedAt:        updatedAt,
	}
}

package service

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/secretbox"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// AI 系统设置存放的 AppSetting key（对应旧 services/ai-config.ts）。
const (
	AISettingKey          = "ai"
	AITemplatesSettingKey = "ai.promptTemplates"
)

// AIConfigCache 当前生效的 AI 配置（数据库优先，其次环境变量）与管理员改过的默认要求模板：进程内缓存，
// 保存 / 恢复后调用 Invalidate（两者共用），下一次读取即用新内容。
type AIConfigCache struct {
	mu sync.Mutex

	cfgLoaded bool
	cfg       coreai.Config

	tplLoaded bool
	tpl       coreai.StoredPromptTemplates
}

// NewAIConfigCache 构造空缓存。
func NewAIConfigCache() *AIConfigCache { return &AIConfigCache{} }

// Invalidate 清掉 AI 配置和模板的缓存。
func (c *AIConfigCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfgLoaded, c.tplLoaded = false, false
}

func envInputOf(cfg *config.Config) coreai.EnvInput {
	return coreai.EnvInput{Provider: cfg.AIProvider, APIKey: cfg.AIAPIKey, BaseURL: cfg.AIBaseURL, Model: cfg.AIModel, TimeoutMs: cfg.AITimeoutMs}
}

func decryptWith(secret string) func(string) (string, bool) {
	return func(enc string) (string, bool) { return secretbox.Decrypt(enc, secret) }
}

func sealSecret(cfg *config.Config, plain string) (string, error) {
	return secretbox.Encrypt(plain, cfg.SettingsSecret)
}

func loadAIConfig(ctx context.Context, q store.Querier, cfg *config.Config) (coreai.Config, error) {
	var raw string
	var updatedAt store.Time
	err := q.QueryRowContext(ctx, `SELECT "value","updatedAt" FROM "AppSetting" WHERE "key" = ?`, AISettingKey).Scan(&raw, &updatedAt)
	if store.IsNoRows(err) {
		return coreai.ConfigFromEnv(envInputOf(cfg)), nil
	}
	if err != nil {
		return coreai.Config{}, err
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return coreai.ConfigFromEnv(envInputOf(cfg)), nil
	}
	stored := coreai.ParseStoredSetting(v)
	t := updatedAt.Time
	return coreai.ResolveConfig(envInputOf(cfg), stored, decryptWith(cfg.SettingsSecret), &t), nil
}

// Config 当前生效的 AI 配置（缓存）。
func (c *AIConfigCache) Config(ctx context.Context, q store.Querier, cfg *config.Config) (coreai.Config, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfgLoaded {
		return c.cfg, nil
	}
	v, err := loadAIConfig(ctx, q, cfg)
	if err != nil {
		return coreai.Config{}, err
	}
	c.cfg, c.cfgLoaded = v, true
	return v, nil
}

func loadTemplates(ctx context.Context, q store.Querier) (coreai.StoredPromptTemplates, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT "value" FROM "AppSetting" WHERE "key" = ?`, AITemplatesSettingKey).Scan(&raw)
	if store.IsNoRows(err) {
		return coreai.StoredPromptTemplates{}, nil
	}
	if err != nil {
		return nil, err
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return coreai.StoredPromptTemplates{}, nil
	}
	return coreai.ParseStoredTemplates(v), nil
}

// Templates 管理员改过的默认要求模板（只含改过的项，缓存）。
func (c *AIConfigCache) Templates(ctx context.Context, q store.Querier) (coreai.StoredPromptTemplates, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tplLoaded {
		return c.tpl, nil
	}
	v, err := loadTemplates(ctx, q)
	if err != nil {
		return nil, err
	}
	c.tpl, c.tplLoaded = v, true
	return v, nil
}

// Template 预览时用的要求模板：自定义优先，否则默认。
func (c *AIConfigCache) Template(ctx context.Context, q store.Querier, key coreai.PromptKey) (string, error) {
	stored, err := c.Templates(ctx, q)
	if err != nil {
		return "", err
	}
	return coreai.ResolveTemplate(key, stored).Text, nil
}

// AIEnabled AI 当前是否可用（数据库配置优先，其次环境变量）。/config 的 ai 字段用这个。
func (c *AIConfigCache) AIEnabled(ctx context.Context, q store.Querier, cfg *config.Config) bool {
	v, err := c.Config(ctx, q, cfg)
	if err != nil {
		return false
	}
	return v.Provider != coreai.ProviderNone
}

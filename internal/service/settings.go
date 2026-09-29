// 系统设置：AI 接口与 AI 提示词的默认要求模板（对应旧 services/settings.ts）。
// 保存 / 恢复后刷新进程内缓存（AIConfigCache.Invalidate），下一次 AI 调用和 /config 立即使用新配置。
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// GetAISettings GET /settings/ai。
func GetAISettings(ctx context.Context, cache *AIConfigCache, q store.Querier, cfg *config.Config) (coreai.SettingsView, error) {
	c, err := cache.Config(ctx, q, cfg)
	if err != nil {
		return coreai.SettingsView{}, err
	}
	return coreai.ToSettingsView(c), nil
}

func candidateOf(ctx context.Context, cache *AIConfigCache, q store.Querier, cfg *config.Config, input coreai.SettingsInput) (coreai.Candidate, error) {
	current, err := cache.Config(ctx, q, cfg)
	if err != nil {
		return coreai.Candidate{}, err
	}
	candidate := coreai.BuildCandidate(input, current)
	if msg := coreai.ValidateCandidate(candidate); msg != "" {
		return coreai.Candidate{}, httpx.Validation(msg)
	}
	return candidate, nil
}

// SaveAISettings PUT /settings/ai。
func SaveAISettings(ctx context.Context, cache *AIConfigCache, db *store.DB, cfg *config.Config, now time.Time, input coreai.SettingsInput, actorID string) (coreai.SettingsView, error) {
	c, err := candidateOf(ctx, cache, db, cfg, input)
	if err != nil {
		return coreai.SettingsView{}, err
	}
	apiKeyEnc := ""
	if c.APIKey != "" {
		enc, err := sealSecret(cfg, c.APIKey)
		if err != nil {
			return coreai.SettingsView{}, err
		}
		apiKeyEnc = enc
	}
	value := map[string]any{"provider": c.Provider, "model": c.Model, "timeoutMs": c.TimeoutMs}
	if c.BaseURL != nil {
		value["baseUrl"] = *c.BaseURL
	} else {
		value["baseUrl"] = nil
	}
	if apiKeyEnc != "" {
		value["apiKeyEnc"] = apiKeyEnc
	} else {
		value["apiKeyEnc"] = nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return coreai.SettingsView{}, err
	}
	if err := db.Tx(ctx, func(tx *sql.Tx) error {
		return upsertAppSetting(ctx, tx, now, AISettingKey, string(raw), actorID)
	}); err != nil {
		return coreai.SettingsView{}, err
	}
	cache.Invalidate()
	return GetAISettings(ctx, cache, db, cfg)
}

// ResetAISettings DELETE /settings/ai：恢复为环境变量。
func ResetAISettings(ctx context.Context, cache *AIConfigCache, db *store.DB, cfg *config.Config) (coreai.SettingsView, error) {
	if err := db.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM "AppSetting" WHERE "key" = ?`, AISettingKey)
		return err
	}); err != nil {
		return coreai.SettingsView{}, err
	}
	cache.Invalidate()
	return GetAISettings(ctx, cache, db, cfg)
}

// TestAISettings POST /settings/ai/test：用填写的值测试连接，不保存。
func TestAISettings(ctx context.Context, cache *AIConfigCache, q store.Querier, cfg *config.Config, input coreai.SettingsInput) (coreai.TestResult, error) {
	c, err := candidateOf(ctx, cache, q, cfg, input)
	if err != nil {
		return coreai.TestResult{}, err
	}
	if c.Provider == coreai.SettingOff {
		return coreai.TestResult{}, httpx.Validation("供应商为「关闭」，无需测试")
	}
	ok, ms, errMsg := TestAIConnection(ctx, c.ToCallConfig())
	return coreai.TestResult{OK: ok, MS: ms, Error: errMsg}, nil
}

// ---------------------------------------------------------------------------
// AI 提示词的默认要求模板：不含输出格式，不做格式检查
// ---------------------------------------------------------------------------

// GetAIPrompts GET /settings/ai/prompts。
func GetAIPrompts(ctx context.Context, cache *AIConfigCache, q store.Querier) (coreai.PromptsView, error) {
	tpl, err := cache.Templates(ctx, q)
	if err != nil {
		return coreai.PromptsView{}, err
	}
	return coreai.ToPromptsView(tpl), nil
}

func writeTemplates(ctx context.Context, db *store.DB, cache *AIConfigCache, now time.Time, next coreai.StoredPromptTemplates, actorID string) error {
	if err := db.Tx(ctx, func(tx *sql.Tx) error {
		if len(next) == 0 {
			_, err := tx.ExecContext(ctx, `DELETE FROM "AppSetting" WHERE "key" = ?`, AITemplatesSettingKey)
			return err
		}
		raw, err := json.Marshal(next)
		if err != nil {
			return err
		}
		return upsertAppSetting(ctx, tx, now, AITemplatesSettingKey, string(raw), actorID)
	}); err != nil {
		return err
	}
	cache.Invalidate()
	return nil
}

// SaveAIPrompts PUT /settings/ai/prompts：只更新传了的项；任何一项不通过（空、超长）就整体拒绝。
func SaveAIPrompts(ctx context.Context, cache *AIConfigCache, db *store.DB, now time.Time, input coreai.StoredPromptTemplates, actorID string) (coreai.PromptsView, error) {
	updates := coreai.StoredPromptTemplates{}
	var errs []string
	for _, k := range coreai.PromptKeys {
		raw, ok := input[k]
		if !ok {
			continue
		}
		r := coreai.ValidatePromptText(raw)
		if r.OK {
			updates[k] = r.Value
		} else {
			errs = append(errs, coreai.PromptLabel[k]+"模板："+r.Message)
		}
	}
	if len(errs) > 0 {
		return coreai.PromptsView{}, httpx.Validation(joinSemi(errs))
	}
	stored, err := cache.Templates(ctx, db)
	if err != nil {
		return coreai.PromptsView{}, err
	}
	if err := writeTemplates(ctx, db, cache, now, coreai.MergeStoredTemplates(stored, updates), actorID); err != nil {
		return coreai.PromptsView{}, err
	}
	return GetAIPrompts(ctx, cache, db)
}

// ResetAIPrompt DELETE /settings/ai/prompts/:key：恢复某一项的默认模板。
func ResetAIPrompt(ctx context.Context, cache *AIConfigCache, db *store.DB, now time.Time, key coreai.PromptKey, actorID string) (coreai.PromptsView, error) {
	stored, err := cache.Templates(ctx, db)
	if err != nil {
		return coreai.PromptsView{}, err
	}
	next := coreai.StoredPromptTemplates{}
	for k, v := range stored {
		if k != key {
			next[k] = v
		}
	}
	if err := writeTemplates(ctx, db, cache, now, next, actorID); err != nil {
		return coreai.PromptsView{}, err
	}
	return GetAIPrompts(ctx, cache, db)
}

func joinSemi(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += "；"
		}
		out += x
	}
	return out
}

func upsertAppSetting(ctx context.Context, tx *sql.Tx, now time.Time, key, value, actorID string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO "AppSetting" ("key","value","updatedById","updatedAt") VALUES (?,?,?,?)
		ON CONFLICT("key") DO UPDATE SET "value"=excluded."value","updatedById"=excluded."updatedById","updatedAt"=excluded."updatedAt"`,
		key, value, actorID, store.NewTime(now))
	return err
}

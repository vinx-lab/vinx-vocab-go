package api

import (
	"net/http"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// 系统设置：只有管理员（system 能力）可用；个人版唯一账号即管理员，两个版本都注册（对应旧 routes/settings.ts）。

type aiSettingsBody struct {
	Provider    httpx.Opt[string]  `json:"provider"`
	BaseURL     httpx.Opt[string]  `json:"baseUrl"`
	APIKey      httpx.Opt[string]  `json:"apiKey"`
	ClearAPIKey httpx.Opt[bool]    `json:"clearApiKey"`
	Model       httpx.Opt[string]  `json:"model"`
	TimeoutSec  httpx.Opt[float64] `json:"timeoutSec"`

	provider    string
	baseURL     *string // nil = 未提交；空串 = 提交了空值
	apiKey      *string
	clearAPIKey bool
	model       *string
	timeoutSec  *int
}

func (b *aiSettingsBody) Validate(v *httpx.V) {
	b.provider = v.Enum("provider", b.Provider, coreai.SettingProviders, "")
	// baseUrl: z.string().trim().max(500).nullish() —— optional + nullable，去掉首尾空白
	if b.BaseURL.Set {
		if b.BaseURL.Null {
			empty := ""
			b.baseURL = &empty
		} else {
			s := v.NullStr("baseUrl", b.BaseURL, httpx.Trim(), httpx.Max(500))
			if s.Set && !s.Null {
				b.baseURL = &s.Val
			}
		}
	}
	if b.APIKey.Set && !b.APIKey.Null {
		s := v.OptStr("apiKey", b.APIKey, httpx.Max(1000))
		b.apiKey = s
	} else if b.APIKey.Set && b.APIKey.Null {
		v.Add("apiKey", "Expected string, received null")
	}
	b.clearAPIKey = v.OptBool("clearApiKey", b.ClearAPIKey, false)
	if b.Model.Set && !b.Model.Null {
		s := v.OptStr("model", b.Model, httpx.Trim(), httpx.Max(200))
		b.model = s
	} else if b.Model.Set && b.Model.Null {
		v.Add("model", "Expected string, received null")
	}
	if b.TimeoutSec.Set && !b.TimeoutSec.Null {
		n := v.Int("timeoutSec", b.TimeoutSec, nil, httpx.Between(coreai.TimeoutSecMin, coreai.TimeoutSecMax))
		b.timeoutSec = &n
	} else if b.TimeoutSec.Set && b.TimeoutSec.Null {
		v.Add("timeoutSec", "Expected number, received null")
	}
}

func (b aiSettingsBody) toInput() coreai.SettingsInput {
	return coreai.SettingsInput{
		Provider:    b.provider,
		BaseURL:     b.baseURL,
		APIKey:      b.apiKey,
		ClearAPIKey: b.clearAPIKey,
		Model:       b.model,
		TimeoutSec:  b.timeoutSec,
	}
}

type aiPromptsBody struct {
	Example httpx.Opt[string] `json:"example"`
	Passage httpx.Opt[string] `json:"passage"`
}

func (b *aiPromptsBody) Validate(v *httpx.V) {
	if !b.Example.Set && !b.Passage.Set {
		v.Add("body", "至少提交一项提示词")
		return
	}
	// 长度上限和非空检查在 internal/core/ai（去掉首尾空白后判断）；这里只挡明显过大的请求体。
	if b.Example.Set && b.Example.Null {
		v.Add("example", "Expected string, received null")
	}
	if b.Passage.Set && b.Passage.Null {
		v.Add("passage", "Expected string, received null")
	}
	if b.Example.Set && !b.Example.Null {
		v.OptStr("example", b.Example, httpx.Max(20_000))
	}
	if b.Passage.Set && !b.Passage.Null {
		v.OptStr("passage", b.Passage, httpx.Max(20_000))
	}
}

func (b aiPromptsBody) toInput() coreai.StoredPromptTemplates {
	out := coreai.StoredPromptTemplates{}
	if b.Example.Set && !b.Example.Null {
		out[coreai.PromptExample] = b.Example.Val
	}
	if b.Passage.Set && !b.Passage.Null {
		out[coreai.PromptPassage] = b.Passage.Val
	}
	return out
}

func registerSettings(r *Router, d *Deps) {
	guard := auth.RequireCap(core.CapSystem)

	r.Get("/settings/ai", func(w http.ResponseWriter, req *http.Request) error {
		v, err := service.GetAISettings(req.Context(), d.AIConfig, d.DB, d.Cfg)
		if err != nil {
			return err
		}
		httpx.OK(w, v)
		return nil
	}, guard)

	r.Put("/settings/ai", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[aiSettingsBody](req)
		if err != nil {
			return err
		}
		v, err := service.SaveAISettings(ctx, d.AIConfig, d.DB, d.Cfg, d.Now(), body.toInput(), actor.ID)
		if err != nil {
			return err
		}
		httpx.OK(w, v)
		return nil
	}, guard)

	r.Delete("/settings/ai", func(w http.ResponseWriter, req *http.Request) error {
		v, err := service.ResetAISettings(req.Context(), d.AIConfig, d.DB, d.Cfg)
		if err != nil {
			return err
		}
		httpx.OK(w, v)
		return nil
	}, guard)

	r.Post("/settings/ai/test", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		body, err := httpx.Decode[aiSettingsBody](req)
		if err != nil {
			return err
		}
		v, err := service.TestAISettings(ctx, d.AIConfig, d.DB, d.Cfg, body.toInput())
		if err != nil {
			return err
		}
		httpx.OK(w, v)
		return nil
	}, guard)

	// AI 提示词的默认要求模板。
	r.Get("/settings/ai/prompts", func(w http.ResponseWriter, req *http.Request) error {
		v, err := service.GetAIPrompts(req.Context(), d.AIConfig, d.DB)
		if err != nil {
			return err
		}
		httpx.OK(w, v)
		return nil
	}, guard)

	r.Put("/settings/ai/prompts", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[aiPromptsBody](req)
		if err != nil {
			return err
		}
		v, err := service.SaveAIPrompts(ctx, d.AIConfig, d.DB, d.Now(), body.toInput(), actor.ID)
		if err != nil {
			return err
		}
		httpx.OK(w, v)
		return nil
	}, guard)

	r.Delete("/settings/ai/prompts/{key}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		key := req.PathValue("key")
		vv := &httpx.V{}
		vv.Enum("key", httpx.Some(key), []string{coreai.PromptExample, coreai.PromptPassage}, "")
		if err := vv.Err(); err != nil {
			return err
		}
		v, err := service.ResetAIPrompt(ctx, d.AIConfig, d.DB, d.Now(), key, actor.ID)
		if err != nil {
			return err
		}
		httpx.OK(w, v)
		return nil
	}, guard)
}

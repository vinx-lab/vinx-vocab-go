package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// AI 生成：句型、仿写、重写一句（spec 0005）。
//
//	POST /ai/units/:id/patterns/preview   句型：预览（输入、学段、可见提示词）
//	POST /ai/units/:id/patterns           句型：开始生成，返回任务 id；草稿在任务结果里
//	POST /ai/units/:id/patterns/save      保存句型草稿（新建 list 篇或追加到已有的句型清单）
//	POST /ai/variants/preview             仿写：预览
//	POST /ai/variants                     仿写：开始生成
//	POST /ai/variants/save                保存仿写草稿
//	POST /ai/sentences/rewrite            重写一句（同步，超时不超过 60 秒）
//
// 权限：系统词书只有管理员，自己的词书老师（books.edit），即 AssertCanEditBook；学生没有 books.edit。
// 重写一句：短文的句子学生也可以用（study），其余同上。

// ---------------------------------------------------------------------------
// 请求体
// ---------------------------------------------------------------------------

// validatePattern 句型预览 / 生成共用的校验（两个请求体字段相同；不用匿名嵌入：httpx.Decode 按顶层键名逐字段匹配）。
func validatePattern(v *httpx.V, topic httpx.Opt[string], count httpx.Opt[float64], level httpx.Opt[string], prompt httpx.Opt[string], requireTopic bool) service.PatternOptions {
	var o service.PatternOptions
	if requireTopic {
		o.Topic = v.Str("topic", topic, httpx.Trim(), httpx.Min(1, "请填写话题或语法点"), httpx.Max(100, "话题过长"))
	} else if t := v.OptStr("topic", topic, httpx.Trim(), httpx.Max(100, "话题过长")); t != nil {
		o.Topic = *t
	}
	def := service.PatternCountDefault
	o.Count = v.Int("count", count, &def, httpx.Between(service.PatternCountMin, service.PatternCountMax))
	o.Level = optLevel(v, level)
	o.Prompt = v.OptStr("prompt", prompt, httpx.Max(20_000))
	return o
}

// patternPreviewBody 预览：话题可省（用单元名预填）。
type patternPreviewBody struct {
	Topic  httpx.Opt[string]  `json:"topic"`
	Count  httpx.Opt[float64] `json:"count"`
	Level  httpx.Opt[string]  `json:"level"`
	Prompt httpx.Opt[string]  `json:"prompt"`

	opts service.PatternOptions
}

func (b *patternPreviewBody) Validate(v *httpx.V) {
	b.opts = validatePattern(v, b.Topic, b.Count, b.Level, b.Prompt, false)
}

// patternGenerateBody 生成：话题必填。
type patternGenerateBody struct {
	Topic  httpx.Opt[string]  `json:"topic"`
	Count  httpx.Opt[float64] `json:"count"`
	Level  httpx.Opt[string]  `json:"level"`
	Prompt httpx.Opt[string]  `json:"prompt"`

	opts service.PatternOptions
}

func (b *patternGenerateBody) Validate(v *httpx.V) {
	b.opts = validatePattern(v, b.Topic, b.Count, b.Level, b.Prompt, true)
}

type draftSentenceBody struct {
	En          httpx.Opt[string] `json:"en"`
	Cn          httpx.Opt[string] `json:"cn"`
	Frame       httpx.Opt[string] `json:"frame"`
	OriginID    httpx.Opt[string] `json:"originId"`
	VariantNote httpx.Opt[string] `json:"variantNote"`
	Change      httpx.Opt[string] `json:"change"`
}

const draftSentenceLimit = 100

// validateDraftSentences 草稿句子：至少 1 句、最多 100 句；variant 为真时读 originId / variantNote / change，否则读 frame。
func validateDraftSentences(v *httpx.V, o httpx.Opt[[]draftSentenceBody], variant bool) []service.AISentenceInput {
	if !o.Set {
		v.Add("sentences", "Required")
		return nil
	}
	if o.Null {
		v.Add("sentences", "Expected array, received null")
		return nil
	}
	v.ArrayLen("sentences", len(o.Val), 1, draftSentenceLimit, "至少保存一句", "")
	out := make([]service.AISentenceInput, 0, len(o.Val))
	for i, s := range o.Val {
		p := fmt.Sprintf("sentences.%d", i)
		in := service.AISentenceInput{
			En: v.Str(p+".en", s.En, httpx.Trim(), httpx.Min(1, "英文不能为空"), httpx.Max(1000, "英文过长")),
			Cn: v.Str(p+".cn", s.Cn, httpx.Trim(), httpx.Min(1, "中文不能为空"), httpx.Max(1000, "中文过长")),
		}
		if variant {
			if id := v.NullStr(p+".originId", s.OriginID, httpx.Trim()); id.Set && !id.Null && id.Val != "" {
				x := id.Val
				in.OriginID = &x
			}
			note := ""
			if n := v.NullStr(p+".variantNote", s.VariantNote, httpx.Trim(), httpx.Max(200, "说明过长")); n.Set && !n.Null {
				note = n.Val
			}
			change := ""
			if s.Change.Set && !s.Change.Null && s.Change.Val != "" {
				change = v.Enum(p+".change", s.Change, coreai.VariantModes, "")
			}
			in.VariantNote = service.VariantNoteOf(change, note)
		} else if f := v.NullStr(p+".frame", s.Frame, httpx.Trim(), httpx.Max(1000, "句型骨架过长")); f.Set && !f.Null && f.Val != "" {
			x := f.Val
			in.Frame = &x
		}
		out = append(out, in)
	}
	return out
}

type patternSaveBody struct {
	TextID    httpx.Opt[string]              `json:"textId"`
	Title     httpx.Opt[string]              `json:"title"`
	Sentences httpx.Opt[[]draftSentenceBody] `json:"sentences"`

	textID    *string
	title     *string
	sentences []service.AISentenceInput
}

func (b *patternSaveBody) Validate(v *httpx.V) {
	b.textID = v.OptStr("textId", b.TextID, httpx.Trim(), httpx.Min(1))
	b.title = v.OptStr("title", b.Title, httpx.Trim(), httpx.Min(1, "标题不能为空"), httpx.Max(100, "标题过长"))
	b.sentences = validateDraftSentences(v, b.Sentences, false)
}

type variantBody struct {
	UnitID      httpx.Opt[string]   `json:"unitId"`
	SentenceIDs httpx.Opt[[]string] `json:"sentenceIds"`
	Pasted      httpx.Opt[string]   `json:"pasted"`
	Modes       httpx.Opt[[]string] `json:"modes"`
	PerItem     httpx.Opt[float64]  `json:"perItem"`
	Vocab       httpx.Opt[string]   `json:"vocab"`
	Level       httpx.Opt[string]   `json:"level"`
	Prompt      httpx.Opt[string]   `json:"prompt"`

	unitID string
	opts   service.VariantOptions
}

func (b *variantBody) Validate(v *httpx.V) {
	b.unitID = v.Str("unitId", b.UnitID, httpx.Min(1))
	if b.SentenceIDs.Set {
		if b.SentenceIDs.Null {
			v.Add("sentenceIds", "Expected array, received null")
		} else {
			b.opts.SentenceIDs = b.SentenceIDs.Val
			v.ArrayLen("sentenceIds", len(b.SentenceIDs.Val), -1, service.VariantOriginLimit, "", "")
		}
	}
	if p := v.OptStr("pasted", b.Pasted); p != nil {
		if len(*p) > service.PastedMaxBytes {
			v.Add("pasted", "粘贴或上传的内容不能超过 20KB")
		}
		b.opts.Pasted = *p
	}
	switch {
	case !b.Modes.Set:
		v.Add("modes", "Required")
	case b.Modes.Null:
		v.Add("modes", "Expected array, received null")
	default:
		v.ArrayLen("modes", len(b.Modes.Val), 1, len(coreai.VariantModes), "请至少选择一种改造方式", "")
		seen := map[string]bool{}
		for i, m := range b.Modes.Val {
			if x := v.Enum(fmt.Sprintf("modes.%d", i), httpx.Some(m), coreai.VariantModes, ""); x != "" && !seen[x] {
				seen[x] = true
				b.opts.Modes = append(b.opts.Modes, x)
			}
		}
	}
	def := service.VariantPerItemDefault
	b.opts.PerItem = v.Int("perItem", b.PerItem, &def, httpx.Between(service.VariantPerItemMin, service.VariantPerItemMax))
	b.opts.Vocab = v.OptEnum("vocab", b.Vocab, []string{service.VariantVocabUnit, service.VariantVocabTarget}, "", service.VariantVocabUnit)
	b.opts.Level = optLevel(v, b.Level)
	b.opts.Prompt = v.OptStr("prompt", b.Prompt, httpx.Max(20_000))
}

type variantSaveBody struct {
	UnitID    httpx.Opt[string]              `json:"unitId"`
	TextID    httpx.Opt[string]              `json:"textId"`
	Title     httpx.Opt[string]              `json:"title"`
	Sentences httpx.Opt[[]draftSentenceBody] `json:"sentences"`

	unitID    string
	textID    *string
	title     *string
	sentences []service.AISentenceInput
}

func (b *variantSaveBody) Validate(v *httpx.V) {
	b.unitID = v.Str("unitId", b.UnitID, httpx.Min(1))
	b.textID = v.OptStr("textId", b.TextID, httpx.Trim(), httpx.Min(1))
	b.title = v.OptStr("title", b.Title, httpx.Trim(), httpx.Min(1, "标题不能为空"), httpx.Max(100, "标题过长"))
	b.sentences = validateDraftSentences(v, b.Sentences, true)
}

// rewriteBody POST /ai/sentences/rewrite 的请求体：{ en, cn?, issues?, level?, kind, unitId?, origin?, target? }。
// unitId 可选：带上时按单元算已知词做超纲检查；不带时，kind=passage 按操作者自己的学习记录，
// 其他类型（例句、句型、仿写）不做超纲检查（返回的 checks.outOfScope 为空），其余检查照常。
type rewriteBody struct {
	En     httpx.Opt[string]   `json:"en"`
	Cn     httpx.Opt[string]   `json:"cn"`
	Issues httpx.Opt[[]string] `json:"issues"`
	Level  httpx.Opt[string]   `json:"level"`
	Kind   httpx.Opt[string]   `json:"kind"`
	UnitID httpx.Opt[string]   `json:"unitId"`
	Origin httpx.Opt[string]   `json:"origin"`
	Target httpx.Opt[string]   `json:"target"`

	unitID *string
	opts   service.RewriteOptions
}

func (b *rewriteBody) Validate(v *httpx.V) {
	b.opts.En = v.Str("en", b.En, httpx.Trim(), httpx.Min(1, "英文不能为空"), httpx.Max(1000, "英文过长"))
	if c := v.OptStr("cn", b.Cn, httpx.Trim(), httpx.Max(1000, "中文过长")); c != nil {
		b.opts.Cn = *c
	}
	if b.Issues.Set {
		if b.Issues.Null {
			v.Add("issues", "Expected array, received null")
		} else {
			v.ArrayLen("issues", len(b.Issues.Val), -1, 20, "", "")
			b.opts.Issues = b.Issues.Val
		}
	}
	b.opts.Level = optLevel(v, b.Level)
	b.opts.Kind = v.Enum("kind", b.Kind, coreai.PromptKeys, "")
	b.unitID = v.OptStr("unitId", b.UnitID, httpx.Min(1))
	if o := v.OptStr("origin", b.Origin, httpx.Trim(), httpx.Max(1000)); o != nil {
		b.opts.Origin = *o
	}
	if t := v.OptStr("target", b.Target, httpx.Trim(), httpx.Max(100)); t != nil {
		b.opts.Target = *t
	}
}

// ---------------------------------------------------------------------------
// 工具
// ---------------------------------------------------------------------------

// aiCallError 同步调用 AI 的错误转成接口错误（业务错误码保留，其余 500；说明去掉 Key）。
func aiCallError(ctx context.Context, d *Deps, err error) error {
	var he *httpx.Error
	if errors.As(err, &he) {
		return err
	}
	cfg, cfgErr := d.AIConfig.Config(ctx, d.DB, d.Cfg)
	ectx := coreai.ErrorContext{}
	if cfgErr == nil {
		ectx = coreai.ErrorContext{TimeoutMs: cfg.TimeoutMs, APIKey: cfg.APIKey}
	}
	ae := coreai.ToAPIError(err, ectx)
	out := httpx.NewError(ae.Code, ae.Message, ae.Details)
	if ae.Status != 0 {
		out = out.WithStatus(ae.Status)
	}
	return out
}

// currentModel 保存草稿时记录的模型名（当前 AI 配置；没有时为 nil）。
func currentModel(ctx context.Context, d *Deps) *string {
	cfg, err := d.AIConfig.Config(ctx, d.DB, d.Cfg)
	if err != nil || cfg.Model == "" {
		return nil
	}
	m := cfg.Model
	return &m
}

// saveDraft 事务里保存草稿，返回保存后的篇。
func saveDraft(ctx context.Context, d *Deps, unit *service.UnitRow, in service.SaveDraftInput) (*service.UnitTextView, error) {
	var id string
	err := d.DB.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = service.SaveDraft(ctx, tx, d.Now(), unit.ID, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return service.GetUnitText(ctx, d.DB, id)
}

// ---------------------------------------------------------------------------
// 路由
// ---------------------------------------------------------------------------

func registerAIGen(r *Router, d *Deps) {
	edit := auth.RequireCap(core.CapBooksEdit)

	// ===== 句型 =====
	r.Post("/ai/units/{id}/patterns/preview", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		unit, err := editableUnit(req, d, req.PathValue("id"))
		if err != nil {
			return err
		}
		body, err := httpx.Decode[patternPreviewBody](req)
		if err != nil {
			return err
		}
		p, err := service.PreviewPatterns(ctx, d.AIConfig, d.DB, unit, body.opts)
		if err != nil {
			return err
		}
		httpx.OK(w, p)
		return nil
	}, edit)

	r.Post("/ai/units/{id}/patterns", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		unit, err := editableUnit(req, d, req.PathValue("id"))
		if err != nil {
			return err
		}
		body, err := httpx.Decode[patternGenerateBody](req)
		if err != nil {
			return err
		}
		opts := body.opts
		if opts.Prompt, err = visiblePrompt(opts.Prompt); err != nil {
			return err
		}
		if err := assertAiEnabled(ctx, d); err != nil {
			return err
		}
		started, err := startAIJob(d, actor.ID, coreai.JobKindPatterns, func(ctx context.Context) (service.PatternDraft, error) {
			cfg, err := d.AIConfig.Config(ctx, d.DB, d.Cfg)
			if err != nil {
				return service.PatternDraft{}, err
			}
			return service.GeneratePatterns(ctx, d.AIConfig, d.DB, cfg.ToCallConfig(), unit, opts)
		})
		if err != nil {
			return err
		}
		httpx.OK(w, started)
		return nil
	}, edit)

	r.Post("/ai/units/{id}/patterns/save", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		unit, err := editableUnit(req, d, req.PathValue("id"))
		if err != nil {
			return err
		}
		body, err := httpx.Decode[patternSaveBody](req)
		if err != nil {
			return err
		}
		title := service.PatternTitle(unit.Name)
		if body.title != nil {
			title = *body.title
		}
		view, err := saveDraft(ctx, d, unit, service.SaveDraftInput{
			Source: service.SentenceAI, TextID: body.textID, Title: title, Sentences: body.sentences, ActorID: actor.ID, Model: currentModel(ctx, d),
		})
		if err != nil {
			return err
		}
		httpx.OK(w, view)
		return nil
	}, edit)

	// ===== 仿写 =====
	r.Post("/ai/variants/preview", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[variantBody](req)
		if err != nil {
			return err
		}
		unit, err := editableUnit(req, d, body.unitID)
		if err != nil {
			return err
		}
		p, err := service.PreviewVariants(ctx, d.AIConfig, d.DB, actor, unit, body.opts)
		if err != nil {
			return err
		}
		httpx.OK(w, p)
		return nil
	}, edit)

	r.Post("/ai/variants", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[variantBody](req)
		if err != nil {
			return err
		}
		unit, err := editableUnit(req, d, body.unitID)
		if err != nil {
			return err
		}
		opts := body.opts
		if opts.Prompt, err = visiblePrompt(opts.Prompt); err != nil {
			return err
		}
		job, err := service.PrepareVariantJob(ctx, d.AIConfig, d.DB, actor, unit, opts)
		if err != nil {
			return err
		}
		if err := assertAiEnabled(ctx, d); err != nil {
			return err
		}
		started, err := startAIJob(d, actor.ID, coreai.JobKindVariants, func(ctx context.Context) (service.VariantDraft, error) {
			cfg, err := d.AIConfig.Config(ctx, d.DB, d.Cfg)
			if err != nil {
				return service.VariantDraft{}, err
			}
			return service.GenerateVariants(ctx, d.DB, cfg.ToCallConfig(), unit, job)
		})
		if err != nil {
			return err
		}
		httpx.OK(w, started)
		return nil
	}, edit)

	r.Post("/ai/variants/save", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[variantSaveBody](req)
		if err != nil {
			return err
		}
		unit, err := editableUnit(req, d, body.unitID)
		if err != nil {
			return err
		}
		title := service.VariantTitle(unit.Name)
		if body.title != nil {
			title = *body.title
		}
		view, err := saveDraft(ctx, d, unit, service.SaveDraftInput{
			Source: service.SentenceVariant, TextID: body.textID, Title: title, Sentences: body.sentences, ActorID: actor.ID, Model: currentModel(ctx, d),
		})
		if err != nil {
			return err
		}
		httpx.OK(w, view)
		return nil
	}, edit)

	// ===== 重写一句 =====
	r.Post("/ai/sentences/rewrite", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[rewriteBody](req)
		if err != nil {
			return err
		}
		opts := body.opts
		if opts.Kind == coreai.PromptPassage {
			if err := service.AssertCan(actor, core.CapStudy, ""); err != nil {
				return err
			}
		} else if err := service.AssertCan(actor, core.CapBooksEdit, ""); err != nil {
			return err
		}
		if body.unitID != nil {
			if opts.Kind == coreai.PromptPassage {
				u, err := service.VisibleUnit(ctx, d.DB, actor, *body.unitID)
				if err != nil {
					return err
				}
				if u == nil {
					return httpx.NotFound("单元不存在")
				}
				opts.Unit = u
			} else if opts.Unit, err = editableUnit(req, d, *body.unitID); err != nil {
				return err
			}
		}
		if err := assertAiEnabled(ctx, d); err != nil {
			return err
		}
		cfg, err := d.AIConfig.Config(ctx, d.DB, d.Cfg)
		if err != nil {
			return err
		}
		res, err := service.RewriteSentence(ctx, d.DB, cfg.ToCallConfig(), actor.ID, opts)
		if err != nil {
			return aiCallError(ctx, d, err)
		}
		httpx.OK(w, res)
		return nil
	}, auth.RequireAuth)
}

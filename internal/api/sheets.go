package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 单词单（对应旧 routes/sheets.ts）：预览、批量生成、列表、明细、删除、今日下一份（NextSheet 在 study.go 的 /today 里用）。

var sheetSourceKindValues = []string{"unfamiliar", "session", "unit", "book"}

// sheetSourceTarget 目标词书来源（spec 0003）：{ kind: "target", status: "untested" | "learning", bookId? }。
// 不放进 sheetSourceKindValues：无效 kind 的错误信息沿用原有的四个取值（契约测试固定了原文）。
const sheetSourceTarget = "target"

// ===== 请求体 =====

// sheetSourceBody 选词来源（旧 discriminatedUnion("kind", [...])）。
type sheetSourceBody struct {
	Kind      httpx.Opt[string] `json:"kind"`
	SessionID httpx.Opt[string] `json:"sessionId"`
	UnitID    httpx.Opt[string] `json:"unitId"`
	BookID    httpx.Opt[string] `json:"bookId"`
	Status    httpx.Opt[string] `json:"status"`
}

// validateSheetSource 手动实现 zod discriminatedUnion 的行为：kind 不在枚举里（含缺省、null、类型错）
// 统一报 "Invalid discriminator value" 于 "<path>.kind"，覆盖解码阶段可能记录的类型错误；
// kind 合法时按对应分支校验专属字段（sessionId / unitId / bookId，z.string().min(1)）。
func validateSheetSource(v *httpx.V, path string, o httpx.Opt[sheetSourceBody]) *service.SheetSource {
	if !o.Set {
		return nil
	}
	if v.Has(path) {
		// 解码阶段已记录 "source" 级别的类型错误（如 Expected object, received string）
		return nil
	}
	if o.Null {
		v.Add(path, "Expected object, received null")
		return nil
	}
	b := o.Val
	kindPath := path + ".kind"
	kind := ""
	if b.Kind.Set && !b.Kind.Null {
		kind = b.Kind.Val
	}
	if !b.Kind.Set || b.Kind.Null || (!slices.Contains(sheetSourceKindValues, kind) && kind != sheetSourceTarget) {
		v.Add(kindPath, "Invalid discriminator value. Expected 'unfamiliar' | 'session' | 'unit' | 'book'")
		return nil
	}
	switch kind {
	case sheetSourceTarget:
		src := &service.SheetSource{Kind: kind, Status: v.Enum(path+".status", b.Status, service.SheetTargetStatuses, "")}
		if s := v.OptStr(path+".bookId", b.BookID, httpx.Min(1)); s != nil {
			src.BookID = *s
		}
		return src
	case "session":
		return &service.SheetSource{Kind: kind, SessionID: v.Str(path+".sessionId", b.SessionID, httpx.Min(1))}
	case "unit":
		return &service.SheetSource{Kind: kind, UnitID: v.Str(path+".unitId", b.UnitID, httpx.Min(1))}
	case "book":
		return &service.SheetSource{Kind: kind, BookID: v.Str(path+".bookId", b.BookID, httpx.Min(1))}
	default:
		return &service.SheetSource{Kind: kind}
	}
}

// ===== 默写单（spec 0006）=====

// sentenceSourceBody 默写单的句子来源：{ kind: "text", textId } | { kind: "learning" } | { kind: "passage", passageId } | { kind: "session", sessionId }。
type sentenceSourceBody struct {
	Kind      httpx.Opt[string] `json:"kind"`
	TextID    httpx.Opt[string] `json:"textId"`
	PassageID httpx.Opt[string] `json:"passageId"`
	SessionID httpx.Opt[string] `json:"sessionId"`
}

// sentenceSourcesMax 一次最多选的句子来源。
const sentenceSourcesMax = 50

func validateSentenceSources(v *httpx.V, path string, o httpx.Opt[[]sentenceSourceBody]) []service.SentenceSource {
	if !o.Set || v.Has(path) {
		return nil
	}
	if o.Null {
		v.Add(path, "Expected array, received null")
		return nil
	}
	v.ArrayLen(path, len(o.Val), -1, sentenceSourcesMax, "", "")
	out := make([]service.SentenceSource, 0, len(o.Val))
	for i, s := range o.Val {
		p := fmt.Sprintf("%s.%d", path, i)
		src := service.SentenceSource{Kind: v.Enum(p+".kind", s.Kind, service.SentenceSourceKinds, "")}
		switch src.Kind {
		case service.SentenceSourceText:
			src.TextID = v.Str(p+".textId", s.TextID, httpx.Min(1))
		case service.SentenceSourcePassage:
			src.PassageID = v.Str(p+".passageId", s.PassageID, httpx.Min(1))
		case service.SentenceSourceSession:
			src.SessionID = v.Str(p+".sessionId", s.SessionID, httpx.Min(1))
		}
		out = append(out, src)
	}
	return out
}

// dictLimits 每份各题型的数量上限 wordCount / phraseCount / sentenceCount（默认 单词 20、短语 10、句子 8）。
func dictLimits(v *httpx.V, words, phrases, sentences httpx.Opt[float64]) core.DictationLimits {
	w, p, s := core.DictationWordDefault, core.DictationPhraseDefault, core.DictationSentenceDefault
	return core.DictationLimits{
		Words:     v.Int("wordCount", words, &w, httpx.Between(0, core.DictationWordMax)),
		Phrases:   v.Int("phraseCount", phrases, &p, httpx.Between(0, core.DictationPhraseMax)),
		Sentences: v.Int("sentenceCount", sentences, &s, httpx.Between(0, core.DictationSentenceMax)),
	}
}

// dictItemBody 默写单的一道题。
type dictItemBody struct {
	Type       httpx.Opt[string] `json:"type"`
	WordID     httpx.Opt[string] `json:"wordId"`
	SentenceID httpx.Opt[string] `json:"sentenceId"`
}

// dictItemsMax 一次生成的题目上限 = 份数 × 各题型每份上限之和。
const dictItemsMax = core.SheetCopiesMax * (core.DictationWordMax + core.DictationPhraseMax + core.DictationSentenceMax)

func validateDictItems(v *httpx.V, path string, o httpx.Opt[[]dictItemBody]) []core.DictationItem {
	if v.Has(path) {
		return nil
	}
	if !o.Set {
		v.Add(path, "Required")
		return nil
	}
	if o.Null {
		v.Add(path, "Expected array, received null")
		return nil
	}
	v.ArrayLen(path, len(o.Val), 1, dictItemsMax, "至少选一道题", "")
	out := make([]core.DictationItem, 0, len(o.Val))
	for i, it := range o.Val {
		p := fmt.Sprintf("%s.%d", path, i)
		item := core.DictationItem{Type: v.Enum(p+".type", it.Type, core.DictationItemTypes, "")}
		switch {
		case item.Type == "":
		case core.IsSentenceItem(item.Type):
			item.SentenceID = v.Str(p+".sentenceId", it.SentenceID, httpx.Min(1))
		default:
			item.WordID = v.Str(p+".wordId", it.WordID, httpx.Min(1))
		}
		out = append(out, item)
	}
	return out
}

// gradeItemBody 一道题的批改结果。
type gradeItemBody struct {
	Index      httpx.Opt[float64] `json:"index"`
	Correct    httpx.Opt[bool]    `json:"correct"`
	UserAnswer httpx.Opt[string]  `json:"userAnswer"`
}

// sheetGradeBody POST /sheets/:id/grade { results: [{ index, correct, userAnswer? }] }。
type sheetGradeBody struct {
	Results httpx.Opt[[]gradeItemBody] `json:"results"`

	results []core.GradeResult
}

func (b *sheetGradeBody) Validate(v *httpx.V) {
	if v.Has("results") {
		return
	}
	if !b.Results.Set {
		v.Add("results", "Required")
		return
	}
	if b.Results.Null {
		v.Add("results", "Expected array, received null")
		return
	}
	v.ArrayLen("results", len(b.Results.Val), 1, dictItemsMax, "", "")
	for i, r := range b.Results.Val {
		p := fmt.Sprintf("results.%d", i)
		g := core.GradeResult{Index: v.Int(p+".index", r.Index, nil, httpx.Between(0, dictItemsMax))}
		switch {
		case v.Has(p + ".correct"):
		case !r.Correct.Set:
			v.Add(p+".correct", "Required")
		case r.Correct.Null:
			v.Add(p+".correct", "Expected boolean, received null")
		default:
			g.Correct = r.Correct.Val
		}
		if ua := v.NullStr(p+".userAnswer", r.UserAnswer, httpx.Max(500)); ua.Set && !ua.Null {
			s := ua.Val
			g.UserAnswer = &s
		}
		b.results = append(b.results, g)
	}
}

// sheetPreviewBody POST /sheets/preview。
type sheetPreviewBody struct {
	UserID  httpx.Opt[string]          `json:"userId"`
	Count   httpx.Opt[float64]         `json:"count"`
	Include httpx.Opt[[]string]        `json:"include"`
	Source  httpx.Opt[sheetSourceBody] `json:"source"`
	// 默写单（spec 0006）
	Format          httpx.Opt[string]               `json:"format"`
	Copies          httpx.Opt[float64]              `json:"copies"`
	IncludeWords    httpx.Opt[bool]                 `json:"includeWords"`
	IncludePhrases  httpx.Opt[bool]                 `json:"includePhrases"`
	SentenceSources httpx.Opt[[]sentenceSourceBody] `json:"sentenceSources"`
	WordCount       httpx.Opt[float64]              `json:"wordCount"`
	PhraseCount     httpx.Opt[float64]              `json:"phraseCount"`
	SentenceCount   httpx.Opt[float64]              `json:"sentenceCount"`

	userID  string
	count   int
	include []string
	source  *service.SheetSource
	format  string
	dict    service.DictationPreviewInput
}

func (b *sheetPreviewBody) Validate(v *httpx.V) {
	b.format = v.OptEnum("format", b.Format, core.SheetFormats, "", core.SheetFormatSelftest)
	one := 1
	b.dict.Copies = v.Int("copies", b.Copies, &one, httpx.Between(1, core.SheetCopiesMax))
	b.dict.Limits = dictLimits(v, b.WordCount, b.PhraseCount, b.SentenceCount)
	b.dict.IncludeWords = v.OptBool("includeWords", b.IncludeWords, true)
	b.dict.IncludePhrases = v.OptBool("includePhrases", b.IncludePhrases, true)
	b.dict.SentenceSources = validateSentenceSources(v, "sentenceSources", b.SentenceSources)
	if s := v.OptStr("userId", b.UserID); s != nil {
		b.userID = *s
	}
	def := core.SheetDefault
	b.count = v.Int("count", b.Count, &def, httpx.Between(core.SheetMin, core.SheetMax))
	if b.Include.Set {
		if b.Include.Null {
			v.Add("include", "Expected array, received null")
		} else {
			ids := b.Include.Val
			v.ArrayLen("include", len(ids), -1, core.SheetMax, "", "")
			b.include = ids
		}
	}
	b.source = validateSheetSource(v, "source", b.Source)
}

// sheetCreateBody POST /sheets。
type sheetCreateBody struct {
	UserID   httpx.Opt[string]   `json:"userId"`
	WordIDs  httpx.Opt[[]string] `json:"wordIds"`
	Modes    httpx.Opt[[]string] `json:"modes"`
	Copies   httpx.Opt[float64]  `json:"copies"`
	PerSheet httpx.Opt[float64]  `json:"perSheet"`
	// 默写单（spec 0006）：format = dictation 时用 items 与各题型每份上限，不用 wordIds / modes / perSheet。
	Format        httpx.Opt[string]         `json:"format"`
	Items         httpx.Opt[[]dictItemBody] `json:"items"`
	WordCount     httpx.Opt[float64]        `json:"wordCount"`
	PhraseCount   httpx.Opt[float64]        `json:"phraseCount"`
	SentenceCount httpx.Opt[float64]        `json:"sentenceCount"`

	userID           string
	wordIDs          []string
	modes            []string
	copies, perSheet int
	format           string
	items            []core.DictationItem
	limits           core.DictationLimits
}

func (b *sheetCreateBody) Validate(v *httpx.V) {
	if s := v.OptStr("userId", b.UserID); s != nil {
		b.userID = *s
	}
	b.format = v.OptEnum("format", b.Format, core.SheetFormats, "", core.SheetFormatSelftest)
	if b.format == core.SheetFormatDictation {
		b.items = validateDictItems(v, "items", b.Items)
		b.limits = dictLimits(v, b.WordCount, b.PhraseCount, b.SentenceCount)
		one := 1
		b.copies = v.Int("copies", b.Copies, &one, httpx.Between(1, core.SheetCopiesMax))
		return
	}
	if !b.WordIDs.Set {
		v.Add("wordIds", "Required")
	} else if b.WordIDs.Null {
		v.Add("wordIds", "Expected array, received null")
	} else {
		ids := b.WordIDs.Val
		v.ArrayLen("wordIds", len(ids), 1, core.SheetMax, "至少选一个单词", "")
		for i, id := range ids {
			v.Str(fmt.Sprintf("wordIds.%d", i), httpx.Some(id), httpx.Min(1))
		}
		b.wordIDs = ids
	}
	b.modes = validateModes(v, "modes", b.Modes, []string{"recognition", "spelling"})
	one := 1
	b.copies = v.Int("copies", b.Copies, &one, httpx.Between(1, core.SheetCopiesMax))
	perMax := core.SheetPerMax
	b.perSheet = v.Int("perSheet", b.PerSheet, &perMax, httpx.Between(1, core.SheetPerMax))
}

// ===== 查询参数 =====

type sheetsListQuery struct {
	Page  httpx.Opt[string] `json:"page"`
	Limit httpx.Opt[string] `json:"limit"`

	page, limit int
}

func (q *sheetsListQuery) Validate(v *httpx.V) {
	q.page, q.limit = pageLimit(v, q.Page, q.Limit)
}

// sheetSourcesQuery GET /sheets/sources 的 unitId（默写单选句子来源时列出该单元的篇）。
type sheetSourcesQuery struct {
	UnitID httpx.Opt[string] `json:"unitId"`

	unitID string
}

func (q *sheetSourcesQuery) Validate(v *httpx.V) {
	if s := v.OptStr("unitId", q.UnitID); s != nil {
		q.unitID = *s
	}
}

// ===== 权限 =====

// targetBodyUser 目标学生：请求体里的 userId（默认本人；给别人需能查看该学生，旧 target）。
func targetBodyUser(ctx context.Context, d *Deps, actor *service.Actor, userID string) (string, error) {
	t := userID
	if t == "" {
		t = actor.ID
	}
	if err := service.AssertCanViewUser(ctx, d.DB, actor, t); err != nil {
		return "", err
	}
	return t, nil
}

// assertSheetSource 来源的范围判定：错词来源只能用目标学生自己的学习组；单元 / 整本书须是操作者可见的词书（旧 assertSource）。
func assertSheetSource(ctx context.Context, d *Deps, actor *service.Actor, target string, source *service.SheetSource) error {
	if source == nil {
		return nil
	}
	switch source.Kind {
	case "session":
		owner, err := service.SheetSourceSessionOwner(ctx, d.DB, source.SessionID)
		if err != nil {
			return err
		}
		if owner != target {
			return httpx.Forbidden("只能用该学生自己的测试错词")
		}
		return service.AssertCanViewUser(ctx, d.DB, actor, owner)
	case "unit":
		filter, args, err := service.VisibleBookFilter(ctx, d.DB, actor, "b")
		if err != nil {
			return err
		}
		var id string
		err = d.DB.QueryRowContext(ctx, `SELECT u."id" FROM "Unit" u JOIN "Book" b ON b."id" = u."bookId" WHERE u."id" = ? AND `+filter,
			append([]any{source.UnitID}, args...)...).Scan(&id)
		if store.IsNoRows(err) {
			return httpx.NotFound("单元不存在")
		}
		return err
	case "book":
		filter, args, err := service.VisibleBookFilter(ctx, d.DB, actor, "b")
		if err != nil {
			return err
		}
		var id string
		err = d.DB.QueryRowContext(ctx, `SELECT b."id" FROM "Book" b WHERE b."id" = ? AND `+filter,
			append([]any{source.BookID}, args...)...).Scan(&id)
		if store.IsNoRows(err) {
			return httpx.NotFound("词书不存在")
		}
		return err
	}
	return nil
}

func registerSheets(r *Router, d *Deps) {
	study := auth.RequireCap(core.CapStudy)

	r.Post("/sheets/preview", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[sheetPreviewBody](req)
		if err != nil {
			return err
		}
		userID, err := targetBodyUser(ctx, d, actor, body.userID)
		if err != nil {
			return err
		}
		if err := assertSheetSource(ctx, d, actor, userID, body.source); err != nil {
			return err
		}
		if body.source != nil && body.source.Kind == sheetSourceTarget {
			// 目标词书按目标学生确定（范围已由 targetBodyUser 判定），版本按本次请求
			body.source.UseClasses = service.TargetUsesClasses(actor)
		}
		if body.format == core.SheetFormatDictation {
			for _, src := range body.dict.SentenceSources {
				if err := service.AssertSentenceSource(ctx, d.DB, actor, userID, src); err != nil {
					return err
				}
			}
			body.dict.Include, body.dict.Source = body.include, body.source
			out, err := service.PreviewDictation(ctx, d.DB, d.Cfg.Location, d.Now(), userID, body.dict)
			if err != nil {
				return err
			}
			httpx.OK(w, out)
			return nil
		}
		out, err := service.PreviewSheet(ctx, d.DB, d.Cfg.Location, d.Now(), userID, body.count, body.include, body.source)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, study)

	r.Get("/sheets/sources", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		userID, err := targetUser(req, d)
		if err != nil {
			return err
		}
		q, err := httpx.DecodeQuery[sheetSourcesQuery](req)
		if err != nil {
			return err
		}
		out, err := service.SheetSources(ctx, d.DB, d.Now(), userID)
		if err != nil {
			return err
		}
		// 默写单的句子来源（spec 0006）：要学的句子数量；带 unitId 时列出该单元的篇
		learning, err := service.LearningSentenceIDs(ctx, d.DB, userID)
		if err != nil {
			return err
		}
		out.LearningSentences = len(learning)
		if q.unitID != "" {
			if err := assertSheetSource(ctx, d, auth.ActorFrom(ctx), userID, &service.SheetSource{Kind: "unit", UnitID: q.unitID}); err != nil {
				return err
			}
			texts, err := service.UnitSourceTexts(ctx, d.DB, q.unitID)
			if err != nil {
				return err
			}
			out.Texts = &texts
		}
		httpx.OK(w, out)
		return nil
	}, study)

	r.Post("/sheets", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[sheetCreateBody](req)
		if err != nil {
			return err
		}
		userID, err := targetBodyUser(ctx, d, actor, body.userID)
		if err != nil {
			return err
		}
		if body.format == core.SheetFormatDictation {
			sentIDs := []string{}
			for _, it := range body.items {
				if core.IsSentenceItem(it.Type) {
					sentIDs = append(sentIDs, it.SentenceID)
				}
			}
			if err := service.AssertSentencesUsable(ctx, d.DB, actor, userID, sentIDs); err != nil {
				return err
			}
			out, err := service.CreateDictationSheet(ctx, d.DB, d.Now(), userID, actor.ID, body.items, body.copies, body.limits)
			if err != nil {
				return err
			}
			httpx.OK(w, out)
			return nil
		}
		out, err := service.CreateSheet(ctx, d.DB, d.Now(), userID, actor.ID, body.wordIDs, body.modes, body.copies, body.perSheet)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, study)

	r.Get("/sheets", func(w http.ResponseWriter, req *http.Request) error {
		userID, err := targetUser(req, d)
		if err != nil {
			return err
		}
		q, err := httpx.DecodeQuery[sheetsListQuery](req)
		if err != nil {
			return err
		}
		out, err := service.ListSheets(req.Context(), d.DB, userID, q.page, q.limit)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, study)

	r.Get("/sheets/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		detail, err := service.SheetDetail(ctx, d.DB, req.PathValue("id"))
		if err != nil {
			return err
		}
		if err := service.AssertCanViewUser(ctx, d.DB, actor, detail.UserID); err != nil {
			return err
		}
		httpx.OK(w, detail)
		return nil
	}, study)

	// 默写单批改（spec 0006）：能查看该学生的人都可以提交；学生本人的账号提交标记为自批；只能提交一次。
	r.Post("/sheets/{id}/grade", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[sheetGradeBody](req)
		if err != nil {
			return err
		}
		id := req.PathValue("id")
		owner, err := service.SheetOwner(ctx, d.DB, id)
		if err != nil {
			return err
		}
		if err := service.AssertCanViewUser(ctx, d.DB, actor, owner.UserID); err != nil {
			return err
		}
		out, err := service.GradeSheet(ctx, d.DB, d.Cfg.Location, d.Now(), actor, id, body.results)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, study)

	r.Delete("/sheets/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		owner, err := service.SheetOwner(ctx, d.DB, id)
		if err != nil {
			return err
		}
		if owner.UserID != actor.ID && owner.CreatorID != actor.ID && !service.SeesAll(actor) {
			if err := service.AssertCanViewUser(ctx, d.DB, actor, owner.UserID); err != nil {
				return httpx.Forbidden("无权删除这张单词单")
			}
		}
		if err := service.DeleteSheet(ctx, d.DB, id); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, study)
}

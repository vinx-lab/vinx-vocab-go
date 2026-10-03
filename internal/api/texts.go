package api

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/vocabparser"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
)

// 单元的篇（课文、句型清单）与句子（spec 0004）：
//
//	GET    /units/:id/texts                  单元内的篇（带句子）
//	POST   /units/:id/texts                  新建篇
//	PATCH  /units/:id/texts/order            单元内篇的排序
//	POST   /units/:id/texts/import/preview   单元内粘贴导入：预览
//	POST   /units/:id/texts/import           单元内粘贴导入：确认
//	PATCH  /texts/:id                        改标题、类型、单元内顺序
//	PUT    /texts/:id/sentences              整体替换句子列表
//	DELETE /texts/:id                        删除篇
//	GET    /words/:id/sentences              出现过这个词的句子（按来源分组）
//	POST   /sentences/analyze                编辑时实时检查：分词、关联、超纲、词库外
//	POST   /sentences/relink                 管理员：重新关联全部句子（spec §5）
//
// 查看跟随词书可见性（不可见 → 404「单元不存在」），编辑跟随词书编辑权限（AssertCanEditBook）。

var textKinds = []string{vocabparser.TextKindText, vocabparser.TextKindList}

type sentenceBody struct {
	ID        httpx.Opt[string]  `json:"id"`
	En        httpx.Opt[string]  `json:"en"`
	Cn        httpx.Opt[string]  `json:"cn"`
	Frame     httpx.Opt[string]  `json:"frame"`
	Paragraph httpx.Opt[float64] `json:"paragraph"`
}

// validateSentences 校验句子数组（path 为数组字段的路径）；required 为假时缺省视为空数组。
func validateSentences(v *httpx.V, path string, o httpx.Opt[[]sentenceBody], required, allowID bool) []service.SentenceInput {
	if !o.Set {
		if required {
			v.Add(path, "Required")
		}
		return nil
	}
	if o.Null {
		v.Add(path, "Expected array, received null")
		return nil
	}
	v.ArrayLen(path, len(o.Val), -1, 500, "", "")
	zero := 0
	out := make([]service.SentenceInput, 0, len(o.Val))
	for i, s := range o.Val {
		p := fmt.Sprintf("%s.%d", path, i)
		in := service.SentenceInput{
			En:        v.Str(p+".en", s.En, httpx.Trim(), httpx.Min(1, "英文不能为空"), httpx.Max(1000, "英文过长")),
			Cn:        v.Str(p+".cn", s.Cn, httpx.Trim(), httpx.Min(1, "中文不能为空"), httpx.Max(1000, "中文过长")),
			Paragraph: v.Int(p+".paragraph", s.Paragraph, &zero, httpx.Between(0, 10000)),
		}
		if f := v.NullStr(p+".frame", s.Frame, httpx.Trim(), httpx.Max(1000, "句型骨架过长")); f.Set && !f.Null && f.Val != "" {
			frame := f.Val
			in.Frame = &frame
		}
		if allowID {
			if id := v.OptStr(p+".id", s.ID); id != nil {
				in.ID = *id
			}
		}
		out = append(out, in)
	}
	return out
}

type textBody struct {
	Title     httpx.Opt[string]         `json:"title"`
	TitleCn   httpx.Opt[string]         `json:"titleCn"`
	Kind      httpx.Opt[string]         `json:"kind"`
	Sentences httpx.Opt[[]sentenceBody] `json:"sentences"`
}

func validateText(v *httpx.V, prefix string, b textBody) service.NewUnitText {
	t := service.NewUnitText{
		Title: v.Str(prefix+"title", b.Title, httpx.Trim(), httpx.Min(1, "标题不能为空"), httpx.Max(100, "标题过长")),
		Kind:  v.OptEnum(prefix+"kind", b.Kind, textKinds, "", vocabparser.TextKindText),
	}
	if c := v.NullStr(prefix+"titleCn", b.TitleCn, httpx.Trim(), httpx.Max(100, "中文标题过长")); c.Set && !c.Null && c.Val != "" {
		s := c.Val
		t.TitleCn = &s
	}
	t.Sentences = validateSentences(v, prefix+"sentences", b.Sentences, false, false)
	return t
}

// unitTextCreateBody POST /units/:id/texts（字段与 textBody 相同；httpx.Decode 按顶层键名逐字段解码，不用匿名嵌入）。
type unitTextCreateBody struct {
	Title     httpx.Opt[string]         `json:"title"`
	TitleCn   httpx.Opt[string]         `json:"titleCn"`
	Kind      httpx.Opt[string]         `json:"kind"`
	Sentences httpx.Opt[[]sentenceBody] `json:"sentences"`
	text      service.NewUnitText
}

func (b *unitTextCreateBody) Validate(v *httpx.V) {
	b.text = validateText(v, "", textBody{Title: b.Title, TitleCn: b.TitleCn, Kind: b.Kind, Sentences: b.Sentences})
}

type unitTextPatchBody struct {
	Title     httpx.Opt[string]  `json:"title"`
	TitleCn   httpx.Opt[string]  `json:"titleCn"`
	Kind      httpx.Opt[string]  `json:"kind"`
	SortOrder httpx.Opt[float64] `json:"sortOrder"`

	patch service.UnitTextPatch
}

func (b *unitTextPatchBody) Validate(v *httpx.V) {
	b.patch.Title = v.OptStr("title", b.Title, httpx.Trim(), httpx.Min(1, "标题不能为空"), httpx.Max(100, "标题过长"))
	if c := v.NullStr("titleCn", b.TitleCn, httpx.Trim(), httpx.Max(100, "中文标题过长")); c.Set {
		if c.Null || c.Val == "" {
			b.patch.TitleCn = &sql.NullString{}
		} else {
			b.patch.TitleCn = &sql.NullString{String: c.Val, Valid: true}
		}
	}
	if b.Kind.Set {
		k := v.OptEnum("kind", b.Kind, textKinds, "", "")
		if k != "" {
			b.patch.Kind = &k
		}
	}
	if b.SortOrder.Set {
		n := v.Int("sortOrder", b.SortOrder, nil, httpx.IntRange{})
		b.patch.SortOrder = &n
	}
}

type textSentencesBody struct {
	Sentences httpx.Opt[[]sentenceBody] `json:"sentences"`
	sentences []service.SentenceInput
}

func (b *textSentencesBody) Validate(v *httpx.V) {
	b.sentences = validateSentences(v, "sentences", b.Sentences, true, true)
}

type textOrderBody struct {
	TextIDs httpx.Opt[[]string] `json:"textIds"`
	textIDs []string
}

func (b *textOrderBody) Validate(v *httpx.V) {
	if !b.TextIDs.Set {
		v.Add("textIds", "Required")
		return
	}
	if b.TextIDs.Null {
		v.Add("textIds", "Expected array, received null")
		return
	}
	b.textIDs = b.TextIDs.Val
	v.ArrayLen("textIds", len(b.textIDs), 1, 500, "请提供顺序", "")
}

type textsPreviewBody struct {
	Text httpx.Opt[string] `json:"text"`
	Kind httpx.Opt[string] `json:"kind"`
	text string
	kind string
}

func (b *textsPreviewBody) Validate(v *httpx.V) {
	b.text = v.Str("text", b.Text, httpx.Min(1, "请粘贴句型或课文"), httpx.Max(200_000))
	b.kind = v.OptEnum("kind", b.Kind, textKinds, "", vocabparser.TextKindList)
}

type textsImportBody struct {
	Texts httpx.Opt[[]textBody] `json:"texts"`
	texts []service.NewUnitText
}

func (b *textsImportBody) Validate(v *httpx.V) {
	b.texts = validateTextList(v, "texts", b.Texts, true)
	if b.Texts.Set && !b.Texts.Null {
		v.ArrayLen("texts", len(b.Texts.Val), 1, 100, "没有可导入的内容", "")
	}
}

// validateTextList 校验篇数组（/units/:id/texts/import 与 /books/import 的 units[].texts 共用）。
func validateTextList(v *httpx.V, path string, o httpx.Opt[[]textBody], required bool) []service.NewUnitText {
	if !o.Set {
		if required {
			v.Add(path, "Required")
		}
		return nil
	}
	if o.Null {
		v.Add(path, "Expected array, received null")
		return nil
	}
	v.ArrayLen(path, len(o.Val), -1, 100, "", "")
	out := make([]service.NewUnitText, 0, len(o.Val))
	for i, t := range o.Val {
		out = append(out, validateText(v, fmt.Sprintf("%s.%d.", path, i), t))
	}
	return out
}

type analyzeBody struct {
	En     httpx.Opt[string] `json:"en"`
	UnitID httpx.Opt[string] `json:"unitId"`
	en     string
	unitID *string
}

func (b *analyzeBody) Validate(v *httpx.V) {
	b.en = v.Str("en", b.En, httpx.Trim(), httpx.Min(1, "请输入句子"), httpx.Max(2000))
	if u := v.NullStr("unitId", b.UnitID); u.Set && !u.Null && u.Val != "" {
		id := u.Val
		b.unitID = &id
	}
}

// editableTextUnit 篇所在单元 + 编辑权限。
func editableTextUnit(req *http.Request, d *Deps, textID string) (*service.UnitRow, error) {
	ctx := req.Context()
	unit, err := service.UnitTextUnit(ctx, d.DB, textID)
	if err != nil {
		return nil, err
	}
	if _, err := service.AssertCanEditBook(ctx, d.DB, auth.ActorFrom(ctx), unit.BookID); err != nil {
		return nil, err
	}
	return unit, nil
}

// editableUnit 单元 + 编辑权限（单元不存在 → 404「单元不存在」）。
func editableUnit(req *http.Request, d *Deps, unitID string) (*service.UnitRow, error) {
	ctx := req.Context()
	unit, err := service.UnitBook(ctx, d.DB, unitID)
	if err != nil {
		return nil, err
	}
	if _, err := service.AssertCanEditBook(ctx, d.DB, auth.ActorFrom(ctx), unit.BookID); err != nil {
		return nil, err
	}
	return unit, nil
}

func registerTexts(r *Router, d *Deps) {
	r.Get("/units/{id}/texts", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		unit, err := service.VisibleUnit(ctx, d.DB, auth.ActorFrom(ctx), req.PathValue("id"))
		if err != nil {
			return err
		}
		if unit == nil {
			return httpx.NotFound("单元不存在")
		}
		items, err := service.ListUnitTexts(ctx, d.DB, unit.ID)
		if err != nil {
			return err
		}
		httpx.OK(w, httpx.List(items))
		return nil
	}, auth.RequireCap(core.CapBooksRead))

	r.Post("/units/{id}/texts", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		unit, err := editableUnit(req, d, req.PathValue("id"))
		if err != nil {
			return err
		}
		body, err := httpx.Decode[unitTextCreateBody](req)
		if err != nil {
			return err
		}
		in := body.text
		in.Source, in.CreatedByID = service.SentenceManual, actor.ID
		var id string
		err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
			var err error
			id, err = service.CreateUnitText(ctx, tx, nil, d.Now(), unit.ID, in)
			return err
		})
		if err != nil {
			return err
		}
		view, err := service.GetUnitText(ctx, d.DB, id)
		if err != nil {
			return err
		}
		httpx.OK(w, view)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Patch("/units/{id}/texts/order", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		unit, err := editableUnit(req, d, req.PathValue("id"))
		if err != nil {
			return err
		}
		body, err := httpx.Decode[textOrderBody](req)
		if err != nil {
			return err
		}
		var n int
		err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
			var err error
			n, err = service.ReorderUnitTexts(ctx, tx, unit.ID, body.textIDs)
			return err
		})
		if err != nil {
			return err
		}
		httpx.OK(w, struct {
			Ordered int `json:"ordered"`
		}{n})
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Post("/units/{id}/texts/import/preview", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		unit, err := editableUnit(req, d, req.PathValue("id"))
		if err != nil {
			return err
		}
		body, err := httpx.Decode[textsPreviewBody](req)
		if err != nil {
			return err
		}
		res, err := service.PreviewUnitTexts(ctx, d.DB, unit.ID, body.text, body.kind)
		if err != nil {
			return err
		}
		httpx.OK(w, res)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Post("/units/{id}/texts/import", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		unit, err := editableUnit(req, d, req.PathValue("id"))
		if err != nil {
			return err
		}
		body, err := httpx.Decode[textsImportBody](req)
		if err != nil {
			return err
		}
		for i := range body.texts {
			body.texts[i].Source, body.texts[i].CreatedByID = service.SentenceImport, actor.ID
		}
		var res service.ImportTextsResult
		err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
			var err error
			res, err = service.ImportUnitTexts(ctx, tx, d.Now(), unit.ID, body.texts)
			return err
		})
		if err != nil {
			return err
		}
		httpx.OK(w, res)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Patch("/texts/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		id := req.PathValue("id")
		if _, err := editableTextUnit(req, d, id); err != nil {
			return err
		}
		body, err := httpx.Decode[unitTextPatchBody](req)
		if err != nil {
			return err
		}
		if err := service.UpdateUnitText(ctx, d.DB, d.Now(), id, body.patch); err != nil {
			return err
		}
		view, err := service.GetUnitText(ctx, d.DB, id)
		if err != nil {
			return err
		}
		httpx.OK(w, view)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Put("/texts/{id}/sentences", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		if _, err := editableTextUnit(req, d, id); err != nil {
			return err
		}
		body, err := httpx.Decode[textSentencesBody](req)
		if err != nil {
			return err
		}
		err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
			return service.ReplaceTextSentences(ctx, tx, d.Now(), id, body.sentences, actor.ID)
		})
		if err != nil {
			return err
		}
		view, err := service.GetUnitText(ctx, d.DB, id)
		if err != nil {
			return err
		}
		httpx.OK(w, view)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Delete("/texts/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		id := req.PathValue("id")
		if _, err := editableTextUnit(req, d, id); err != nil {
			return err
		}
		err := d.DB.Tx(ctx, func(tx *sql.Tx) error { return service.DeleteUnitText(ctx, tx, id) })
		if err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Get("/words/{id}/sentences", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		res, err := service.GetWordSentences(ctx, d.DB, auth.ActorFrom(ctx), req.PathValue("id"))
		if err != nil {
			return err
		}
		if res == nil {
			return httpx.NotFound("单词不存在")
		}
		httpx.OK(w, res)
		return nil
	}, auth.RequireCap(core.CapBooksRead))

	r.Post("/sentences/analyze", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		body, err := httpx.Decode[analyzeBody](req)
		if err != nil {
			return err
		}
		res, err := service.AnalyzeSentence(ctx, d.DB, auth.ActorFrom(ctx), body.en, body.unitID)
		if err != nil {
			return err
		}
		httpx.OK(w, res)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Post("/sentences/relink", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		var n int
		err := d.DB.Tx(ctx, func(tx *sql.Tx) error {
			var err error
			n, err = service.RelinkAllSentences(ctx, tx, d.Now())
			return err
		})
		if err != nil {
			return err
		}
		httpx.OK(w, struct {
			Sentences int `json:"sentences"`
		}{n})
		return nil
	}, auth.RequireCap(core.CapSystem))
}

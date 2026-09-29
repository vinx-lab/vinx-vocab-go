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

// 词书 / 单元 / 单词 / 导入（对应旧 routes/books.ts）。

// ===== 入参 =====

type bookCreateBody struct {
	Name        httpx.Opt[string] `json:"name"`
	Description httpx.Opt[string] `json:"description"`
	IsSystem    httpx.Opt[bool]   `json:"isSystem"`

	name        string
	description *string
	isSystem    bool
}

func (b *bookCreateBody) Validate(v *httpx.V) {
	b.name = v.Str("name", b.Name, httpx.Trim(), httpx.Min(1, "名称不能为空"), httpx.Max(60, "名称过长"))
	d := v.NullStr("description", b.Description, httpx.Max(300, "描述过长"))
	if d.Set && !d.Null {
		s := d.Val
		b.description = &s
	}
	b.isSystem = v.OptBool("isSystem", b.IsSystem, false)
}

type bookPatchBody struct {
	Name        httpx.Opt[string] `json:"name"`
	Description httpx.Opt[string] `json:"description"`
	IsSystem    httpx.Opt[bool]   `json:"isSystem"`

	name        *string
	description httpx.Opt[string]
	isSystem    *bool
}

func (b *bookPatchBody) Validate(v *httpx.V) {
	b.name = v.OptStr("name", b.Name, httpx.Trim(), httpx.Min(1, "名称不能为空"), httpx.Max(60, "名称过长"))
	b.description = v.NullStr("description", b.Description, httpx.Max(300, "描述过长"))
	if b.IsSystem.Set {
		val := v.OptBool("isSystem", b.IsSystem, false)
		b.isSystem = &val
	}
}

type unitCreateBody struct {
	Name      httpx.Opt[string]  `json:"name"`
	SortOrder httpx.Opt[float64] `json:"sortOrder"`

	name      string
	sortOrder *int
}

func (b *unitCreateBody) Validate(v *httpx.V) {
	b.name = v.Str("name", b.Name, httpx.Trim(), httpx.Min(1, "名称不能为空"), httpx.Max(60, "名称过长"))
	if b.SortOrder.Set {
		n := v.Int("sortOrder", b.SortOrder, nil, httpx.IntRange{})
		b.sortOrder = &n
	}
}

type unitPatchBody struct {
	Name      httpx.Opt[string]  `json:"name"`
	SortOrder httpx.Opt[float64] `json:"sortOrder"`

	name      *string
	sortOrder *int
}

func (b *unitPatchBody) Validate(v *httpx.V) {
	b.name = v.OptStr("name", b.Name, httpx.Trim(), httpx.Min(1, "名称不能为空"), httpx.Max(60, "名称过长"))
	if b.SortOrder.Set {
		n := v.Int("sortOrder", b.SortOrder, nil, httpx.IntRange{})
		b.sortOrder = &n
	}
}

type unitOrderBody struct {
	UnitIDs httpx.Opt[[]string] `json:"unitIds"`

	unitIDs []string
}

func (b *unitOrderBody) Validate(v *httpx.V) {
	if !b.UnitIDs.Set {
		v.Add("unitIds", "Required")
		return
	}
	if b.UnitIDs.Null {
		v.Add("unitIds", "Expected array, received null")
		return
	}
	b.unitIDs = b.UnitIDs.Val
	v.ArrayLen("unitIds", len(b.unitIDs), 1, 500, "请提供单元顺序", "")
}

// wordCreateBody 加入单元用的新词（旧 wordSchema：无 type 字段）。
type wordCreateBody struct {
	Spelling     httpx.Opt[string] `json:"spelling"`
	Phonetic     httpx.Opt[string] `json:"phonetic"`
	PartOfSpeech httpx.Opt[string] `json:"partOfSpeech"`
	Definition   httpx.Opt[string] `json:"definition"`
	Example      httpx.Opt[string] `json:"example"`
	ExampleCn    httpx.Opt[string] `json:"exampleCn"`

	entry service.ImportEntry
}

func (b *wordCreateBody) Validate(v *httpx.V) {
	b.entry.Spelling = v.Str("spelling", b.Spelling, httpx.Trim(), httpx.Min(1, "拼写不能为空"), httpx.Max(100))
	b.entry.Definition = v.Str("definition", b.Definition, httpx.Trim(), httpx.Min(1, "释义不能为空"), httpx.Max(300))
	b.entry.Phonetic = optStrVal(v.NullStr("phonetic", b.Phonetic, httpx.Max(100)))
	b.entry.PartOfSpeech = optStrVal(v.NullStr("partOfSpeech", b.PartOfSpeech, httpx.Max(30)))
	b.entry.Example = optStrVal(v.NullStr("example", b.Example, httpx.Max(500)))
	b.entry.ExampleCn = optStrVal(v.NullStr("exampleCn", b.ExampleCn, httpx.Max(500)))
}

// wordPatchBody 编辑词条（旧 wordSchema.partial()）。
type wordPatchBody struct {
	Spelling     httpx.Opt[string] `json:"spelling"`
	Phonetic     httpx.Opt[string] `json:"phonetic"`
	PartOfSpeech httpx.Opt[string] `json:"partOfSpeech"`
	Definition   httpx.Opt[string] `json:"definition"`
	Example      httpx.Opt[string] `json:"example"`
	ExampleCn    httpx.Opt[string] `json:"exampleCn"`

	patch service.WordPatch
}

func nullStrPatch(o httpx.Opt[string]) *sql.NullString {
	if !o.Set {
		return nil
	}
	if o.Null {
		return &sql.NullString{Valid: false}
	}
	return &sql.NullString{String: o.Val, Valid: true}
}

func (b *wordPatchBody) Validate(v *httpx.V) {
	if s := v.OptStr("spelling", b.Spelling, httpx.Trim(), httpx.Min(1, "拼写不能为空"), httpx.Max(100)); s != nil {
		b.patch.Spelling = s
	}
	if d := v.OptStr("definition", b.Definition, httpx.Trim(), httpx.Min(1, "释义不能为空"), httpx.Max(300)); d != nil {
		b.patch.Definition = d
	}
	b.patch.Phonetic = nullStrPatch(v.NullStr("phonetic", b.Phonetic, httpx.Max(100)))
	b.patch.PartOfSpeech = nullStrPatch(v.NullStr("partOfSpeech", b.PartOfSpeech, httpx.Max(30)))
	b.patch.Example = nullStrPatch(v.NullStr("example", b.Example, httpx.Max(500)))
	b.patch.ExampleCn = nullStrPatch(v.NullStr("exampleCn", b.ExampleCn, httpx.Max(500)))
}

func optStrVal(o httpx.Opt[string]) string {
	if o.Set && !o.Null {
		return o.Val
	}
	return ""
}

// importPreviewBody POST /books/import/preview。
type importPreviewBody struct {
	Text           httpx.Opt[string] `json:"text"`
	SplitByInitial httpx.Opt[bool]   `json:"splitByInitial"`
	DefaultUnit    httpx.Opt[string] `json:"defaultUnit"`

	text           string
	splitByInitial bool
	defaultUnit    string
}

func (b *importPreviewBody) Validate(v *httpx.V) {
	b.text = v.Str("text", b.Text, httpx.Min(1, "请粘贴词表内容"), httpx.Max(2_000_000))
	b.splitByInitial = v.OptBool("splitByInitial", b.SplitByInitial, false)
	if d := v.OptStr("defaultUnit", b.DefaultUnit, httpx.Max(60)); d != nil {
		b.defaultUnit = *d
	}
}

type importEntryBody struct {
	Spelling     httpx.Opt[string] `json:"spelling"`
	Phonetic     httpx.Opt[string] `json:"phonetic"`
	PartOfSpeech httpx.Opt[string] `json:"partOfSpeech"`
	Definition   httpx.Opt[string] `json:"definition"`
	Type         httpx.Opt[string] `json:"type"`
	Example      httpx.Opt[string] `json:"example"`
	ExampleCn    httpx.Opt[string] `json:"exampleCn"`
}

type importUnitBody struct {
	Name    httpx.Opt[string]            `json:"name"`
	Entries httpx.Opt[[]importEntryBody] `json:"entries"`
}

type newBookImportBody struct {
	Name        httpx.Opt[string] `json:"name"`
	Description httpx.Opt[string] `json:"description"`
	IsSystem    httpx.Opt[bool]   `json:"isSystem"`
}

// importBody POST /books/import（旧 importSchema）。
type importBody struct {
	BookID  httpx.Opt[string]            `json:"bookId"`
	NewBook httpx.Opt[newBookImportBody] `json:"newBook"`
	Units   httpx.Opt[[]importUnitBody]  `json:"units"`

	bookID  *string
	newBook *service.NewBookInput
	units   []service.ImportUnit
}

func (b *importBody) Validate(v *httpx.V) {
	b.bookID = v.OptStr("bookId", b.BookID)
	if b.bookID != nil && *b.bookID == "" {
		// 旧版用 `!body.bookId` 判断，空串按未提供处理（M1）。
		b.bookID = nil
	}

	if b.NewBook.Set {
		if b.NewBook.Null {
			v.Add("newBook", "Expected object, received null")
		} else {
			nb := b.NewBook.Val
			name := v.Str("newBook.name", nb.Name, httpx.Trim(), httpx.Min(1, "名称不能为空"), httpx.Max(60, "名称过长"))
			var desc *string
			d := v.NullStr("newBook.description", nb.Description, httpx.Max(300, "描述过长"))
			if d.Set && !d.Null {
				s := d.Val
				desc = &s
			}
			isSystem := v.OptBool("newBook.isSystem", nb.IsSystem, false)
			b.newBook = &service.NewBookInput{Name: name, Description: desc, IsSystem: isSystem}
		}
	}

	if !b.Units.Set {
		v.Add("units", "Required")
		return
	}
	if b.Units.Null {
		v.Add("units", "Expected array, received null")
		return
	}
	units := b.Units.Val
	v.ArrayLen("units", len(units), 1, 200, "没有可导入的单元", "")
	out := make([]service.ImportUnit, 0, len(units))
	for i, u := range units {
		prefix := fmt.Sprintf("units.%d", i)
		// 旧 importSchema 的单元名没有自定义消息，用 zod 默认英文文案（I1）。
		name := v.Str(prefix+".name", u.Name, httpx.Trim(), httpx.Min(1), httpx.Max(60))
		var entries []service.ImportEntry
		switch {
		case !u.Entries.Set:
			v.Add(prefix+".entries", "Required")
		case u.Entries.Null:
			v.Add(prefix+".entries", "Expected array, received null")
		default:
			es := u.Entries.Val
			v.ArrayLen(prefix+".entries", len(es), -1, 5000, "", "")
			entries = make([]service.ImportEntry, 0, len(es))
			for j, e := range es {
				ep := fmt.Sprintf("%s.entries.%d", prefix, j)
				spelling := v.Str(ep+".spelling", e.Spelling, httpx.Trim(), httpx.Min(1, "拼写不能为空"), httpx.Max(100))
				definition := v.Str(ep+".definition", e.Definition, httpx.Trim(), httpx.Min(1, "释义不能为空"), httpx.Max(300))
				phonetic := optStrVal(v.NullStr(ep+".phonetic", e.Phonetic, httpx.Max(100)))
				pos := optStrVal(v.NullStr(ep+".partOfSpeech", e.PartOfSpeech, httpx.Max(30)))
				example := optStrVal(v.NullStr(ep+".example", e.Example, httpx.Max(500)))
				exampleCn := optStrVal(v.NullStr(ep+".exampleCn", e.ExampleCn, httpx.Max(500)))
				typ := v.OptEnum(ep+".type", e.Type, []string{"word", "phrase"}, "", "")
				entries = append(entries, service.ImportEntry{
					Spelling: spelling, Definition: definition, Phonetic: phonetic, PartOfSpeech: pos,
					Example: example, ExampleCn: exampleCn, Type: typ,
				})
			}
		}
		out = append(out, service.ImportUnit{Name: name, Entries: entries})
	}
	b.units = out
}

// pageWordsQuery /units/:id/words 与 /words 的分页 + 搜索参数（旧 pageQuery.extend({...})）。
type unitWordsQuery struct {
	Page  httpx.Opt[string] `json:"page"`
	Limit httpx.Opt[string] `json:"limit"`
	Q     httpx.Opt[string] `json:"q"`

	page, limit int
	q           string
}

func (q *unitWordsQuery) Validate(v *httpx.V) {
	one := 1
	hundred := 100
	q.page = v.CoerceInt("page", q.Page, &one, httpx.IntRange{Min: &one})
	q.limit = v.CoerceInt("limit", q.Limit, &hundred, httpx.Between(1, 500))
	if s := v.OptStr("q", q.Q); s != nil {
		q.q = *s
	}
}

type wordsSearchQuery struct {
	Page  httpx.Opt[string] `json:"page"`
	Limit httpx.Opt[string] `json:"limit"`
	Q     httpx.Opt[string] `json:"q"`

	page, limit int
	q           string
}

func (q *wordsSearchQuery) Validate(v *httpx.V) {
	one := 1
	twenty := 20
	q.page = v.CoerceInt("page", q.Page, &one, httpx.IntRange{Min: &one})
	q.limit = v.CoerceInt("limit", q.Limit, &twenty, httpx.Between(1, 100))
	q.q = v.Str("q", q.Q, httpx.Trim(), httpx.Min(1, "请输入关键词"))
}

func registerBooks(r *Router, d *Deps) {
	// ===== 词书 =====

	r.Get("/books", func(w http.ResponseWriter, req *http.Request) error {
		actor := auth.ActorFrom(req.Context())
		items, err := service.ListBooks(req.Context(), d.DB, actor)
		if err != nil {
			return err
		}
		httpx.OK(w, httpx.List(items))
		return nil
	}, auth.RequireCap(core.CapBooksRead))

	r.Post("/books", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[bookCreateBody](req)
		if err != nil {
			return err
		}
		book, err := service.CreateBook(ctx, d.DB, d.Now(), actor.ID, service.NewBookInput{
			Name: body.name, Description: body.description, IsSystem: service.IsAdmin(actor) && body.isSystem,
		})
		if err != nil {
			return err
		}
		httpx.OK(w, book)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Get("/books/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		detail, err := service.GetBookDetail(ctx, d.DB, actor, id)
		if err != nil {
			return err
		}
		if detail == nil {
			return httpx.NotFound("词书不存在")
		}
		httpx.OK(w, detail)
		return nil
	}, auth.RequireCap(core.CapBooksRead))

	r.Patch("/books/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		if _, err := service.AssertCanEditBook(ctx, d.DB, actor, id); err != nil {
			return err
		}
		body, err := httpx.Decode[bookPatchBody](req)
		if err != nil {
			return err
		}
		patch := service.BookPatch{Name: body.name}
		if body.description.Set {
			if body.description.Null {
				patch.Description = &sql.NullString{Valid: false}
			} else {
				patch.Description = &sql.NullString{String: body.description.Val, Valid: true}
			}
		}
		if service.IsAdmin(actor) && body.isSystem != nil {
			patch.IsSystem = body.isSystem
		}
		if err := service.UpdateBook(ctx, d.DB, d.Now(), id, patch); err != nil {
			return err
		}
		book, err := service.GetBookRow(ctx, d.DB, id)
		if err != nil {
			return err
		}
		httpx.OK(w, book)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Delete("/books/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		if _, err := service.AssertCanEditBook(ctx, d.DB, actor, id); err != nil {
			return err
		}
		if err := service.AssertBookNotUsedByPlans(ctx, d.DB, id); err != nil {
			return err
		}
		if err := service.DeleteBook(ctx, d.DB, id); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	// ===== 单元 =====

	r.Post("/books/{id}/units", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		if _, err := service.AssertCanEditBook(ctx, d.DB, actor, id); err != nil {
			return err
		}
		body, err := httpx.Decode[unitCreateBody](req)
		if err != nil {
			return err
		}
		unit, err := service.CreateUnit(ctx, d.DB, d.Now(), id, body.name, body.sortOrder)
		if err != nil {
			return err
		}
		httpx.OK(w, unit)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Patch("/units/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		unit, err := service.UnitBook(ctx, d.DB, id)
		if err != nil {
			return err
		}
		if _, err := service.AssertCanEditBook(ctx, d.DB, actor, unit.BookID); err != nil {
			return err
		}
		body, err := httpx.Decode[unitPatchBody](req)
		if err != nil {
			return err
		}
		updated, err := service.UpdateUnit(ctx, d.DB, id, body.name, body.sortOrder)
		if err != nil {
			return err
		}
		httpx.OK(w, updated)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Delete("/units/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		unit, err := service.UnitBook(ctx, d.DB, id)
		if err != nil {
			return err
		}
		if _, err := service.AssertCanEditBook(ctx, d.DB, actor, unit.BookID); err != nil {
			return err
		}
		if err := service.AssertUnitNotUsedByPlans(ctx, d.DB, id); err != nil {
			return err
		}
		if err := service.DeleteUnit(ctx, d.DB, id); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Patch("/books/{id}/units/order", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		if _, err := service.AssertCanEditBook(ctx, d.DB, actor, id); err != nil {
			return err
		}
		body, err := httpx.Decode[unitOrderBody](req)
		if err != nil {
			return err
		}
		// 整册一次提交顺序：包进事务，避免中途出错留下半套顺序（M5）。
		var n int
		err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
			var err error
			n, err = service.ReorderUnits(ctx, tx, id, body.unitIDs)
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

	// ===== 单元内词条 =====

	r.Get("/units/{id}/words", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		q, err := httpx.DecodeQuery[unitWordsQuery](req)
		if err != nil {
			return err
		}
		res, err := service.ListUnitWords(ctx, d.DB, actor, id, q.q, q.page, q.limit)
		if err != nil {
			return err
		}
		if res == nil {
			return httpx.NotFound("单元不存在")
		}
		httpx.OK(w, res)
		return nil
	}, auth.RequireCap(core.CapBooksRead))

	r.Post("/units/{id}/words", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		unit, err := service.UnitBook(ctx, d.DB, id)
		if err != nil {
			return err
		}
		if _, err := service.AssertCanEditBook(ctx, d.DB, actor, unit.BookID); err != nil {
			return err
		}
		body, err := httpx.Decode[wordCreateBody](req)
		if err != nil {
			return err
		}
		entry := body.entry
		entry.Spelling = vocabparser.NormalizeSpelling(entry.Spelling)
		var result service.ImportResult
		err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
			var err error
			result, err = service.ImportUnits(ctx, tx, d.Now(), unit.BookID, []service.ImportUnit{{Name: unit.Name, Entries: []service.ImportEntry{entry}}})
			return err
		})
		if err != nil {
			return err
		}
		httpx.OK(w, struct {
			Reused bool `json:"reused"`
			Linked bool `json:"linked"`
		}{result.WordsReused > 0, result.WordsLinked > 0})
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Delete("/units/{id}/words/{wordId}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id, wordID := req.PathValue("id"), req.PathValue("wordId")
		unit, err := service.UnitBook(ctx, d.DB, id)
		if err != nil {
			return err
		}
		if _, err := service.AssertCanEditBook(ctx, d.DB, actor, unit.BookID); err != nil {
			return err
		}
		if err := service.RemoveUnitWord(ctx, d.DB, id, wordID); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	// ===== 词条 =====

	r.Get("/words/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		detail, err := service.GetWordDetail(ctx, d.DB, actor, id)
		if err != nil {
			return err
		}
		if detail == nil {
			return httpx.NotFound("单词不存在")
		}
		httpx.OK(w, detail)
		return nil
	}, auth.RequireCap(core.CapBooksRead))

	r.Patch("/words/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		// 与旧版顺序一致：先 parseBody（校验失败先报 400），再查词、校验权限（M2）。
		body, err := httpx.Decode[wordPatchBody](req)
		if err != nil {
			return err
		}
		existing, err := service.GetWordRow(ctx, d.DB, id)
		if err != nil {
			return err
		}
		if existing == nil {
			return httpx.NotFound("单词不存在")
		}
		if !service.SeesAll(actor) {
			ok, err := service.WordEditableBy(ctx, d.DB, actor, id)
			if err != nil {
				return err
			}
			if !ok {
				return httpx.Forbidden("该词被系统词书或他人词书引用，只有管理员可以修改")
			}
		}
		patch := body.patch
		if patch.Spelling != nil {
			normalized := vocabparser.NormalizeSpelling(*patch.Spelling)
			patch.Spelling = &normalized
			if normalized != existing.Spelling {
				clash, err := service.FindWordBySpelling(ctx, d.DB, normalized)
				if err != nil {
					return err
				}
				if clash != nil {
					return httpx.NewError(httpx.CodeDuplicate, "已存在相同拼写的单词", nil)
				}
			}
		}
		updated, err := service.UpdateWord(ctx, d.DB, d.Now(), id, patch)
		if err != nil {
			return err
		}
		httpx.OK(w, updated)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Get("/words", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		q, err := httpx.DecodeQuery[wordsSearchQuery](req)
		if err != nil {
			return err
		}
		items, total, err := service.SearchWords(ctx, d.DB, q.q, q.page, q.limit)
		if err != nil {
			return err
		}
		httpx.OK(w, httpx.Page(items, total, q.page, q.limit))
		return nil
	}, auth.RequireCap(core.CapBooksRead))

	// ===== 导入 =====

	r.Post("/books/import/preview", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		body, err := httpx.Decode[importPreviewBody](req)
		if err != nil {
			return err
		}
		defaultUnit := body.defaultUnit
		if defaultUnit == "" {
			defaultUnit = "未分组"
		}
		res, err := service.PreviewImport(ctx, d.DB, body.text, body.splitByInitial, defaultUnit)
		if err != nil {
			return err
		}
		httpx.OK(w, res)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Post("/books/import", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[importBody](req)
		if err != nil {
			return err
		}
		if body.bookID == nil && body.newBook == nil {
			return httpx.Validation("请选择词书或新建词书")
		}
		if body.bookID != nil {
			if _, err := service.AssertCanEditBook(ctx, d.DB, actor, *body.bookID); err != nil {
				return err
			}
		} else {
			if err := service.AssertCan(actor, core.CapBooksEdit, ""); err != nil {
				return err
			}
			if !service.IsAdmin(actor) {
				body.newBook.IsSystem = false
			}
		}
		var result *service.ImportBooksResult
		err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
			var err error
			result, err = service.ImportIntoBook(ctx, tx, d.Now(), actor.ID, body.bookID, body.newBook, body.units)
			return err
		})
		if err != nil {
			return err
		}
		httpx.OK(w, result)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))
}

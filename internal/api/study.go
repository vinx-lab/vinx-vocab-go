package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 今日、学习组、记录（对应旧 routes/study.ts）。

var (
	sessionKindValues = []string{"learn", "review", "test", "drill", "sheet"}
	answerModeValues  = []string{"recognition", "spelling", "cloze"}
	answerPhaseValues = []string{"practice", "consolidate", "test"}
)

// ===== 请求体 =====

type startBody struct {
	Kind    httpx.Opt[string] `json:"kind"`
	PlanID  httpx.Opt[string] `json:"planId"`
	SheetID httpx.Opt[string] `json:"sheetId"`

	in service.StartInput
}

func (b *startBody) Validate(v *httpx.V) {
	b.in.Kind = v.Enum("kind", b.Kind, sessionKindValues, "")
	if o := v.NullStr("planId", b.PlanID); o.Present() {
		s := o.Val
		b.in.PlanID = &s
	}
	if o := v.NullStr("sheetId", b.SheetID); o.Present() {
		s := o.Val
		b.in.SheetID = &s
	}
}

type answerBody struct {
	WordID     httpx.Opt[string]  `json:"wordId"`
	Mode       httpx.Opt[string]  `json:"mode"`
	Phase      httpx.Opt[string]  `json:"phase"`
	Attempt    httpx.Opt[float64] `json:"attempt"`
	Answer     httpx.Opt[string]  `json:"answer"`
	HintUsed   httpx.Opt[bool]    `json:"hintUsed"`
	DontKnow   httpx.Opt[bool]    `json:"dontKnow"`
	DurationMs httpx.Opt[float64] `json:"durationMs"`

	in service.AnswerInput
}

func (b *answerBody) Validate(v *httpx.V) {
	zero := 0
	b.in.WordID = v.Str("wordId", b.WordID, httpx.Min(1))
	b.in.Mode = v.Enum("mode", b.Mode, answerModeValues, "")
	b.in.Phase = v.Enum("phase", b.Phase, answerPhaseValues, "")
	b.in.Attempt = v.Int("attempt", b.Attempt, nil, httpx.Between(1, 20))
	b.in.Answer = v.Str("answer", b.Answer, httpx.Max(300))
	b.in.HintUsed = v.OptBool("hintUsed", b.HintUsed, false)
	b.in.DontKnow = v.OptBool("dontKnow", b.DontKnow, false)
	b.in.DurationMs = v.Int("durationMs", b.DurationMs, &zero, httpx.IntRange{Min: &zero})
}

type progressBody struct {
	Progress httpx.Opt[json.RawMessage] `json:"progress"`

	progress json.RawMessage
}

func rawKind(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "undefined"
	}
	switch raw[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	}
	return "number"
}

func (b *progressBody) Validate(v *httpx.V) {
	// z.record(z.unknown())
	switch {
	case !b.Progress.Set:
		v.Add("progress", "Required")
	case b.Progress.Null:
		v.Add("progress", "Expected object, received null")
	case rawKind(b.Progress.Val) != "object":
		v.Add("progress", "Expected object, received "+rawKind(b.Progress.Val))
	default:
		var buf bytes.Buffer
		if err := json.Compact(&buf, b.Progress.Val); err != nil {
			v.Add("progress", "Expected object, received "+rawKind(b.Progress.Val))
			return
		}
		b.progress = buf.Bytes()
	}
}

// ===== 查询参数 =====

type userQuery struct {
	UserID httpx.Opt[string] `json:"userId"`

	userID string
}

func (q *userQuery) Validate(v *httpx.V) {
	if s := v.OptStr("userId", q.UserID); s != nil {
		q.userID = *s
	}
}

type dailyQuery struct {
	Days httpx.Opt[string] `json:"days"`

	days int
}

func (q *dailyQuery) Validate(v *httpx.V) {
	def := 30
	q.days = v.CoerceInt("days", q.Days, &def, httpx.Between(1, 180))
}

type sessionsQuery struct {
	Page  httpx.Opt[string] `json:"page"`
	Limit httpx.Opt[string] `json:"limit"`
	Kind  httpx.Opt[string] `json:"kind"`

	page, limit int
	kind        string
}

func pageLimit(v *httpx.V, page, limit httpx.Opt[string]) (int, int) {
	one, twenty := 1, 20
	return v.CoerceInt("page", page, &one, httpx.IntRange{Min: &one}), v.CoerceInt("limit", limit, &twenty, httpx.Between(1, 100))
}

func (q *sessionsQuery) Validate(v *httpx.V) {
	q.page, q.limit = pageLimit(v, q.Page, q.Limit)
	q.kind = v.OptEnum("kind", q.Kind, sessionKindValues, "", "")
}

type wordsQuery struct {
	Page   httpx.Opt[string] `json:"page"`
	Limit  httpx.Opt[string] `json:"limit"`
	Filter httpx.Opt[string] `json:"filter"`
	Q      httpx.Opt[string] `json:"q"`

	page, limit int
	filter, q   string
}

func (q *wordsQuery) Validate(v *httpx.V) {
	q.page, q.limit = pageLimit(v, q.Page, q.Limit)
	q.filter = v.OptEnum("filter", q.Filter, service.WordFilters, "", "all")
	if s := v.OptStr("q", q.Q, httpx.Trim()); s != nil {
		q.q = *s
	}
}

// targetUser 目标用户：默认本人；查看他人需权限与数据范围（旧 targetUser）。
func targetUser(req *http.Request, d *Deps) (string, error) {
	actor := auth.ActorFrom(req.Context())
	q, err := httpx.DecodeQuery[userQuery](req)
	if err != nil {
		return "", err
	}
	target := q.userID
	if target == "" {
		target = actor.ID
	}
	if err := service.AssertCanViewUser(req.Context(), d.DB, actor, target); err != nil {
		return "", err
	}
	return target, nil
}

// ===== 输出 =====

// todayView GET /today。
type todayView struct {
	core.TodaySummary
	DrillAvailable int                    `json:"drillAvailable"`
	Sheet          *service.NextSheetView `json:"sheet"`
	// GradedSheets 今天批改提交的默写单（spec 0006，今日页显示「已批改」；sheet 照旧只给待测 / 待批改的下一份）。
	GradedSheets []service.GradedSheetToday `json:"gradedSheets"`
	Streak       int                        `json:"streak"`
	Stats        service.TodayStats         `json:"stats"`
	LearnedWords int                        `json:"learnedWords"`
	// OutsideTargetPlanIDs 今日计划里有单元所在的书不在我的目标词书内的计划（spec 0008，卡片标「不在目标词书内」）。
	OutsideTargetPlanIDs []string `json:"outsideTargetPlanIds"`
	// PausedSelfPlans 因班级未开放自主安排而暂停的自建计划数（不在今日队列里，今日页提示一句）。
	PausedSelfPlans int `json:"pausedSelfPlans"`
}

// sanitizedItem 进行中检测的题目（不含标准答案与例句）。
type sanitizedItem struct {
	WordID        string   `json:"wordId"`
	Spelling      string   `json:"spelling"`
	Answer        string   `json:"answer"`
	Phonetic      *string  `json:"phonetic"`
	PartOfSpeech  *string  `json:"partOfSpeech"`
	Definition    string   `json:"definition"`
	Example       *string  `json:"example"`
	ExampleCn     *string  `json:"exampleCn"`
	Type          string   `json:"type"`
	IsNew         bool     `json:"isNew"`
	Options       []string `json:"options"`
	Modes         []string `json:"modes"`
	Cloze         *string  `json:"cloze"`
	ClozeCn       *string  `json:"clozeCn"`
	Letters       int      `json:"letters"`
	WordsInAnswer int      `json:"wordsInAnswer"`
}

var nonLetterRe = regexp.MustCompile(`[^a-zA-Z]`)

// sanitizeTestItem 进行中检测只下发作答所需信息：认义题给英文与选项，拼写题给释义与字母数。
func sanitizeTestItem(snap *service.Snapshot, i *service.SnapshotItem) sanitizedItem {
	target := i.Answer
	if target == "" {
		target = i.Spelling
	}
	itemModes := snap.ItemModes(i)
	out := sanitizedItem{
		WordID: i.WordID, PartOfSpeech: i.PartOfSpeech, Type: i.Type, Options: i.Options, Modes: itemModes,
		Cloze: i.Cloze, ClozeCn: i.ClozeCn,
		Letters:       len(nonLetterRe.ReplaceAllString(target, "")),
		WordsInAnswer: len(strings.FieldsFunc(target, isJSSpaceRune)),
	}
	if slices.Contains(snap.Modes, "recognition") {
		out.Spelling = i.Spelling
		out.Phonetic = i.Phonetic
	}
	if slices.Contains(itemModes, "spelling") {
		out.Definition = i.Definition
	}
	if out.Options == nil {
		out.Options = []string{}
	}
	return out
}

func isJSSpaceRune(r rune) bool { return httpx.JSTrim(string(r)) == "" }

type sessionAnswerView struct {
	WordID     string  `json:"wordId"`
	Mode       string  `json:"mode"`
	Phase      string  `json:"phase"`
	Attempt    int     `json:"attempt"`
	Correct    *bool   `json:"correct"`
	UserAnswer *string `json:"userAnswer"`
	HintUsed   bool    `json:"hintUsed"`
	DontKnow   bool    `json:"dontKnow"`
}

type sessionView struct {
	ID          string              `json:"id"`
	Kind        string              `json:"kind"`
	Status      string              `json:"status"`
	UserID      string              `json:"userId"`
	PlanID      *string             `json:"planId"`
	SheetID     *string             `json:"sheetId"`
	PlanName    *string             `json:"planName"`
	Modes       []string            `json:"modes"`
	Items       []any               `json:"items"`
	Answers     []sessionAnswerView `json:"answers"`
	Progress    json.RawMessage     `json:"progress"`
	Result      json.RawMessage     `json:"result"`
	StartedAt   store.Time          `json:"startedAt"`
	CompletedAt store.NullTime      `json:"completedAt"`
	IsOwner     bool                `json:"isOwner"`
}

func rawJSON(s *string) json.RawMessage {
	if s == nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(*s)
}

func registerStudy(r *Router, d *Deps) {
	study := auth.RequireCap(core.CapStudy)
	records := auth.RequireCap(core.CapStudy, core.CapStudentsView)

	// ===== 今日 =====

	r.Get("/today", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		now := d.Now()
		loc := d.Cfg.Location
		queue, err := service.ComputeToday(ctx, d.DB, loc, now, actor.ID)
		if err != nil {
			return err
		}
		drill, err := service.DrillCandidates(ctx, d.DB, loc, now, actor.ID)
		if err != nil {
			return err
		}
		summary, err := service.UserSummary(ctx, d.DB, loc, now, actor.ID, &queue)
		if err != nil {
			return err
		}
		sheet, err := service.NextSheet(ctx, d.DB, actor.ID)
		if err != nil {
			return err
		}
		graded, err := service.SheetsGradedToday(ctx, d.DB, actor.ID, core.DayKeyOf(now, loc))
		if err != nil {
			return err
		}
		planIDs := make([]string, len(queue.Plans))
		for i, p := range queue.Plans {
			planIDs[i] = p.PlanID
		}
		outside, paused, err := service.TodayPlanFlags(ctx, d.DB, actor, planIDs)
		if err != nil {
			return err
		}
		httpx.OK(w, todayView{TodaySummary: queue, DrillAvailable: len(drill), Sheet: sheet, GradedSheets: graded, Streak: summary.Streak,
			Stats: summary.Today, LearnedWords: summary.LearnedWords, OutsideTargetPlanIDs: outside, PausedSelfPlans: paused})
		return nil
	}, study)

	// ===== 学习组 =====

	r.Post("/study/sessions", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[startBody](req)
		if err != nil {
			return err
		}
		res, err := service.StartSession(ctx, d.DB, d.Cfg.Location, d.Now(), d.Rand, actor.ID, body.in)
		if err != nil {
			return err
		}
		httpx.OK(w, map[string]any{"id": res.Session.ID, "resumed": res.Resumed})
		return nil
	}, study)

	r.Get("/study/sessions/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		s, err := service.GetSession(ctx, d.DB, req.PathValue("id"))
		if err != nil {
			return err
		}
		if s == nil {
			return httpx.NotFound("学习组不存在")
		}
		if s.UserID != actor.ID {
			if err := service.AssertCanViewUser(ctx, d.DB, actor, s.UserID); err != nil {
				return err
			}
		}
		snap, err := service.ParseSnapshot(s.SnapshotText)
		if err != nil {
			return err
		}
		answers, err := service.SessionAnswers(ctx, d.DB, s.ID)
		if err != nil {
			return err
		}
		hide := core.IsTestKind(s.Kind) && s.Status == "active"
		items := make([]any, len(snap.Items))
		for i := range snap.Items {
			if hide {
				items[i] = sanitizeTestItem(snap, &snap.Items[i])
			} else {
				items[i] = snap.RawItem(i)
			}
		}
		av := make([]sessionAnswerView, len(answers))
		for i, a := range answers {
			c := a.Correct
			av[i] = sessionAnswerView{WordID: a.WordID, Mode: a.Mode, Phase: a.Phase, Attempt: a.Attempt, Correct: &c,
				UserAnswer: a.UserAnswer, HintUsed: a.HintUsed, DontKnow: a.DontKnow}
			if hide {
				av[i].Correct = nil
			}
		}
		httpx.OK(w, sessionView{
			ID: s.ID, Kind: s.Kind, Status: s.Status, UserID: s.UserID, PlanID: s.PlanID, SheetID: s.SheetID,
			PlanName: snap.PlanName, Modes: snap.Modes, Items: items, Answers: av, Progress: rawJSON(s.Progress),
			Result: rawJSON(s.Result), StartedAt: s.StartedAt, CompletedAt: s.CompletedAt, IsOwner: s.UserID == actor.ID,
		})
		return nil
	}, study)

	r.Post("/study/sessions/{id}/answers", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[answerBody](req)
		if err != nil {
			return err
		}
		res, err := service.RecordAnswer(ctx, d.DB, d.Cfg.Location, d.Now(), actor.ID, req.PathValue("id"), body.in)
		if err != nil {
			return err
		}
		httpx.OK(w, res)
		return nil
	}, study)

	r.Patch("/study/sessions/{id}/progress", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[progressBody](req)
		if err != nil {
			return err
		}
		if err := service.SaveProgress(ctx, d.DB, actor.ID, req.PathValue("id"), body.progress); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, study)

	r.Post("/study/sessions/{id}/complete", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		out, err := service.CompleteSession(ctx, d.DB, d.Cfg.Location, d.Now(), actor.ID, req.PathValue("id"))
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, study)

	r.Delete("/study/sessions/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		if err := service.DiscardSession(ctx, d.DB, actor.ID, req.PathValue("id")); err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, study)

	// ===== 记录 =====

	r.Get("/records/summary", func(w http.ResponseWriter, req *http.Request) error {
		userID, err := targetUser(req, d)
		if err != nil {
			return err
		}
		out, err := service.UserSummary(req.Context(), d.DB, d.Cfg.Location, d.Now(), userID, nil)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, records)

	r.Get("/records/daily", func(w http.ResponseWriter, req *http.Request) error {
		userID, err := targetUser(req, d)
		if err != nil {
			return err
		}
		q, err := httpx.DecodeQuery[dailyQuery](req)
		if err != nil {
			return err
		}
		out, err := service.UserDaily(req.Context(), d.DB, d.Cfg.Location, d.Now(), userID, q.days)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, records)

	r.Get("/records/sessions", func(w http.ResponseWriter, req *http.Request) error {
		userID, err := targetUser(req, d)
		if err != nil {
			return err
		}
		q, err := httpx.DecodeQuery[sessionsQuery](req)
		if err != nil {
			return err
		}
		out, err := service.UserSessions(req.Context(), d.DB, userID, q.page, q.limit, q.kind)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, records)

	r.Get("/records/sessions/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		out, err := service.SessionRecord(ctx, d.DB, auth.ActorFrom(ctx), req.PathValue("id"))
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, records)

	r.Get("/records/words", func(w http.ResponseWriter, req *http.Request) error {
		userID, err := targetUser(req, d)
		if err != nil {
			return err
		}
		q, err := httpx.DecodeQuery[wordsQuery](req)
		if err != nil {
			return err
		}
		out, err := service.UserWords(req.Context(), d.DB, d.Cfg.Location, d.Now(), userID, q.filter, q.q, q.page, q.limit)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, records)

	r.Get("/records/words/{wordId}", func(w http.ResponseWriter, req *http.Request) error {
		userID, err := targetUser(req, d)
		if err != nil {
			return err
		}
		out, err := service.UserWordHistory(req.Context(), d.DB, d.Cfg.Location, d.Now(), userID, req.PathValue("wordId"))
		if err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}, records)
}

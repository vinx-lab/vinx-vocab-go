package api

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"

	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// AI 内容生成、发音、短文（对应旧 routes/ai.ts）。

// ---------------------------------------------------------------------------
// 请求体
// ---------------------------------------------------------------------------

var passageSources = []string{"due", "difficult", "recent", "manual"}

type passagePreviewBody struct {
	WordIDs httpx.Opt[[]string] `json:"wordIds"`
	Source  httpx.Opt[string]   `json:"source"`
	Count   httpx.Opt[float64]  `json:"count"`
	Topic   httpx.Opt[string]   `json:"topic"`
	Level   httpx.Opt[string]   `json:"level"` // spec 0005：临时学段

	wordIDs []string
	source  string
	count   int
	topic   *string
	level   *string
}

// optLevel 可选的学段（spec 0005）：缺省返回 nil，非法 → 400。
func optLevel(v *httpx.V, o httpx.Opt[string]) *string {
	if !o.Set {
		return nil
	}
	if l := v.Enum("level", o, coreai.Levels, ""); l != "" {
		return &l
	}
	return nil
}

func (b *passagePreviewBody) Validate(v *httpx.V) {
	b.level = optLevel(v, b.Level)
	if b.WordIDs.Set {
		if b.WordIDs.Null {
			v.Add("wordIds", "Expected array, received null")
		} else {
			b.wordIDs = b.WordIDs.Val
			v.ArrayLen("wordIds", len(b.wordIDs), -1, service.PassageWordLimit, "", "")
		}
	}
	b.source = v.OptEnum("source", b.Source, passageSources, "", "manual")
	eight := 8
	b.count = v.Int("count", b.Count, &eight, httpx.Between(3, service.PassageWordLimit))
	b.topic = v.OptStr("topic", b.Topic, httpx.Trim(), httpx.Max(60))
}

// passageGenerateBody 与 passagePreviewBody 字段相同（不用匿名嵌入：httpx.Decode 按 JSON 顶层键名逐字段
// 匹配，不会像 encoding/json 那样展开匿名字段），额外带 prompt。
type passageGenerateBody struct {
	WordIDs httpx.Opt[[]string] `json:"wordIds"`
	Source  httpx.Opt[string]   `json:"source"`
	Count   httpx.Opt[float64]  `json:"count"`
	Topic   httpx.Opt[string]   `json:"topic"`
	Level   httpx.Opt[string]   `json:"level"`
	Prompt  httpx.Opt[string]   `json:"prompt"`

	preview passagePreviewBody
	prompt  *string
}

func (b *passageGenerateBody) Validate(v *httpx.V) {
	b.preview = passagePreviewBody{WordIDs: b.WordIDs, Source: b.Source, Count: b.Count, Topic: b.Topic, Level: b.Level}
	b.preview.Validate(v)
	b.prompt = v.OptStr("prompt", b.Prompt, httpx.Max(20_000))
}

type unitExamplesBody struct {
	WordIDs     httpx.Opt[[]string] `json:"wordIds"`
	Limit       httpx.Opt[float64]  `json:"limit"`
	OverwriteAi httpx.Opt[bool]     `json:"overwriteAi"`
	Prompt      httpx.Opt[string]   `json:"prompt"`
	Level       httpx.Opt[string]   `json:"level"` // spec 0005

	wordIDs     []string
	hasWordIDs  bool
	limit       int
	overwriteAi bool
	prompt      *string
	level       *string
}

func (b *unitExamplesBody) Validate(v *httpx.V) {
	b.level = optLevel(v, b.Level)
	if b.WordIDs.Set {
		if b.WordIDs.Null {
			v.Add("wordIds", "Expected array, received null")
		} else {
			b.wordIDs, b.hasWordIDs = b.WordIDs.Val, true
			v.ArrayLen("wordIds", len(b.wordIDs), -1, service.ExampleBatchLimit, "", "")
		}
	}
	def := service.ExampleBatchLimit
	b.limit = v.Int("limit", b.Limit, &def, httpx.Between(1, service.ExampleBatchLimit))
	b.overwriteAi = v.OptBool("overwriteAi", b.OverwriteAi, false)
	b.prompt = v.OptStr("prompt", b.Prompt, httpx.Max(20_000))
}

type wordExampleBody struct {
	WordIDs httpx.Opt[[]string] `json:"wordIds"`
	Prompt  httpx.Opt[string]   `json:"prompt"`
	Level   httpx.Opt[string]   `json:"level"` // spec 0005

	wordIDs []string
	prompt  *string
	level   *string
}

func (b *wordExampleBody) Validate(v *httpx.V) {
	b.level = optLevel(v, b.Level)
	if b.WordIDs.Set {
		if b.WordIDs.Null {
			v.Add("wordIds", "Expected array, received null")
		} else {
			b.wordIDs = b.WordIDs.Val
			v.ArrayLen("wordIds", len(b.wordIDs), -1, 1, "", "")
		}
	}
	b.prompt = v.OptStr("prompt", b.Prompt, httpx.Max(20_000))
}

// levelOnlyBody 单个词例句预览：原来不读请求体，spec 0005 只增加可选的 level（其余字段忽略）。
type levelOnlyBody struct {
	Level httpx.Opt[string] `json:"level"`
	level *string
}

func (b *levelOnlyBody) Validate(v *httpx.V) { b.level = optLevel(v, b.Level) }

// visiblePrompt 页面提交的可见提示词：不能为空、最多 6000 字（去掉首尾空白后）。
func visiblePrompt(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	r := coreai.ValidatePromptText(*raw)
	if !r.OK {
		return nil, httpx.Validation(r.Message)
	}
	return &r.Value, nil
}

// assertAiEnabled 生成前先确认 AI 可用，免得任务一开始就失败。
func assertAiEnabled(ctx context.Context, d *Deps) error {
	if !d.AIEnabled(ctx) {
		return httpx.NewError(httpx.CodeInvalidAction, "未配置 AI 服务（接口地址或 API Key），该功能不可用", nil)
	}
	return nil
}

var missingExampleWhere = `(w."example" IS NULL OR w."example" = '')`

// unitExampleWordIDs 单元里要补例句的词：给了 wordIds 就取其中属于本单元的（保持顺序），否则按缺例句挑。
func unitExampleWordIDs(ctx context.Context, q store.Querier, unitID string, wordIDs []string, hasWordIDs bool, limit int, overwriteAi bool) ([]string, error) {
	if hasWordIDs {
		rows, err := q.QueryContext(ctx, `SELECT "wordId" FROM "UnitWord" WHERE "unitId" = ? AND "wordId" IN (`+store.Placeholders(len(wordIDs))+`)`, append([]any{unitID}, store.Args(wordIDs)...)...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		inUnit := map[string]bool{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			inUnit[id] = true
		}
		out := make([]string, 0, len(wordIDs))
		for _, id := range wordIDs {
			if inUnit[id] {
				out = append(out, id)
			}
		}
		return out, nil
	}
	where := `uw."unitId" = ? AND ` + missingExampleWhere
	if overwriteAi {
		where = `uw."unitId" = ? AND (` + missingExampleWhere + ` OR w."exampleSource" = 'ai')`
	}
	rows, err := q.QueryContext(ctx, `SELECT uw."wordId" FROM "UnitWord" uw JOIN "Word" w ON w."id" = uw."wordId" WHERE `+where+` ORDER BY uw."sortOrder" ASC LIMIT ?`, unitID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func countMissingExamples(ctx context.Context, q store.Querier, unitID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM "UnitWord" uw JOIN "Word" w ON w."id" = uw."wordId" WHERE uw."unitId" = ? AND `+missingExampleWhere, unitID).Scan(&n)
	return n, err
}

// assertUnitEditable 单元存在且当前账号能编辑它的词书。
func assertUnitEditable(ctx context.Context, d *Deps, unitID string) error {
	actor := auth.ActorFrom(ctx)
	u, err := service.UnitBook(ctx, d.DB, unitID)
	if err != nil {
		return err
	}
	_, err = service.AssertCanEditBook(ctx, d.DB, actor, u.BookID)
	return err
}

// assertWordEditable 单词存在且当前账号能编辑它所在的全部词书。
func assertWordEditable(ctx context.Context, d *Deps, wordID string) error {
	actor := auth.ActorFrom(ctx)
	var exists int
	if err := d.DB.QueryRowContext(ctx, `SELECT count(*) FROM "Word" WHERE "id" = ?`, wordID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return httpx.NotFound("单词不存在")
	}
	bookIDs, err := queryStringsLocal(ctx, d.DB, `SELECT DISTINCT u."bookId" FROM "UnitWord" uw JOIN "Unit" u ON u."id" = uw."unitId" WHERE uw."wordId" = ?`, wordID)
	if err != nil {
		return err
	}
	for _, bid := range bookIDs {
		if _, err := service.AssertCanEditBook(ctx, d.DB, actor, bid); err != nil {
			return err
		}
	}
	return nil
}

func queryStringsLocal(ctx context.Context, q store.Querier, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// pickWords 短文取词来源：到期复习 / 易错难词 / 最近学过。
func pickWords(ctx context.Context, d *Deps, userID, source string, count int) ([]string, error) {
	now := d.Now()
	dayKey := core.DayKeyOf(now, d.Cfg.Location)
	_, end := core.DayRange(dayKey, d.Cfg.Location)
	switch source {
	case "due":
		return queryStringsLocal(ctx, d.DB, `SELECT "wordId" FROM "MemoryState" WHERE "userId" = ? AND "due" < ? ORDER BY "due" ASC LIMIT ?`, userID, end, count)
	case "difficult":
		return queryStringsLocal(ctx, d.DB, `SELECT "wordId" FROM "MemoryState" WHERE "userId" = ? AND ("lapses" >= 1 OR "difficulty" >= 7) ORDER BY "lapses" DESC, "difficulty" DESC LIMIT ?`, userID, count)
	default: // recent
		since := core.AddDays(dayKey, -13)
		return queryStringsLocal(ctx, d.DB, `SELECT "wordId" FROM "MemoryState" WHERE "userId" = ? AND "introducedDay" >= ? ORDER BY "introducedAt" DESC LIMIT ?`, userID, since, count)
	}
}

func wordsForPreview(ctx context.Context, d *Deps, actorID string, body passagePreviewBody) ([]string, error) {
	if body.wordIDs != nil || body.source == "manual" {
		return body.wordIDs, nil
	}
	return pickWords(ctx, d, actorID, body.source, body.count)
}

// ---------------------------------------------------------------------------
// 后台任务：在独立的上下文里跑，写库用 d.DB（不复用请求 ctx，避免请求结束后被取消）。
// ---------------------------------------------------------------------------

func startAIJob[T any](d *Deps, ownerID string, kind coreai.JobKind, run func(ctx context.Context) (T, error)) (coreai.JobStarted, error) {
	job, err := d.AIJobs.Start(ownerID, kind)
	if err != nil {
		return coreai.JobStarted{}, httpx.NewError(httpx.CodeTooManyRequests, err.Error(), nil)
	}
	go func() {
		ctx := context.Background()
		// I3：任务在独立 goroutine 里跑，router.go 的 recover 只接得住 HTTP 处理函数里的 panic，接不住
		// 这里的。不接住的话一次 panic 会让整个进程退出，所有人的任务和正在处理的请求都被打断，比
		// oracle 的等价路径（async 异常，被 catch 成 failed）影响大得多。接住后按普通错误一样走
		// DescribeError（脱敏 Key，说明文案与真实错误一致：都是 "AI 调用失败：<message>" 这一支路径），
		// 并记日志带堆栈方便排查。
		defer func() {
			if p := recover(); p != nil {
				slog.Error("AI 任务 panic", "jobId", job.ID, "kind", kind, "ownerId", ownerID, "panic", p, "stack", string(debug.Stack()))
				d.failAIJob(ctx, job.ID, panicToError(p))
			}
		}()
		result, err := run(ctx)
		if err != nil {
			d.failAIJob(ctx, job.ID, err)
			return
		}
		d.AIJobs.Finish(job.ID, result)
	}()
	return coreai.JobStarted{JobID: job.ID}, nil
}

// panicToError 把 recover() 拿到的任意值转成 error（供 DescribeError 使用）。
func panicToError(p any) error {
	if e, ok := p.(error); ok {
		return e
	}
	return fmt.Errorf("%v", p)
}

// failAIJob 统一的任务失败处理：按当前 AI 配置取 Key/超时（用于脱敏与超时说明），转成具体说明后写入任务。
func (d *Deps) failAIJob(ctx context.Context, jobID string, err error) {
	cfg, cfgErr := d.AIConfig.Config(ctx, d.DB, d.Cfg)
	apiKey, timeoutMs := "", 0
	if cfgErr == nil {
		apiKey, timeoutMs = cfg.APIKey, cfg.TimeoutMs
	}
	d.AIJobs.Fail(jobID, coreai.DescribeError(err, coreai.ErrorContext{TimeoutMs: timeoutMs, APIKey: apiKey}))
}

// ---------------------------------------------------------------------------
// 路由
// ---------------------------------------------------------------------------

func registerAI(r *Router, d *Deps) {
	// 能力探测：前端据此显示/隐藏 AI 与发音入口。
	r.Get("/ai/status", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		st, err := service.AIStatus(ctx, d.AIConfig, d.DB, d.Cfg)
		if err != nil {
			return err
		}
		httpx.OK(w, struct {
			service.AIStatusView
			Audio bool `json:"audio"`
		}{st, service.AudioEnabled(d.Cfg)})
		return nil
	}, auth.RequireAuth)

	// ===== 真人发音（按需抓取 + 本地缓存）=====
	r.Get("/audio/words/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		id := req.PathValue("id")
		buf, _, err := service.GetWordAudio(ctx, d.DB, d.Cfg, id)
		if err != nil {
			return err
		}
		w.Header().Set("content-type", "audio/mpeg")
		w.Header().Set("cache-control", "public, max-age=2592000, immutable")
		w.Header().Set("content-length", strconv.Itoa(len(buf)))
		w.WriteHeader(http.StatusOK)
		w.Write(buf)
		return nil
	}, auth.RequireAuth)

	// 预缓存整单元发音（老师备课时一次拉好，学生端零等待）。
	r.Post("/audio/units/{id}/prefetch", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		id := req.PathValue("id")
		ids, err := queryStringsLocal(ctx, d.DB, `SELECT "wordId" FROM "UnitWord" WHERE "unitId" = ? ORDER BY "sortOrder" ASC LIMIT 500`, id)
		if err != nil {
			return err
		}
		res, err := service.PrefetchAudio(ctx, d.DB, d.Cfg, ids)
		if err != nil {
			return err
		}
		httpx.OK(w, res)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	// ===== 后台任务进度：只有发起人能查 =====
	r.Get("/ai/jobs/{id}", func(w http.ResponseWriter, req *http.Request) error {
		actor := auth.ActorFrom(req.Context())
		id := req.PathValue("id")
		view, ok := d.AIJobs.View(id, actor.ID)
		if !ok {
			return httpx.NotFound("任务不存在或已过期")
		}
		httpx.OK(w, view)
		return nil
	}, auth.RequireAuth)

	// ===== AI 例句 =====
	// 预览：单词 + 默认可见提示词，不调用 AI。
	r.Post("/ai/words/{id}/example/preview", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		id := req.PathValue("id")
		if err := assertWordEditable(ctx, d, id); err != nil {
			return err
		}
		body, err := httpx.Decode[levelOnlyBody](req)
		if err != nil {
			return err
		}
		level, err := service.WordLevel(ctx, d.DB, id, body.level)
		if err != nil {
			return err
		}
		preview, err := service.PreviewExamples(ctx, d.AIConfig, d.DB, []string{id}, level)
		if err != nil {
			return err
		}
		httpx.OK(w, preview)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	// 生成：立即返回任务号，例句在后台生成并写回词书。
	r.Post("/ai/words/{id}/example", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		id := req.PathValue("id")
		actor := auth.ActorFrom(ctx)
		if err := assertWordEditable(ctx, d, id); err != nil {
			return err
		}
		body, err := httpx.Decode[wordExampleBody](req)
		if err != nil {
			return err
		}
		for _, w := range body.wordIDs {
			if w != id {
				return httpx.Validation("单词与地址不一致")
			}
		}
		prompt, err := visiblePrompt(body.prompt)
		if err != nil {
			return err
		}
		if err := assertAiEnabled(ctx, d); err != nil {
			return err
		}
		level, err := service.WordLevel(ctx, d.DB, id, body.level)
		if err != nil {
			return err
		}
		known, err := service.KnownWordsForWord(ctx, d.DB, id)
		if err != nil {
			return err
		}
		started, err := startAIJob(d, actor.ID, coreai.JobKindExample, func(ctx context.Context) (service.AiExamplesJobResult, error) {
			cfg, err := d.AIConfig.Config(ctx, d.DB, d.Cfg)
			if err != nil {
				return service.AiExamplesJobResult{}, err
			}
			r, err := service.GenerateExamples(ctx, d.AIConfig, d.DB, d.Now(), cfg.ToCallConfig(), []string{id}, prompt, service.GenOptions{Level: level, Known: known})
			if err != nil {
				return service.AiExamplesJobResult{}, err
			}
			if len(r.Items) == 0 {
				return service.AiExamplesJobResult{}, coreai.NewAPIError("SERVER", "AI 没能生成合格的例句（回复里没有用上这个词），请重试")
			}
			return r, nil
		})
		if err != nil {
			return err
		}
		httpx.OK(w, started)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	// 单元批量补例句（每次最多 EXAMPLE_BATCH_LIMIT 个）：先预览再生成。
	r.Post("/ai/units/{id}/examples/preview", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		id := req.PathValue("id")
		if err := assertUnitEditable(ctx, d, id); err != nil {
			return err
		}
		body, err := httpx.Decode[unitExamplesBody](req)
		if err != nil {
			return err
		}
		ids, err := unitExampleWordIDs(ctx, d.DB, id, body.wordIDs, body.hasWordIDs, body.limit, body.overwriteAi)
		if err != nil {
			return err
		}
		level, err := service.UnitLevel(ctx, d.DB, id, body.level)
		if err != nil {
			return err
		}
		preview, err := service.PreviewExamples(ctx, d.AIConfig, d.DB, ids, level)
		if err != nil {
			return err
		}
		remaining, err := countMissingExamples(ctx, d.DB, id)
		if err != nil {
			return err
		}
		httpx.OK(w, service.AiUnitExamplesPreview{AiPreview: preview, Remaining: remaining})
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	r.Post("/ai/units/{id}/examples", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		id := req.PathValue("id")
		actor := auth.ActorFrom(ctx)
		if err := assertUnitEditable(ctx, d, id); err != nil {
			return err
		}
		body, err := httpx.Decode[unitExamplesBody](req)
		if err != nil {
			return err
		}
		prompt, err := visiblePrompt(body.prompt)
		if err != nil {
			return err
		}
		ids, err := unitExampleWordIDs(ctx, d.DB, id, body.wordIDs, body.hasWordIDs, body.limit, body.overwriteAi)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return httpx.NewError(httpx.CodeNoData, "没有需要补例句的单词", nil)
		}
		if err := assertAiEnabled(ctx, d); err != nil {
			return err
		}
		level, err := service.UnitLevel(ctx, d.DB, id, body.level)
		if err != nil {
			return err
		}
		known, err := service.KnownWordsUpToUnit(ctx, d.DB, id)
		if err != nil {
			return err
		}
		started, err := startAIJob(d, actor.ID, coreai.JobKindExamples, func(ctx context.Context) (service.AiExamplesJobResult, error) {
			cfg, err := d.AIConfig.Config(ctx, d.DB, d.Cfg)
			if err != nil {
				return service.AiExamplesJobResult{}, err
			}
			r, err := service.GenerateExamples(ctx, d.AIConfig, d.DB, d.Now(), cfg.ToCallConfig(), ids, prompt, service.GenOptions{Level: level, Known: known})
			if err != nil {
				return service.AiExamplesJobResult{}, err
			}
			remaining, err := countMissingExamples(ctx, d.DB, id)
			if err != nil {
				return service.AiExamplesJobResult{}, err
			}
			r.Remaining = &remaining
			return r, nil
		})
		if err != nil {
			return err
		}
		httpx.OK(w, started)
		return nil
	}, auth.RequireCap(core.CapBooksEdit))

	// ===== AI 巩固短文 =====
	// 预览：按来源和词数挑词（或用给定的词），拼出默认可见提示词，不调用 AI。
	r.Post("/ai/passages/preview", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[passagePreviewBody](req)
		if err != nil {
			return err
		}
		ids, err := wordsForPreview(ctx, d, actor.ID, body)
		if err != nil {
			return err
		}
		level, err := service.LearnerLevel(ctx, d.DB, service.TargetUsesClasses(actor), actor.ID, body.level)
		if err != nil {
			return err
		}
		preview, err := service.PreviewPassage(ctx, d.AIConfig, d.DB, ids, valOr(body.topic, ""), level)
		if err != nil {
			return err
		}
		httpx.OK(w, preview)
		return nil
	}, auth.RequireCap(core.CapStudy))

	// 生成：立即返回任务号；短文在后台生成后存进短文列表。
	r.Post("/passages/generate", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		body, err := httpx.Decode[passageGenerateBody](req)
		if err != nil {
			return err
		}
		prompt, err := visiblePrompt(body.prompt)
		if err != nil {
			return err
		}
		ids, err := wordsForPreview(ctx, d, actor.ID, body.preview)
		if err != nil {
			return err
		}
		if len(ids) < 3 {
			return httpx.NewError(httpx.CodeNoData, "可用的单词不足 3 个，先去学几个词再来", nil)
		}
		if err := assertAiEnabled(ctx, d); err != nil {
			return err
		}
		topic := valOr(body.preview.topic, "")
		level, err := service.LearnerLevel(ctx, d.DB, service.TargetUsesClasses(actor), actor.ID, body.preview.level)
		if err != nil {
			return err
		}
		started, err := startAIJob(d, actor.ID, coreai.JobKindPassage, func(ctx context.Context) (aiPassageJobResult, error) {
			cfg, err := d.AIConfig.Config(ctx, d.DB, d.Cfg)
			if err != nil {
				return aiPassageJobResult{}, err
			}
			result, err := service.GeneratePassage(ctx, d.AIConfig, d.DB, cfg.ToCallConfig(), ids, prompt, topic, level)
			if err != nil {
				return aiPassageJobResult{}, err
			}
			id, err := service.SavePassage(ctx, d.DB, d.Now(), actor.ID, cfg.Model, result)
			if err != nil {
				return aiPassageJobResult{}, err
			}
			wordIDs := make([]string, len(result.Words))
			for i, w := range result.Words {
				wordIDs[i] = w.WordID
			}
			sentences, err := service.CheckPassageSentences(ctx, d.DB, id, actor.ID, wordIDs, level)
			if err != nil {
				return aiPassageJobResult{}, err
			}
			return aiPassageJobResult{ID: id, Title: result.Title, MissingWords: result.MissingWords, Level: level, Sentences: sentences}, nil
		})
		if err != nil {
			return err
		}
		httpx.OK(w, started)
		return nil
	}, auth.RequireCap(core.CapStudy))

	r.Get("/passages", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		q, err := httpx.DecodeQuery[passageListQuery](req)
		if err != nil {
			return err
		}
		rows, err := d.DB.QueryContext(ctx, `SELECT "id","title","titleCn","wordIds","createdAt" FROM "Passage" WHERE "userId" = ? ORDER BY "createdAt" DESC LIMIT ? OFFSET ?`, actor.ID, q.limit, (q.page-1)*q.limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []passageListItem{}
		for rows.Next() {
			var it passageListItem
			var wordIDs store.JSON[[]string]
			var createdAt store.Time
			if err := rows.Scan(&it.ID, &it.Title, &it.TitleCn, &wordIDs, &createdAt); err != nil {
				return err
			}
			it.WordCount = len(wordIDs.V)
			it.CreatedAt = createdAt
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		var total int
		if err := d.DB.QueryRowContext(ctx, `SELECT count(*) FROM "Passage" WHERE "userId" = ?`, actor.ID).Scan(&total); err != nil {
			return err
		}
		httpx.OK(w, httpx.Page(items, total, q.page, q.limit))
		return nil
	}, auth.RequireCap(core.CapStudy))

	r.Get("/passages/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		var p passageDetail
		var userID string
		var wordIDs store.JSON[[]string]
		var questions store.JSON[[]service.PassageQuestion]
		var createdAt store.Time
		err := d.DB.QueryRowContext(ctx, `SELECT "id","userId","title","titleCn","body","bodyCn","questions","wordIds","model","createdAt" FROM "Passage" WHERE "id" = ?`, id).
			Scan(&p.ID, &userID, &p.Title, &p.TitleCn, &p.Passage, &p.PassageCn, &questions, &wordIDs, &p.Model, &createdAt)
		if store.IsNoRows(err) || (err == nil && userID != actor.ID) {
			return httpx.NotFound("短文不存在")
		}
		if err != nil {
			return err
		}
		p.CreatedAt = createdAt
		if questions.Null {
			p.Questions = []service.PassageQuestion{}
		} else {
			p.Questions = questions.V
		}
		p.Words, err = wordsBrief(ctx, d.DB, wordIDs.V)
		if err != nil {
			return err
		}
		// spec 0004：逐句结构（没有拆分的旧短文为空数组）
		if p.Sentences, err = service.PassageSentences(ctx, d.DB, p.ID); err != nil {
			return err
		}
		httpx.OK(w, p)
		return nil
	}, auth.RequireCap(core.CapStudy))

	r.Delete("/passages/{id}", func(w http.ResponseWriter, req *http.Request) error {
		ctx := req.Context()
		actor := auth.ActorFrom(ctx)
		id := req.PathValue("id")
		var userID string
		err := d.DB.QueryRowContext(ctx, `SELECT "userId" FROM "Passage" WHERE "id" = ?`, id).Scan(&userID)
		if store.IsNoRows(err) || (err == nil && userID != actor.ID) {
			return httpx.NotFound("短文不存在")
		}
		if err != nil {
			return err
		}
		err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
			sentenceIDs, err := service.SentenceIDsOfPassage(ctx, tx, id)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM "Passage" WHERE "id" = ?`, id); err != nil {
				return err
			}
			return service.PruneSentences(ctx, tx, sentenceIDs)
		})
		if err != nil {
			return err
		}
		httpx.OK(w, nil)
		return nil
	}, auth.RequireCap(core.CapStudy))
}

func valOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

// aiPassageJobResult 短文任务的结果：已保存的短文 id（页面据此打开短文）。
type aiPassageJobResult struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	MissingWords []string `json:"missingWords"`
	// spec 0005：这次用的学段、保存下来的逐句结构与每句的检查结果
	Level     string                         `json:"level"`
	Sentences []service.PassageSentenceCheck `json:"sentences"`
}

type passageListQuery struct {
	Page  httpx.Opt[string] `json:"page"`
	Limit httpx.Opt[string] `json:"limit"`

	page  int
	limit int
}

func (q *passageListQuery) Validate(v *httpx.V) {
	one, twenty := 1, 20
	q.page = v.CoerceInt("page", q.Page, &one, httpx.IntRange{Min: &one})
	q.limit = v.CoerceInt("limit", q.Limit, &twenty, httpx.Between(1, 100))
}

type passageListItem struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	TitleCn   *string    `json:"titleCn"`
	WordCount int        `json:"wordCount"`
	CreatedAt store.Time `json:"createdAt"`
}

type passageWordBrief struct {
	ID         string  `json:"id"`
	Spelling   string  `json:"spelling"`
	Definition string  `json:"definition"`
	Phonetic   *string `json:"phonetic"`
}

type passageDetail struct {
	ID        string                    `json:"id"`
	Title     string                    `json:"title"`
	TitleCn   *string                   `json:"titleCn"`
	Passage   string                    `json:"passage"`
	PassageCn *string                   `json:"passageCn"`
	Questions []service.PassageQuestion `json:"questions"`
	Words     []passageWordBrief        `json:"words"`
	Model     *string                   `json:"model"`
	CreatedAt store.Time                `json:"createdAt"`
	Sentences []service.SentenceView    `json:"sentences"` // spec 0004
}

func wordsBrief(ctx context.Context, q store.Querier, ids []string) ([]passageWordBrief, error) {
	out := []passageWordBrief{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT "id","spelling","definition","phonetic" FROM "Word" WHERE "id" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var w passageWordBrief
		if err := rows.Scan(&w.ID, &w.Spelling, &w.Definition, &w.Phonetic); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

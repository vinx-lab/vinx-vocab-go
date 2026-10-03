// AI 内容生成：教材风格例句、用指定单词生成巩固短文（对应旧 services/ai.ts）。
//
// 供应商可换，配置来自 AIConfigCache.Config（页面保存的优先，其次环境变量）：
//   - openai：任何 OpenAI 兼容的 /chat/completions 接口；
//   - anthropic：Claude 官方 API（需 API Key）。
//
// 提示词分两部分：可见提示词（要求模板 + 单词列表 + 主题）生成前在页面上预览、可按次编辑，作为用户消息；
// 输出格式隐藏，由这里作为系统消息自动加上（internal/core/ai）。出错时转成具体说明。
// 生成结果一律标记来源 ai 并记录时间，供人工校对；不覆盖人工维护的内容。API Key 不写日志、不出现在错误信息里。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// ---------------------------------------------------------------------------
// 状态
// ---------------------------------------------------------------------------

// AIStatusView GET /ai/status 的返回（不含 audio 字段，由处理函数补上）。
type AIStatusView struct {
	Enabled  bool    `json:"enabled"`
	Provider string  `json:"provider"`
	Model    *string `json:"model"`
	BaseURL  *string `json:"baseUrl"`
}

// AIStatus 当前 AI 能力状态。
func AIStatus(ctx context.Context, cache *AIConfigCache, q store.Querier, cfg *config.Config) (AIStatusView, error) {
	c, err := cache.Config(ctx, q, cfg)
	if err != nil {
		return AIStatusView{}, err
	}
	v := AIStatusView{Enabled: c.Provider != coreai.ProviderNone, Provider: c.Provider}
	if c.Provider != coreai.ProviderNone {
		m := c.Model
		v.Model = &m
	}
	if c.Provider == coreai.ProviderOpenAI {
		v.BaseURL = c.BaseURL
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// 调用供应商
// ---------------------------------------------------------------------------

type chatReply struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// callProvider 发一次请求拿回复文本；非 2xx 抛「AI 服务返回 <状态码>：<上游错误信息>」。
func callProvider(ctx context.Context, cfg coreai.CallConfig, system, user string, maxTokens int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
	defer cancel()

	anthropic := cfg.Provider == coreai.ProviderAnthropic
	var url string
	var body []byte
	var err error
	if anthropic {
		base := "https://api.anthropic.com"
		if cfg.BaseURL != nil && *cfg.BaseURL != "" {
			base = *cfg.BaseURL
		}
		url = strings.TrimSuffix(base, "/") + "/v1/messages"
		body, err = json.Marshal(map[string]any{
			"model":      cfg.Model,
			"max_tokens": maxTokens,
			"system":     system,
			"messages":   []map[string]string{{"role": "user", "content": user}},
		})
	} else {
		url = strings.TrimSuffix(*cfg.BaseURL, "/") + "/chat/completions"
		body, err = json.Marshal(map[string]any{
			"model":       cfg.Model,
			"max_tokens":  maxTokens,
			"temperature": 0.6,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": user},
			},
		})
	}
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	if anthropic {
		req.Header.Set("x-api-key", cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if cfg.APIKey != "" {
		req.Header.Set("authorization", "Bearer "+cfg.APIKey)
	}

	// httpClient（audio.go）：有意跟随系统代理变量，见那里的注释（评审 M6）。
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", coreai.NewAPIError("SERVER", coreai.UpstreamHTTPMessage(resp.StatusCode, coreai.UpstreamErrorText(string(respBody)), cfg.APIKey))
	}
	var reply chatReply
	if err := json.Unmarshal(respBody, &reply); err != nil {
		return "", coreai.NewAPIError("SERVER", fmt.Sprintf("AI 服务返回的不是 JSON：%s", collapseAndTrim(string(respBody), 160)))
	}
	var text string
	if anthropic {
		var parts []string
		for _, b := range reply.Content {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		text = strings.TrimSpace(strings.Join(parts, "\n"))
	} else if len(reply.Choices) > 0 {
		text = strings.TrimSpace(reply.Choices[0].Message.Content)
	}
	if text == "" {
		return "", coreai.NewAPIError("SERVER", coreai.EmptyReply)
	}
	return text, nil
}

var wsRunRe = regexp.MustCompile(`\s+`)

func collapseAndTrim(s string, n int) string {
	s = wsRunRe.ReplaceAllString(strings.TrimSpace(s), " ")
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// AskWith 用指定配置问一次拿文本；两种供应商都走 HTTP。出错一律转成具体说明（超时 / 连不上 / 上游 HTTP
// 状态 / 没有内容），并去掉 Key。
func AskWith(ctx context.Context, cfg coreai.CallConfig, system, user string, maxTokens int) (string, error) {
	if cfg.Provider == coreai.ProviderNone {
		return "", coreai.NewAPIError("INVALID_ACTION", "未配置 AI 服务（接口地址或 API Key），该功能不可用")
	}
	text, err := callProvider(ctx, cfg, system, user, maxTokens)
	if err != nil {
		return "", coreai.ToAPIError(err, coreai.ErrorContext{TimeoutMs: cfg.TimeoutMs, APIKey: cfg.APIKey})
	}
	return text, nil
}

func askFor(ctx context.Context, cfg coreai.CallConfig, key coreai.PromptKey, visiblePrompt string, maxTokens int) (string, error) {
	m := coreai.BuildMessages(key, visiblePrompt)
	return AskWith(ctx, cfg, m.System, m.User, maxTokens)
}

// TestAIConnection 测试连接：用给定配置发一个很短的请求，返回是否成功和耗时；错误信息不含 Key。
func TestAIConnection(ctx context.Context, cfg coreai.CallConfig) (ok bool, ms int64, errMsg *string) {
	started := time.Now()
	_, err := AskWith(ctx, cfg, "You are a connectivity check. Reply with the single word OK.", "ping", 64)
	ms = time.Since(started).Milliseconds()
	if err != nil {
		m := coreai.DescribeError(err, coreai.ErrorContext{TimeoutMs: cfg.TimeoutMs, APIKey: cfg.APIKey})
		return false, ms, &m
	}
	return true, ms, nil
}

// ---------------------------------------------------------------------------
// 单词加载
// ---------------------------------------------------------------------------

type wordRow struct {
	ID           string
	Spelling     string
	Definition   string
	PartOfSpeech *string
}

// loadWords 按给定顺序取单词（去重、最多 limit 个）；查不到的忽略。
func loadWords(ctx context.Context, q store.Querier, wordIDs []string, limit int) ([]wordRow, error) {
	seen := map[string]bool{}
	ids := make([]string, 0, len(wordIDs))
	for _, id := range wordIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		if len(ids) >= limit {
			break
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT "id","spelling","partOfSpeech","definition" FROM "Word" WHERE "id" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]wordRow{}
	for rows.Next() {
		var w wordRow
		if err := rows.Scan(&w.ID, &w.Spelling, &w.PartOfSpeech, &w.Definition); err != nil {
			return nil, err
		}
		byID[w.ID] = w
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]wordRow, 0, len(ids))
	for _, id := range ids {
		if w, ok := byID[id]; ok {
			out = append(out, w)
		}
	}
	return out, nil
}

func promptWordsOf(rows []wordRow) []coreai.PromptWord {
	out := make([]coreai.PromptWord, len(rows))
	for i, w := range rows {
		pos := ""
		if w.PartOfSpeech != nil {
			pos = *w.PartOfSpeech
		}
		out[i] = coreai.PromptWord{Spelling: w.Spelling, Definition: w.Definition, PartOfSpeech: pos}
	}
	return out
}

// AiPreviewWord 预览里的单词。
type AiPreviewWord struct {
	ID         string `json:"id"`
	Spelling   string `json:"spelling"`
	Definition string `json:"definition"`
}

// AiPreview 预览结果：单词 + 默认的可见提示词。
type AiPreview struct {
	Words  []AiPreviewWord `json:"words"`
	Prompt string          `json:"prompt"`
}

func previewWordsOf(rows []wordRow) []AiPreviewWord {
	out := make([]AiPreviewWord, len(rows))
	for i, w := range rows {
		out[i] = AiPreviewWord{ID: w.ID, Spelling: w.Spelling, Definition: w.Definition}
	}
	return out
}

// AiUnitExamplesPreview 单元补例句预览额外返回：本单元缺例句的词数。
type AiUnitExamplesPreview struct {
	AiPreview
	Remaining int `json:"remaining"`
}

// ---------------------------------------------------------------------------
// 例句
// ---------------------------------------------------------------------------

// ExampleBatchLimit 一次最多生成的例句数。
const ExampleBatchLimit = 20

// ExampleResultItem 单个词的生成结果。
type ExampleResultItem struct {
	WordID     string `json:"wordId"`
	Spelling   string `json:"spelling"`
	Example    string `json:"example"`
	ExampleCn  string `json:"exampleCn"`
	ClozeReady bool   `json:"clozeReady"`
}

// AiExamplesJobResult 例句任务的结果。
type AiExamplesJobResult struct {
	Items     []ExampleResultItem `json:"items"`
	Failed    []string            `json:"failed"`
	Remaining *int                `json:"remaining,omitempty"`
}

// PreviewExamples 例句预览：单词 + 默认可见提示词（要求模板 + 单词列表），不调用 AI。
func PreviewExamples(ctx context.Context, cache *AIConfigCache, q store.Querier, wordIDs []string) (AiPreview, error) {
	rows, err := loadWords(ctx, q, wordIDs, ExampleBatchLimit)
	if err != nil {
		return AiPreview{}, err
	}
	tpl, err := cache.Template(ctx, q, coreai.PromptExample)
	if err != nil {
		return AiPreview{}, err
	}
	return AiPreview{Words: previewWordsOf(rows), Prompt: coreai.BuildExamplePrompt(tpl, promptWordsOf(rows))}, nil
}

// clozeReady 是否能挖空（对应旧 lib/questions.ts buildCloze != null，仅取「是否命中」，不产出挖空文本）。
// 本地小型移植：避免依赖尚未实现的学习模块。
func clozeReady(example, spelling string) bool {
	target := strings.TrimSpace(parenRe2.ReplaceAllString(strings.SplitN(spelling, "=", 2)[0], ""))
	if example == "" || target == "" {
		return false
	}
	escaped := regexp.QuoteMeta(target)
	escaped = wsRunRe.ReplaceAllString(escaped, `\s+`)
	// Compile（非 MustCompile）：escaped 由 QuoteMeta 生成理论上总合法，防御一下不让后台任务 panic（I3）。
	re, err := regexp.Compile(`(?i)(^|[^A-Za-z])(` + escaped + `)(s|es|ed|ing|d|'s)?([^A-Za-z]|$)`)
	if err != nil {
		return false
	}
	return re.MatchString(example)
}

var parenRe2 = regexp.MustCompile(`[（(][^)）]*[)）]`)

// GenerateExamples 为若干单词生成例句并写回 Word（标记来源 ai）。prompt 是页面上的可见提示词
// （用户消息），不传时按默认模板拼；单词以 wordIds 为准。
func GenerateExamples(ctx context.Context, cache *AIConfigCache, q store.Querier, now time.Time, cfg coreai.CallConfig, wordIDs []string, prompt *string) (AiExamplesJobResult, error) {
	rows, err := loadWords(ctx, q, wordIDs, ExampleBatchLimit)
	if err != nil {
		return AiExamplesJobResult{}, err
	}
	if len(rows) == 0 {
		return AiExamplesJobResult{Items: []ExampleResultItem{}, Failed: []string{}}, nil
	}

	visible := ""
	if prompt != nil {
		visible = *prompt
	} else {
		tpl, err := cache.Template(ctx, q, coreai.PromptExample)
		if err != nil {
			return AiExamplesJobResult{}, err
		}
		visible = coreai.BuildExamplePrompt(tpl, promptWordsOf(rows))
	}

	reply, err := askFor(ctx, cfg, coreai.PromptExample, visible, 3000)
	if err != nil {
		return AiExamplesJobResult{}, err
	}
	// 解析到 []any（未做字段类型校验，与旧版 parseAiList 的运行时语义一致，见 I1）：一项字段类型不对
	// 或整项不是对象都不会连累数组里其他合格的项。
	list, err := coreai.ParseAiList(reply)
	if err != nil {
		return AiExamplesJobResult{}, err
	}
	// bySpelling 的 key 用 String(p.spelling ?? "").trim().toLowerCase()（AsString 已处理数字/布尔转字符串）。
	bySpelling := map[string]map[string]any{}
	for _, it := range list {
		m := coreai.AsMap(it)
		key := strings.ToLower(strings.TrimSpace(coreai.AsString(m["spelling"])))
		if key != "" {
			bySpelling[key] = m
		}
	}

	items := []ExampleResultItem{}
	failed := []string{}
	var exampleLex *Lexicon
	for i, w := range rows {
		hit, ok := bySpelling[strings.ToLower(w.Spelling)]
		if !ok && len(list) == len(rows) {
			hit, ok = coreai.AsMap(list[i]), true
		}
		example, exampleCn := "", ""
		if ok {
			example = strings.TrimSpace(coreai.AsString(hit["example"]))
			exampleCn = strings.TrimSpace(coreai.AsString(hit["exampleCn"]))
		}
		if example == "" || exampleCn == "" || !coreai.ExampleUsesWord(example, w.Spelling) {
			failed = append(failed, w.Spelling)
			continue
		}
		ts := store.NewTime(now)
		if _, err := q.ExecContext(ctx, `UPDATE "Word" SET "example"=?,"exampleCn"=?,"exampleSource"='ai',"exampleAt"=?,"updatedAt"=? WHERE "id"=?`, example, exampleCn, ts, ts, w.ID); err != nil {
			return AiExamplesJobResult{}, err
		}
		// 例句双写（spec 0004 §3）
		if exampleLex == nil {
			if exampleLex, err = LoadLexicon(ctx, q); err != nil {
				return AiExamplesJobResult{}, err
			}
		}
		if err := SyncExampleSentence(ctx, q, exampleLex, now, w.ID); err != nil {
			return AiExamplesJobResult{}, err
		}
		items = append(items, ExampleResultItem{WordID: w.ID, Spelling: w.Spelling, Example: example, ExampleCn: exampleCn, ClozeReady: clozeReady(example, w.Spelling)})
	}
	return AiExamplesJobResult{Items: items, Failed: failed}, nil
}

// ---------------------------------------------------------------------------
// 巩固短文
// ---------------------------------------------------------------------------

// PassageWordLimit 一次最多用于生成短文的单词数。
const PassageWordLimit = 15

// PassageQuestion 短文理解题。
type PassageQuestion struct {
	Q string `json:"q"`
	A string `json:"a"`
}

// PassageWordStat 短文里目标词的出现情况。
type PassageWordStat struct {
	WordID     string `json:"wordId"`
	Spelling   string `json:"spelling"`
	Definition string `json:"definition"`
	Appeared   bool   `json:"appeared"`
}

// PassageResult 生成短文的结果（由调用方保存）。
type PassageResult struct {
	Title        string
	TitleCn      string
	Passage      string
	PassageCn    string
	Questions    []PassageQuestion
	Words        []PassageWordStat
	MissingWords []string
}

// PreviewPassage 短文预览：单词 + 默认可见提示词（要求模板 + 目标单词 + 主题），不调用 AI。
func PreviewPassage(ctx context.Context, cache *AIConfigCache, q store.Querier, wordIDs []string, topic string) (AiPreview, error) {
	rows, err := loadWords(ctx, q, wordIDs, PassageWordLimit)
	if err != nil {
		return AiPreview{}, err
	}
	tpl, err := cache.Template(ctx, q, coreai.PromptPassage)
	if err != nil {
		return AiPreview{}, err
	}
	return AiPreview{Words: previewWordsOf(rows), Prompt: coreai.BuildPassagePrompt(tpl, promptWordsOf(rows), topic)}, nil
}

// GeneratePassage 用指定单词生成巩固短文（结果由调用方保存）。prompt 是页面上的可见提示词
// （用户消息），不传时按默认模板拼；「出现了哪些目标词」按 wordIds 统计。
func GeneratePassage(ctx context.Context, cache *AIConfigCache, q store.Querier, cfg coreai.CallConfig, wordIDs []string, prompt *string, topic string) (PassageResult, error) {
	rows, err := loadWords(ctx, q, wordIDs, PassageWordLimit)
	if err != nil {
		return PassageResult{}, err
	}
	if len(rows) == 0 {
		return PassageResult{}, coreai.NewAPIError("VALIDATION", "请先选择单词")
	}

	visible := ""
	if prompt != nil {
		visible = *prompt
	} else {
		tpl, err := cache.Template(ctx, q, coreai.PromptPassage)
		if err != nil {
			return PassageResult{}, err
		}
		visible = coreai.BuildPassagePrompt(tpl, promptWordsOf(rows), topic)
	}

	reply, err := askFor(ctx, cfg, coreai.PromptPassage, visible, 2500)
	if err != nil {
		return PassageResult{}, err
	}
	// 解析到 map（不校验字段类型，见 I1）：title 是数字、questions 不是数组等都按旧版的
	// String(x ?? "") / Array.isArray(...) 语义宽松处理，不整段报错。
	raw, err := coreai.ParseAiObject(reply)
	if err != nil {
		return PassageResult{}, err
	}
	passage := strings.TrimSpace(coreai.AsString(raw["passage"]))
	if passage == "" {
		return PassageResult{}, coreai.NewAPIError("SERVER", "AI 没有生成短文")
	}

	wordStats := make([]PassageWordStat, len(rows))
	missing := []string{}
	for i, w := range rows {
		appeared := coreai.ExampleUsesWord(passage, w.Spelling)
		wordStats[i] = PassageWordStat{WordID: w.ID, Spelling: w.Spelling, Definition: w.Definition, Appeared: appeared}
		if !appeared {
			missing = append(missing, w.Spelling)
		}
	}

	title := "Reading"
	if t := strings.TrimSpace(coreai.AsString(raw["title"])); t != "" {
		title = t
	}
	titleCn := strings.TrimSpace(coreai.AsString(raw["titleCn"]))
	passageCn := strings.TrimSpace(coreai.AsString(raw["passageCn"]))
	questions := []PassageQuestion{}
	if arr, ok := raw["questions"].([]any); ok {
		for i, qv := range arr {
			if i >= 5 {
				break
			}
			qm := coreai.AsMap(qv)
			questions = append(questions, PassageQuestion{Q: coreai.AsString(qm["q"]), A: coreai.AsString(qm["a"])})
		}
	}

	return PassageResult{Title: title, TitleCn: titleCn, Passage: passage, PassageCn: passageCn, Questions: questions, Words: wordStats, MissingWords: missing}, nil
}

// SavePassage 把生成结果存进短文列表（对应旧 routes/ai.ts 里 /passages/generate 的 db.passage.create）。
func SavePassage(ctx context.Context, q store.Querier, now time.Time, userID, model string, r PassageResult) (string, error) {
	id := store.NewID()
	wordIDs := make([]string, len(r.Words))
	for i, w := range r.Words {
		wordIDs[i] = w.WordID
	}
	questionsJSON := store.NewJSON(r.Questions)
	wordIDsJSON := store.NewJSON(wordIDs)
	_, err := q.ExecContext(ctx, `INSERT INTO "Passage" ("id","userId","title","titleCn","body","bodyCn","questions","wordIds","source","model","createdAt") VALUES (?,?,?,?,?,?,?,?,'ai',?,?)`,
		id, userID, r.Title, r.TitleCn, r.Passage, r.PassageCn, questionsJSON, wordIDsJSON, model, store.NewTime(now))
	if err != nil {
		return "", err
	}
	// 逐句结构（spec 0004 §4）：中英句数一致时拆分；0005 让 AI 直接按句输出后改为直接保存
	if err := SavePassageSentences(ctx, q, now, id); err != nil {
		return "", err
	}
	return id, nil
}

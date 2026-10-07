package service

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 历史补算（spec 0009 §5）：按新规则本该更新、当时没更新的记忆，按组的完成时间顺序补上。
//
//   - 待补证据 = core.BuildEvidence 得到的证据里，ReviewLog 中没有对应（同一个词、同一学习日）记录的；
//   - 只补「全部待补证据都晚于这个词最后一次记忆更新」的词（接着往下算，与当时就按新规则结算一致），
//     交错的词跳过并列出；没有记忆的词从头重放（答错不建，第一次答对建卡）；
//   - 评分、建卡 / 更新的判断与线上结算用同一套函数（core.DeriveRating、core.MemoryActionFor）；
//   - 补写的 ReviewLog 带 source = BackfillSource，reviewedAt / dayKey / sessionId 用原来那一组的；不改 Answer 与学习组。

// BackfillSource 补算写入的 ReviewLog.source。
const BackfillSource = "backfill-0009"

// BackfillMemorySnap 补算前后的记忆（没有记忆为 nil）。
type BackfillMemorySnap struct {
	Stability float64   `json:"stability"`
	Due       time.Time `json:"due"`
	Reps      int       `json:"reps"`
	Lapses    int       `json:"lapses"`
}

// BackfillEvent 一条补上的证据。
type BackfillEvent struct {
	SessionID string    `json:"sessionId"`
	Kind      string    `json:"kind"`
	Day       string    `json:"day"`
	At        time.Time `json:"at"`
	Rating    string    `json:"rating"`
	Action    string    `json:"action"` // create | update | none（答错的生词不建卡）
}

// BackfillWord 一个词的补算。
type BackfillWord struct {
	UserID   string              `json:"userId"`
	WordID   string              `json:"wordId"`
	Spelling string              `json:"spelling"`
	Before   *BackfillMemorySnap `json:"before"`
	After    *BackfillMemorySnap `json:"after"`
	Events   []BackfillEvent     `json:"events"`
	// Skipped 跳过的原因（不为空时 After 等于 Before，不写库）。
	Skipped string `json:"skipped,omitempty"`
}

// BackfillUserSummary 一个学生的汇总。
type BackfillUserSummary struct {
	UserID  string `json:"userId"`
	Name    string `json:"name"`
	Words   int    `json:"words"`   // 有待补证据的词
	Events  int    `json:"events"`  // 写入的复习记录条数
	Created int    `json:"created"` // 新建记忆的词
	Updated int    `json:"updated"` // 更新记忆的词
	NoMem   int    `json:"noMem"`   // 只有答错、不建记忆的生词
	Skipped int    `json:"skipped"` // 交错等原因跳过的词
}

// BackfillReport 补算报告。
type BackfillReport struct {
	Applied bool                  `json:"applied"`
	Users   []BackfillUserSummary `json:"users"`
	Words   []BackfillWord        `json:"words"`
}

var errBackfillDryRun = errors.New("dry run")

// BackfillMemory 演练（apply = false，事务回滚、不写库）或执行补算。调用方须先停服务、先备份。
func BackfillMemory(ctx context.Context, db *store.DB, loc *time.Location, apply bool) (*BackfillReport, error) {
	report := &BackfillReport{Applied: apply, Users: []BackfillUserSummary{}, Words: []BackfillWord{}}
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT "id","name" FROM "User" ORDER BY "createdAt", "id"`)
		if err != nil {
			return err
		}
		type user struct{ id, name string }
		users := []user{}
		for rows.Next() {
			var u user
			if err := rows.Scan(&u.id, &u.name); err != nil {
				rows.Close()
				return err
			}
			users = append(users, u)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, u := range users {
			sum, words, err := backfillUser(ctx, tx, loc, u.id, apply)
			if err != nil {
				return err
			}
			if sum.Words == 0 {
				continue
			}
			sum.Name = u.name
			report.Users = append(report.Users, *sum)
			report.Words = append(report.Words, words...)
		}
		if !apply {
			return errBackfillDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errBackfillDryRun) {
		return nil, err
	}
	return report, nil
}

func backfillSnap(m *core.MemoryCard) *BackfillMemorySnap {
	if m == nil {
		return nil
	}
	return &BackfillMemorySnap{Stability: core.Round1(m.Stability), Due: m.Due, Reps: m.Reps, Lapses: m.Lapses}
}

func backfillUser(ctx context.Context, tx store.Querier, loc *time.Location, userID string, apply bool) (*BackfillUserSummary, []BackfillWord, error) {
	sum := &BackfillUserSummary{UserID: userID}
	src, err := loadEvidenceSources(ctx, tx, []string{userID}, "")
	if err != nil {
		return nil, nil, err
	}
	evidence := core.BuildEvidence(src[userID].sessions, src[userID].answers)
	if len(evidence) == 0 {
		return sum, nil, nil
	}

	// 已有复习记录的（词、学习日）
	logged := map[string]bool{}
	hasLog := map[string]bool{}
	lrows, err := tx.QueryContext(ctx, `SELECT "wordId","dayKey" FROM "ReviewLog" WHERE "userId" = ?`, userID)
	if err != nil {
		return nil, nil, err
	}
	for lrows.Next() {
		var w, d string
		if err := lrows.Scan(&w, &d); err != nil {
			lrows.Close()
			return nil, nil, err
		}
		logged[w+"\x00"+d] = true
		hasLog[w] = true
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return nil, nil, err
	}

	// 组的计划与每个词要求的题型（评分用）
	type sessInfo struct {
		planID *string
		dayKey string
		modes  map[string][]string
	}
	sessions := map[string]*sessInfo{}
	srows, err := tx.QueryContext(ctx, `SELECT "id","planId","dayKey","snapshot" FROM "StudySession" WHERE "userId" = ? AND "status" = 'completed'`, userID)
	if err != nil {
		return nil, nil, err
	}
	for srows.Next() {
		var id, dayKey, text string
		var planID *string
		if err := srows.Scan(&id, &planID, &dayKey, &text); err != nil {
			srows.Close()
			return nil, nil, err
		}
		snap, err := ParseSnapshot(text)
		if err != nil {
			srows.Close()
			return nil, nil, err
		}
		info := &sessInfo{planID: planID, dayKey: dayKey, modes: map[string][]string{}}
		for i := range snap.Items {
			info.modes[snap.Items[i].WordID] = snap.ItemModes(&snap.Items[i])
		}
		sessions[id] = info
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, nil, err
	}

	memories, err := loadAllMemories(ctx, tx, userID)
	if err != nil {
		return nil, nil, err
	}

	wordIDs := make([]string, 0, len(evidence))
	for w := range evidence {
		wordIDs = append(wordIDs, w)
	}
	sort.Strings(wordIDs)
	out := []BackfillWord{}
	for _, w := range wordIDs {
		row := memories[w]
		pending := []core.Evidence{}
		for _, e := range evidence[w] {
			if logged[w+"\x00"+e.Day] {
				continue
			}
			// 记忆建立之前答错的：那时没有记忆、答错也不建卡，新规则下同样什么都不做
			if row != nil && e.At.Before(row.introducedAt) && !e.Correct {
				continue
			}
			pending = append(pending, e)
		}
		if len(pending) == 0 {
			continue
		}
		sum.Words++
		var card *core.MemoryCard
		if row != nil {
			c := row.MemoryCard
			card = &c
		}
		bw := BackfillWord{UserID: userID, WordID: w, Before: backfillSnap(card), Events: []BackfillEvent{}}
		switch {
		case card != nil && card.LastReview != nil && !pending[0].At.After(*card.LastReview):
			bw.Skipped = "待补作答早于最后一次记忆更新（与已有记录交错）"
		case card == nil && hasLog[w]:
			bw.Skipped = "没有记忆却有复习记录"
		}
		if bw.Skipped != "" {
			bw.After = bw.Before
			sum.Skipped++
			out = append(out, bw)
			continue
		}

		type logRow struct {
			e           core.Evidence
			rating      core.Rating
			stateBefore int
			card        core.MemoryCard
		}
		logs := []logRow{}
		created := false
		var createdPlan *string
		createdDay := ""
		var createdAt time.Time
		for _, e := range pending {
			// 每个词每个学习日最多一次（K19）：当天已经动过记忆的跳过
			if card != nil && card.LastReview != nil && core.DayKeyOf(*card.LastReview, loc) == e.Day {
				continue
			}
			info := sessions[e.SessionID]
			if info == nil {
				continue
			}
			rating := core.DeriveRating(e.Kind, e.Attempts, info.modes[w])
			action := core.MemoryActionFor(e.Kind, card != nil, rating)
			ev := BackfillEvent{SessionID: e.SessionID, Kind: e.Kind, Day: e.Day, At: e.At, Action: "none"}
			if rating != 0 {
				ev.Rating = core.RatingName[rating]
			}
			switch action {
			case core.MemoryCreateLearn, core.MemoryCreate:
				c := core.ApplyRating(core.NewMemoryCard(e.At), rating, e.At)
				if action == core.MemoryCreateLearn {
					tomorrowStart, _ := core.DayRange(core.AddDays(e.Day, 1), loc)
					c = core.CapDue(c, tomorrowStart, e.At)
					createdPlan = info.planID
				}
				created, createdDay, createdAt = true, info.dayKey, e.At
				logs = append(logs, logRow{e: e, rating: rating, stateBefore: 0, card: c})
				card = &c
				ev.Action = "create"
			case core.MemoryUpdate:
				before := card.State
				c := core.ApplyRating(*card, rating, e.At)
				logs = append(logs, logRow{e: e, rating: rating, stateBefore: before, card: c})
				card = &c
				ev.Action = "update"
			}
			bw.Events = append(bw.Events, ev)
		}
		bw.After = backfillSnap(card)
		switch {
		case created:
			sum.Created++
		case len(logs) > 0:
			sum.Updated++
		default:
			sum.NoMem++
		}
		sum.Events += len(logs)
		if apply && len(logs) > 0 {
			if created {
				if _, err := createMemory(ctx, tx, createdAt, createdDay, userID, w, createdPlan, *card); err != nil {
					return nil, nil, err
				}
			} else if err := updateMemory(ctx, tx, logs[len(logs)-1].e.At, row.ID, *card); err != nil {
				return nil, nil, err
			}
			for _, l := range logs {
				sid := l.e.SessionID
				if err := insertReviewLog(ctx, tx, l.e.At, l.e.Day, userID, w, sid, l.rating, l.stateBefore, l.card, BackfillSource); err != nil {
					return nil, nil, err
				}
			}
		}
		out = append(out, bw)
	}
	if len(out) > 0 {
		ids := make([]string, len(out))
		for i, b := range out {
			ids[i] = b.WordID
		}
		spell, err := wordSpellings(ctx, tx, ids)
		if err != nil {
			return nil, nil, err
		}
		for i := range out {
			out[i].Spelling = spell[out[i].WordID]
		}
	}
	return sum, out, nil
}

// backfillMemory 补算用的记忆：FSRS 字段 + 建立时间。
type backfillMemory struct {
	memoryRow
	introducedAt time.Time
}

// loadAllMemories 一个学生的全部记忆。
func loadAllMemories(ctx context.Context, q store.Querier, userID string) (map[string]*backfillMemory, error) {
	rows, err := q.QueryContext(ctx, `SELECT "id","wordId","due","stability","difficulty","elapsedDays","scheduledDays","learningSteps","reps","lapses","state","lastReview","introducedAt"
		FROM "MemoryState" WHERE "userId" = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*backfillMemory{}
	for rows.Next() {
		var m backfillMemory
		var wordID string
		var due, intro store.Time
		var last store.NullTime
		if err := rows.Scan(&m.ID, &wordID, &due, &m.Stability, &m.Difficulty, &m.ElapsedDays, &m.ScheduledDays, &m.LearningSteps, &m.Reps, &m.Lapses, &m.State, &last, &intro); err != nil {
			return nil, err
		}
		m.introducedAt = intro.Time
		m.Due = due.Time
		if last.Valid {
			lr := last.Time
			m.LastReview = &lr
		}
		out[wordID] = &m
	}
	return out, rows.Err()
}

// wordSpellings 词的拼写（报告用）。
func wordSpellings(ctx context.Context, q store.Querier, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for i := 0; i < len(ids); i += 500 {
		chunk := ids[i:min(i+500, len(ids))]
		rows, err := q.QueryContext(ctx, `SELECT "id","spelling" FROM "Word" WHERE "id" IN (`+store.Placeholders(len(chunk))+`)`, store.Args(chunk)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, sp string
			if err := rows.Scan(&id, &sp); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = sp
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

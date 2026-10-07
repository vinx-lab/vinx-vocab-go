package service

import (
	"context"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 作答证据（spec 0009）：已完成的学习组 + 首次作答，交给 core.BuildEvidence 筛选。
// 覆盖进度、学习记录、结算与历史补算都从这里取，口径一致。

// evidenceSource 一个用户的组与首次作答（BuildEvidence 的输入）。
type evidenceSource struct {
	sessions []core.EvidenceSession
	answers  []core.EvidenceAnswer
}

// loadEvidenceSources 一批用户已完成的学习组与它们的首次作答（练习 / 检测阶段的 attempt = 1）。
// sinceDay 非空时只取完成学习日不早于它的组（结算时只看当天）。
func loadEvidenceSources(ctx context.Context, q store.Querier, userIDs []string, sinceDay string) (map[string]*evidenceSource, error) {
	out := map[string]*evidenceSource{}
	for _, id := range userIDs {
		out[id] = &evidenceSource{}
	}
	if len(userIDs) == 0 {
		return out, nil
	}
	ph, args := store.Placeholders(len(userIDs)), store.Args(userIDs)
	cond := ""
	if sinceDay != "" {
		cond = ` AND coalesce(s."completedDay", s."dayKey") >= ?`
		args = append(args, sinceDay)
	}
	// 默写单的批改组另取快照（判定自批，spec 0006）
	srows, err := q.QueryContext(ctx, `SELECT s."id", s."userId", s."kind", s."sheetId", s."completedAt", coalesce(s."completedDay", s."dayKey"),
			CASE WHEN w."format" = 'dictation' THEN s."snapshot" END
		FROM "StudySession" s LEFT JOIN "WordSheet" w ON w."id" = s."sheetId"
		WHERE s."userId" IN (`+ph+`) AND s."status" = 'completed'`+cond+` ORDER BY s."completedAt", s.rowid`, args...)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var s core.EvidenceSession
		var uid string
		var at store.NullTime
		var dictSnapshot *string
		if err := srows.Scan(&s.ID, &uid, &s.Kind, &s.SheetID, &at, &s.Day, &dictSnapshot); err != nil {
			srows.Close()
			return nil, err
		}
		if at.Valid {
			s.At = at.Time
		}
		if dictSnapshot != nil {
			sg, err := snapshotSelfGraded(*dictSnapshot)
			if err != nil {
				srows.Close()
				return nil, err
			}
			s.SelfGraded = sg != nil && *sg
		}
		out[uid].sessions = append(out[uid].sessions, s)
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, err
	}

	arows, err := q.QueryContext(ctx, `SELECT a."userId", a."sessionId", a."wordId", a."mode", a."phase", a."attempt", a."correct", a."hintUsed"
		FROM "Answer" a JOIN "StudySession" s ON s."id" = a."sessionId"
		WHERE a."userId" IN (`+ph+`) AND s."status" = 'completed' AND a."phase" IN ('practice','test') AND a."attempt" = 1`+cond+`
		ORDER BY a."createdAt", a.rowid`, args...)
	if err != nil {
		return nil, err
	}
	defer arows.Close()
	for arows.Next() {
		var a core.EvidenceAnswer
		var uid string
		if err := arows.Scan(&uid, &a.SessionID, &a.WordID, &a.Mode, &a.Phase, &a.Attempt, &a.Correct, &a.HintUsed); err != nil {
			return nil, err
		}
		out[uid].answers = append(out[uid].answers, a)
	}
	return out, arows.Err()
}

// loadEvidence 一批用户每个词的证据（userId → wordId → 按时间升序）。
func loadEvidence(ctx context.Context, q store.Querier, userIDs []string) (map[string]map[string][]core.Evidence, error) {
	src, err := loadEvidenceSources(ctx, q, userIDs, "")
	if err != nil {
		return nil, err
	}
	out := map[string]map[string][]core.Evidence{}
	for uid, s := range src {
		out[uid] = core.BuildEvidence(s.sessions, s.answers)
	}
	return out, nil
}

// loadStageMemories 一批用户的记忆（判定词状态用）。
func loadStageMemories(ctx context.Context, q store.Querier, userIDs []string) (map[string]map[string]*core.StageMemory, error) {
	out := map[string]map[string]*core.StageMemory{}
	for _, id := range userIDs {
		out[id] = map[string]*core.StageMemory{}
	}
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT "userId","wordId","stability","due","lastReview" FROM "MemoryState" WHERE "userId" IN (`+store.Placeholders(len(userIDs))+`)`, store.Args(userIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid, wid string
		var m core.StageMemory
		var due store.Time
		var last store.NullTime
		if err := rows.Scan(&uid, &wid, &m.Stability, &due, &last); err != nil {
			return nil, err
		}
		m.Due = due.Time
		if last.Valid {
			t := last.Time
			m.LastReview = &t
		}
		out[uid][wid] = &m
	}
	return out, rows.Err()
}

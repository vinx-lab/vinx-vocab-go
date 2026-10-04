package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core/vocabparser"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 系统词书拼写修复（spec 0007 §3）：内置考纲词表里带注释的拼写（`fly (flew, flown)`、`bike = bicycle`、
// `gladness //ˈɡlædnəs//` 等）在老库里是独立的 Word。按 vocabparser.CleanSpelling 规整后：
// 库里已有同拼写（大小写完全一致）的词就合并过去，没有就直接改名。新安装由解析器规整，这里只修已有的库。

func init() {
	// 每次打开数据库检查一次；修完之后范围内没有能被规整的拼写，后续打开只做一次只读查询。
	store.RegisterOpenHook(RepairWordSpellings)
}

// WordRepairStats 一次修复的统计。*Dropped 为合并时因唯一约束冲突删掉的行数（保留另一条）。
type WordRepairStats struct {
	Renamed int
	Merged  int

	UnitWordDropped     int
	MemoryStateDropped  int
	AnswerDropped       int
	ReviewLogDropped    int
	SentenceWordDropped int
}

// dirtyWord 范围内拼写能被规整的词。
type dirtyWord struct {
	id, spelling string
	phonetic     *string
	definition   string
}

// systemWordsToClean 出现在系统词书（isSystem = 1）单元里、拼写能被 CleanSpelling 规整的词（按 rowid 顺序）。
func systemWordsToClean(ctx context.Context, q store.Querier) ([]dirtyWord, error) {
	rows, err := q.QueryContext(ctx, `SELECT w."id", w."spelling", w."phonetic", w."definition" FROM "Word" w
		WHERE EXISTS (SELECT 1 FROM "UnitWord" uw JOIN "Unit" u ON u."id" = uw."unitId" JOIN "Book" b ON b."id" = u."bookId"
			WHERE uw."wordId" = w."id" AND b."isSystem" = 1)
		ORDER BY w.rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dirtyWord
	for rows.Next() {
		var w dirtyWord
		if err := rows.Scan(&w.id, &w.spelling, &w.phonetic, &w.definition); err != nil {
			return nil, err
		}
		if sp, _, _ := vocabparser.CleanSpelling(w.spelling); sp != w.spelling {
			out = append(out, w)
		}
	}
	return out, rows.Err()
}

// RepairWordSpellings 打开数据库时修复系统词书里带注释的拼写（幂等；没有要修的词时不开写事务）。
func RepairWordSpellings(ctx context.Context, d *store.DB, now time.Time) error {
	todo, err := systemWordsToClean(ctx, d)
	if err != nil || len(todo) == 0 {
		return err
	}
	var st WordRepairStats
	if err := d.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		st, err = RepairSystemWordSpellings(ctx, tx, now)
		return err
	}); err != nil {
		return err
	}
	slog.Info(fmt.Sprintf("系统词书拼写修复：改名 %d、合并 %d（合并时冲突去掉：单元词 %d、记忆状态 %d、作答 %d、复习记录 %d、句中词 %d）",
		st.Renamed, st.Merged, st.UnitWordDropped, st.MemoryStateDropped, st.AnswerDropped, st.ReviewLogDropped, st.SentenceWordDropped))
	return nil
}

// RepairSystemWordSpellings 在事务里逐个处理范围内能被规整的词：已有规整后拼写的词（大小写完全一致）就合并过去，
// 否则改名（音标为空时补上拆出的音标，注释以「（…）」接在释义末尾）。逐个处理并每次重新查目标词，
// 两个脏拼写规整成同一个词时，第一个改名，第二个合并到它上面。
func RepairSystemWordSpellings(ctx context.Context, tx *sql.Tx, now time.Time) (WordRepairStats, error) {
	var st WordRepairStats
	todo, err := systemWordsToClean(ctx, tx)
	if err != nil {
		return st, err
	}
	for _, w := range todo {
		spelling, phonetic, note := vocabparser.CleanSpelling(w.spelling)
		var keepID string
		err := tx.QueryRowContext(ctx, `SELECT "id" FROM "Word" WHERE "spelling" = ?`, spelling).Scan(&keepID)
		switch {
		case store.IsNoRows(err):
			if err := renameWord(ctx, tx, now, w, spelling, phonetic, note); err != nil {
				return st, err
			}
			st.Renamed++
		case err != nil:
			return st, err
		default:
			if err := mergeWord(ctx, tx, w.id, keepID, &st); err != nil {
				return st, fmt.Errorf("合并「%s」到「%s」失败：%w", w.spelling, spelling, err)
			}
			st.Merged++
		}
	}
	return st, nil
}

// renameWord 改名：类型按规整后的拼写重算；音标为空时用拆出的音标；发音缓存文件名随拼写变，清掉。
func renameWord(ctx context.Context, tx *sql.Tx, now time.Time, w dirtyWord, spelling, phonetic, note string) error {
	typ := "word"
	if strings.ContainsAny(spelling, " \t") {
		typ = "phrase"
	}
	ph := w.phonetic
	if (ph == nil || strings.TrimSpace(*ph) == "") && phonetic != "" {
		ph = &phonetic
	}
	_, err := tx.ExecContext(ctx, `UPDATE "Word" SET "spelling" = ?, "type" = ?, "phonetic" = ?, "definition" = ?, "audioFile" = NULL, "updatedAt" = ? WHERE "id" = ?`,
		spelling, typ, ph, vocabparser.AppendNote(w.definition, note), store.NewTime(now), w.id)
	return err
}

// mergeWord 把 oldID 的所有引用改指向 keepID，最后删除 oldID。Word 上的外键都是 ON DELETE CASCADE，
// 必须先改完引用再删。保留词的拼写、音标、释义、例句都不变。
func mergeWord(ctx context.Context, tx *sql.Tx, oldID, keepID string, st *WordRepairStats) error {
	exec := func(query string, args ...any) (int, error) {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		return int(n), err
	}

	// UnitWord：同一单元两个词都在时只留一条，sortOrder 取两者较小的
	if _, err := exec(`UPDATE "UnitWord" SET "sortOrder" = (SELECT o."sortOrder" FROM "UnitWord" o WHERE o."unitId" = "UnitWord"."unitId" AND o."wordId" = ?)
		WHERE "wordId" = ? AND "sortOrder" > (SELECT o."sortOrder" FROM "UnitWord" o WHERE o."unitId" = "UnitWord"."unitId" AND o."wordId" = ?)`,
		oldID, keepID, oldID); err != nil {
		return err
	}
	n, err := exec(`DELETE FROM "UnitWord" WHERE "wordId" = ? AND "unitId" IN (SELECT "unitId" FROM "UnitWord" WHERE "wordId" = ?)`, oldID, keepID)
	if err != nil {
		return err
	}
	st.UnitWordDropped += n
	if _, err := exec(`UPDATE "UnitWord" SET "wordId" = ? WHERE "wordId" = ?`, keepID, oldID); err != nil {
		return err
	}

	// MemoryState：同一学生两条都有时保留 lastReview 较晚的（NULL 视为最早），相同时保留 reps 较多的，再相同保留保留词的那条
	n, err = dropConflicts(ctx, tx, `SELECT o."id", k."id",
			CASE WHEN coalesce(o."lastReview", '') > coalesce(k."lastReview", '') THEN 1
				WHEN coalesce(o."lastReview", '') < coalesce(k."lastReview", '') THEN 0
				WHEN o."reps" > k."reps" THEN 1 ELSE 0 END
		FROM "MemoryState" o JOIN "MemoryState" k ON k."userId" = o."userId" AND k."wordId" = ?
		WHERE o."wordId" = ?`, "MemoryState", keepID, oldID)
	if err != nil {
		return err
	}
	st.MemoryStateDropped += n
	if _, err := exec(`UPDATE "MemoryState" SET "wordId" = ? WHERE "wordId" = ?`, keepID, oldID); err != nil {
		return err
	}

	// Answer：唯一键 (sessionId, wordId, mode, phase, attempt) 冲突时保留先写入的（createdAt，再 rowid）
	n, err = dropConflicts(ctx, tx, `SELECT o."id", k."id",
			CASE WHEN o."createdAt" < k."createdAt" OR (o."createdAt" = k."createdAt" AND o.rowid < k.rowid) THEN 1 ELSE 0 END
		FROM "Answer" o JOIN "Answer" k ON k."sessionId" = o."sessionId" AND k."wordId" = ?
			AND k."mode" = o."mode" AND k."phase" = o."phase" AND k."attempt" = o."attempt"
		WHERE o."wordId" = ?`, "Answer", keepID, oldID)
	if err != nil {
		return err
	}
	st.AnswerDropped += n
	if _, err := exec(`UPDATE "Answer" SET "wordId" = ? WHERE "wordId" = ?`, keepID, oldID); err != nil {
		return err
	}

	// ReviewLog：唯一键 (sessionId, wordId)，sessionId 为 NULL 时不冲突；冲突时保留先写入的（reviewedAt，再 rowid）
	n, err = dropConflicts(ctx, tx, `SELECT o."id", k."id",
			CASE WHEN o."reviewedAt" < k."reviewedAt" OR (o."reviewedAt" = k."reviewedAt" AND o.rowid < k.rowid) THEN 1 ELSE 0 END
		FROM "ReviewLog" o JOIN "ReviewLog" k ON k."sessionId" = o."sessionId" AND k."wordId" = ?
		WHERE o."wordId" = ? AND o."sessionId" IS NOT NULL`, "ReviewLog", keepID, oldID)
	if err != nil {
		return err
	}
	st.ReviewLogDropped += n
	if _, err := exec(`UPDATE "ReviewLog" SET "wordId" = ? WHERE "wordId" = ?`, keepID, oldID); err != nil {
		return err
	}

	// Sentence.wordId（例句、AI 例句）：无唯一约束
	if _, err := exec(`UPDATE "Sentence" SET "wordId" = ? WHERE "wordId" = ?`, keepID, oldID); err != nil {
		return err
	}
	// SentenceWord：主键 (sentenceId, wordId, position) 冲突时删掉旧的那条
	n, err = exec(`DELETE FROM "SentenceWord" WHERE "wordId" = ? AND EXISTS (SELECT 1 FROM "SentenceWord" k
		WHERE k."sentenceId" = "SentenceWord"."sentenceId" AND k."position" = "SentenceWord"."position" AND k."wordId" = ?)`, oldID, keepID)
	if err != nil {
		return err
	}
	st.SentenceWordDropped += n
	if _, err := exec(`UPDATE "SentenceWord" SET "wordId" = ? WHERE "wordId" = ?`, keepID, oldID); err != nil {
		return err
	}

	// JSON 数组 wordIds：替换并去重，保持顺序
	for _, table := range []string{"WordSheet", "Passage"} {
		if err := replaceInIDArrays(ctx, tx, table, oldID, keepID); err != nil {
			return err
		}
	}
	// 其余存了词 id 的 JSON 列按文本替换（cuid 全局唯一，替换安全；连同引号一起匹配，只换完整的 JSON 字符串）。
	// WordSheet.items 是默写单的题目列表，批改按下标对应，不去重。
	oldQ, keepQ := `"`+oldID+`"`, `"`+keepID+`"`
	for _, tc := range [][2]string{{"WordSheet", "items"}, {"StudySession", "snapshot"}, {"StudySession", "progress"}, {"StudySession", "result"}} {
		if _, err := exec(fmt.Sprintf(`UPDATE "%s" SET "%s" = replace("%s", ?, ?) WHERE instr("%s", ?) > 0`, tc[0], tc[1], tc[1], tc[1]),
			oldQ, keepQ, oldQ); err != nil {
			return err
		}
	}

	// 引用都改完了，最后删旧词（级联不会再删到任何东西）
	_, err = exec(`DELETE FROM "Word" WHERE "id" = ?`, oldID)
	return err
}

// dropConflicts 处理唯一约束冲突：query 返回 (旧词那条 id, 保留词那条 id, 是否保留旧词那条)。
// 保留旧词那条时删掉保留词那条（随后旧词那条改指向保留词），否则删掉旧词那条。返回删掉的行数。
func dropConflicts(ctx context.Context, tx *sql.Tx, query, table string, args ...any) (int, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	var drop []string
	for rows.Next() {
		var oldRow, keepRow string
		var keepOld bool
		if err := rows.Scan(&oldRow, &keepRow, &keepOld); err != nil {
			rows.Close()
			return 0, err
		}
		if keepOld {
			drop = append(drop, keepRow)
		} else {
			drop = append(drop, oldRow)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, id := range drop {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM "%s" WHERE "id" = ?`, table), id); err != nil {
			return 0, err
		}
	}
	return len(drop), nil
}

// replaceInIDArrays 把 table.wordIds（JSON 字符串数组）里的 oldID 换成 keepID，去重并保持顺序。
func replaceInIDArrays(ctx context.Context, tx *sql.Tx, table, oldID, keepID string) error {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT "id", "wordIds" FROM "%s" WHERE instr("wordIds", ?) > 0`, table), oldID)
	if err != nil {
		return err
	}
	type row struct{ id, ids string }
	var todo []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.ids); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, r := range todo {
		var ids []string
		if err := json.Unmarshal([]byte(r.ids), &ids); err != nil {
			return fmt.Errorf("%s %s 的 wordIds 不是字符串数组：%w", table, r.id, err)
		}
		out := make([]string, 0, len(ids))
		seen := make(map[string]bool, len(ids))
		for _, id := range ids {
			if id == oldID {
				id = keepID
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE "%s" SET "wordIds" = ? WHERE "id" = ?`, table), string(raw), r.id); err != nil {
			return err
		}
	}
	return nil
}

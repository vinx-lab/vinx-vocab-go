// Package seed 内置词书与演示数据（对应旧 prisma/seed.ts）。
//
//   - Books：首次启动（库里还没有任何词书）时导入内置词表为系统词书；
//   - Demo：seed-demo 子命令额外创建演示账号、演示班级 DEMO01 与演示计划，与旧 seed 相同（可重跑）。
package seed

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	vinxvocab "github.com/vinx-lab/vinx-vocab-go"
	"github.com/vinx-lab/vinx-vocab-go/internal/auth"
	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/vocabparser"
	"github.com/vinx-lab/vinx-vocab-go/internal/school"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// DemoPassword 演示账号密码。
const DemoPassword = "dev123456"

// Account 演示账号。
type Account struct{ Email, Name, Role string }

// Accounts 演示账号（顺序与旧 seed 一致）。
var Accounts = []Account{
	{"admin@vinx.test", "管理员", "admin"},
	{"teacher@vinx.test", "王老师", "teacher"},
	{"student@vinx.test", "小明", "student"},
	{"student2@vinx.test", "小红", "student"},
}

// BookSpec 内置词书。
type BookSpec struct {
	File, Name, Description string
	ByInitial               bool
}

// Books 内置词书（sortOrder 即下标）。
var Books = []BookSpec{
	{File: "七上", Name: "七年级上册", Description: "初中英语七年级上册教材词汇"},
	{File: "七下", Name: "七年级下册", Description: "初中英语七年级下册教材词汇"},
	{File: "八上", Name: "八年级上册", Description: "初中英语八年级上册教材词汇"},
	{File: "八下", Name: "八年级下册", Description: "初中英语八年级下册教材词汇"},
	{File: "九上", Name: "九年级上册", Description: "初中英语九年级上册教材词汇"},
	{File: "中考词汇_两源合并版", Name: "中考核心词汇", Description: "中考考纲词汇，按首字母分单元", ByInitial: true},
}

// VocabFS 内置词表来源（测试可替换）。
var VocabFS fs.FS = vinxvocab.VocabFS

// ParseBook 读取并解析一本内置词书：单元内去重，考纲词表按首字母分单元。
func ParseBook(b BookSpec) ([]service.ImportUnit, error) {
	raw, err := fs.ReadFile(VocabFS, "data/vocab/"+b.File+".txt")
	if err != nil {
		return nil, err
	}
	units := vocabparser.ParseVocabText(string(raw), "")
	for i := range units {
		units[i] = vocabparser.DedupeUnitEntries(units[i])
	}
	if b.ByInitial {
		units = vocabparser.SplitByInitial(units)
	}
	out := make([]service.ImportUnit, 0, len(units))
	for _, u := range units {
		out = append(out, service.ImportUnit{Name: u.Name, Entries: service.EntriesFromParsed(u.Entries)})
	}
	return out, nil
}

// HasAnyBook 库里是否已有词书（首次启动判断）。
func HasAnyBook(ctx context.Context, q store.Querier) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM "Book"`).Scan(&n)
	return n > 0, err
}

// FirstRunBooks 首次启动：库里没有任何词书时导入内置词书（无所有者，对应旧 SEED_BOOKS_ONLY=1）。
func FirstRunBooks(ctx context.Context, db *store.DB, now time.Time) error {
	has, err := HasAnyBook(ctx, db)
	if err != nil || has {
		return err
	}
	_, err = SeedBooks(ctx, db, now, nil)
	return err
}

// SeedBooks 导入缺少的系统词书（按名称 + isSystem 判断是否已存在），返回 文件名 → 词书 id。
func SeedBooks(ctx context.Context, db *store.DB, now time.Time, ownerID *string) (map[string]string, error) {
	ids := map[string]string{}
	for i, b := range Books {
		var existing string
		err := db.QueryRowContext(ctx, `SELECT "id" FROM "Book" WHERE "name" = ? AND "isSystem" = 1 ORDER BY "createdAt" LIMIT 1`, b.Name).Scan(&existing)
		if err == nil {
			ids[b.File] = existing
			continue
		}
		if !store.IsNoRows(err) {
			return nil, err
		}
		units, err := ParseBook(b)
		if err != nil {
			slog.Warn("跳过内置词书", "name", b.Name, "err", err)
			continue
		}
		bookID := store.NewID()
		var res service.ImportResult
		err = db.Tx(ctx, func(tx *sql.Tx) error {
			ts := store.NewTime(now)
			// 学段按书名写入（spec 0005 §1）
			if _, err := tx.ExecContext(ctx, `INSERT INTO "Book" ("id","name","description","isSystem","ownerId","sortOrder","level","createdAt","updatedAt") VALUES (?,?,?,?,?,?,?,?,?)`,
				bookID, b.Name, b.Description, true, ownerID, i, coreai.LevelForBookName(b.Name), ts, ts); err != nil {
				return err
			}
			var err error
			res, err = service.ImportUnits(ctx, tx, now, bookID, units)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("导入词书「%s」失败：%w", b.Name, err)
		}
		ids[b.File] = bookID
		slog.Info(fmt.Sprintf("词书「%s」：%d 单元，关联 %d 词（新建 %d，复用 %d）", b.Name, res.UnitsCreated, res.WordsLinked, res.WordsCreated, res.WordsReused))
	}
	return ids, nil
}

// Demo 演示数据：账号（已存在则只更新姓名）→ 系统词书（所有者为演示管理员）→ 演示班级与计划。可重跑。
func Demo(ctx context.Context, db *store.DB, now time.Time) error {
	users, err := seedAccounts(ctx, db, now)
	if err != nil {
		return err
	}
	admin := users["admin@vinx.test"]
	bookIDs, err := SeedBooks(ctx, db, now, &admin)
	if err != nil {
		return err
	}
	// 先 serve（首次启动导入的系统词书无所有者）再 seed-demo 时，与旧 seed 一样把内置系统词书归到演示管理员名下
	for _, b := range Books {
		if id := bookIDs[b.File]; id != "" {
			if _, err := db.ExecContext(ctx, `UPDATE "Book" SET "ownerId" = ? WHERE "id" = ? AND "ownerId" IS NULL`, admin, id); err != nil {
				return err
			}
		}
	}
	return seedDemoClass(ctx, db, now, users, bookIDs)
}

func seedAccounts(ctx context.Context, db *store.DB, now time.Time) (map[string]string, error) {
	hash, err := auth.HashPasswordCost(DemoPassword, 10)
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	for _, a := range Accounts {
		u, err := service.FindUserByEmail(ctx, db, a.Email)
		if err != nil {
			return nil, err
		}
		if u != nil {
			if _, err := db.ExecContext(ctx, `UPDATE "User" SET "name" = ?, "updatedAt" = ? WHERE "id" = ?`, a.Name, store.NewTime(now), u.ID); err != nil {
				return nil, err
			}
		} else if u, err = service.CreateUser(ctx, db, now, service.NewUser{Email: a.Email, Name: a.Name, Role: a.Role, PasswordHash: hash}); err != nil {
			return nil, err
		}
		ids[a.Email] = u.ID
	}
	slog.Info(fmt.Sprintf("演示账号 %d 个（密码 %s）", len(Accounts), DemoPassword))
	return ids, nil
}

// DemoClassName / DemoInviteCode / DemoPlanName 演示班级与计划。
const (
	DemoClassName  = "八年级一班"
	DemoInviteCode = "DEMO01"
	DemoPlanName   = "八上 Unit 1–2 每日背词"
)

func seedDemoClass(ctx context.Context, db *store.DB, now time.Time, users, bookIDs map[string]string) error {
	teacherID := users["teacher@vinx.test"]
	ts := store.NewTime(now)
	return db.Tx(ctx, func(tx *sql.Tx) error {
		var classID string
		err := tx.QueryRowContext(ctx, `SELECT "id" FROM "Classroom" WHERE "inviteCode" = ?`, DemoInviteCode).Scan(&classID)
		if store.IsNoRows(err) {
			classID = store.NewID()
			if _, err := tx.ExecContext(ctx, `INSERT INTO "Classroom" ("id","name","teacherId","inviteCode","createdAt","updatedAt") VALUES (?,?,?,?,?,?)`,
				classID, DemoClassName, teacherID, DemoInviteCode, ts, ts); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		for _, email := range []string{"student@vinx.test", "student2@vinx.test"} {
			if err := school.AddMember(ctx, tx, now, classID, users[email]); err != nil {
				return err
			}
		}

		bookID := bookIDs["八上"]
		if bookID == "" {
			return nil
		}
		var exists int
		tx.QueryRowContext(ctx, `SELECT count(*) FROM "Plan" WHERE "name" = ? AND "creatorId" = ?`, DemoPlanName, teacherID).Scan(&exists)
		if exists > 0 {
			return nil
		}
		rows, err := tx.QueryContext(ctx, `SELECT "id" FROM "Unit" WHERE "bookId" = ? AND "name" IN ('Unit 1','Unit 2') ORDER BY "sortOrder"`, bookID)
		if err != nil {
			return err
		}
		var unitIDs []string
		for rows.Next() {
			var id string
			rows.Scan(&id)
			unitIDs = append(unitIDs, id)
		}
		rows.Close()
		planID := store.NewID()
		if _, err := tx.ExecContext(ctx, `INSERT INTO "Plan" ("id","name","creatorId","kind","newPerDay","reviewPerDay","modes","createdAt","updatedAt") VALUES (?,?,?,?,?,?,?,?,?)`,
			planID, DemoPlanName, teacherID, "daily", 10, 50, store.NewJSON([]string{"recognition", "spelling"}), ts, ts); err != nil {
			return err
		}
		for i, u := range unitIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO "PlanUnit" ("planId","unitId","sortOrder") VALUES (?,?,?)`, planID, u, i); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO "PlanTarget" ("id","planId","classId") VALUES (?,?,?)`, store.NewID(), planID, classID); err != nil {
			return err
		}
		slog.Info(fmt.Sprintf("演示班级「%s」（邀请码 %s）与计划「%s」", DemoClassName, DemoInviteCode, DemoPlanName))
		return nil
	})
}

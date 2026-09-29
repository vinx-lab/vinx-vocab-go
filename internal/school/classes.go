package school

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 班级、成员、邀请码、批量建号（对应旧 school/classes.routes.ts 与 classes.service.ts）。
// 权限（能力守卫、AssertCanManageClass）由路由负责；这里是查询与写入。

// InviteCodeAlphabet 邀请码字符集（去掉易混的 I、O、0、1）。
const InviteCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// RandomInviteCode 随机邀请码（默认 6 位）。
func RandomInviteCode(n int) string {
	if n <= 0 {
		n = 6
	}
	b := make([]byte, n)
	max := big.NewInt(int64(len(InviteCodeAlphabet)))
	for i := range b {
		k, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		b[i] = InviteCodeAlphabet[k.Int64()]
	}
	return string(b)
}

// UniqueInviteCode 生成库里未用过的邀请码；10 次都撞号 → SERVER「邀请码生成失败，请重试」。
func UniqueInviteCode(ctx context.Context, q store.Querier) (string, error) {
	for i := 0; i < 10; i++ {
		code := RandomInviteCode(6)
		var id string
		err := q.QueryRowContext(ctx, `SELECT "id" FROM "Classroom" WHERE "inviteCode" = ?`, code).Scan(&id)
		if store.IsNoRows(err) {
			return code, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", httpx.NewError(httpx.CodeServer, "邀请码生成失败，请重试", nil)
}

// ClassRow Classroom 表的一行（旧版直接返回 Prisma 行：建班、改名 / 归档、换邀请码）。
type ClassRow struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	TeacherID  string     `json:"teacherId"`
	InviteCode string     `json:"inviteCode"`
	Archived   bool       `json:"archived"`
	CreatedAt  store.Time `json:"createdAt"`
	UpdatedAt  store.Time `json:"updatedAt"`
}

// GetClassRow 按 id 取班级；不存在返回 (nil, nil)。
func GetClassRow(ctx context.Context, q store.Querier, id string) (*ClassRow, error) {
	var c ClassRow
	err := q.QueryRowContext(ctx, `SELECT "id","name","teacherId","inviteCode","archived","createdAt","updatedAt" FROM "Classroom" WHERE "id" = ?`, id).
		Scan(&c.ID, &c.Name, &c.TeacherID, &c.InviteCode, &c.Archived, &c.CreatedAt, &c.UpdatedAt)
	if store.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ClassListItem GET /classes 的一项。
type ClassListItem struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	InviteCode  string     `json:"inviteCode"`
	Archived    bool       `json:"archived"`
	TeacherID   string     `json:"teacherId"`
	TeacherName string     `json:"teacherName"`
	MemberCount int        `json:"memberCount"`
	PlanCount   int        `json:"planCount"`
	CreatedAt   store.Time `json:"createdAt"`
}

// ListClasses 管理员看全部，老师看自己的；未归档在前、新建在前。
func ListClasses(ctx context.Context, q store.Querier, a *service.Actor) ([]ClassListItem, error) {
	where, args := "", []any{}
	if !service.IsAdmin(a) {
		where, args = `WHERE c."teacherId" = ?`, []any{a.ID}
	}
	rows, err := q.QueryContext(ctx, `SELECT c."id",c."name",c."inviteCode",c."archived",c."teacherId",u."name",
		(SELECT count(*) FROM "ClassMember" m WHERE m."classId" = c."id"),
		(SELECT count(*) FROM "PlanTarget" t WHERE t."classId" = c."id"),
		c."createdAt"
		FROM "Classroom" c JOIN "User" u ON u."id" = c."teacherId" `+where+`
		ORDER BY c."archived" ASC, c."createdAt" DESC, c.rowid DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ClassListItem{}
	for rows.Next() {
		var c ClassListItem
		if err := rows.Scan(&c.ID, &c.Name, &c.InviteCode, &c.Archived, &c.TeacherID, &c.TeacherName, &c.MemberCount, &c.PlanCount, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateClass 建班（班主任为当前操作者）。
func CreateClass(ctx context.Context, db *store.DB, now time.Time, teacherID, name string) (*ClassRow, error) {
	var out *ClassRow
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		code, err := UniqueInviteCode(ctx, tx)
		if err != nil {
			return err
		}
		id := store.NewID()
		ts := store.NewTime(now)
		if _, err := tx.ExecContext(ctx, `INSERT INTO "Classroom" ("id","name","teacherId","inviteCode","archived","createdAt","updatedAt") VALUES (?,?,?,?,0,?,?)`,
			id, name, teacherID, code, ts, ts); err != nil {
			return err
		}
		out, err = GetClassRow(ctx, tx, id)
		return err
	})
	return out, err
}

// ClassPatch 部分更新；nil 表示不改。
type ClassPatch struct {
	Name     *string
	Archived *bool
}

// UpdateClass 改名 / 归档（updatedAt 总会刷新，与 Prisma 一致）。
func UpdateClass(ctx context.Context, q store.Querier, now time.Time, id string, p ClassPatch) (*ClassRow, error) {
	sets := `"updatedAt" = ?`
	args := []any{store.NewTime(now)}
	if p.Name != nil {
		sets += `, "name" = ?`
		args = append(args, *p.Name)
	}
	if p.Archived != nil {
		sets += `, "archived" = ?`
		args = append(args, *p.Archived)
	}
	args = append(args, id)
	if _, err := q.ExecContext(ctx, `UPDATE "Classroom" SET `+sets+` WHERE "id" = ?`, args...); err != nil {
		return nil, err
	}
	return GetClassRow(ctx, q, id)
}

// RotateInviteCode 换邀请码（旧码立即失效）。
func RotateInviteCode(ctx context.Context, db *store.DB, now time.Time, id string) (*ClassRow, error) {
	var out *ClassRow
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		code, err := UniqueInviteCode(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE "Classroom" SET "inviteCode" = ?, "updatedAt" = ? WHERE "id" = ?`, code, store.NewTime(now), id); err != nil {
			return err
		}
		out, err = GetClassRow(ctx, tx, id)
		return err
	})
	return out, err
}

// DeleteClass 删除班级（成员关系、班级计划安排级联删除）。
func DeleteClass(ctx context.Context, q store.Querier, id string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM "Classroom" WHERE "id" = ?`, id)
	return err
}

// ClassTeacher 详情里的班主任。
type ClassTeacher struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ClassMemberView 详情里的成员。
type ClassMemberView struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Email    string     `json:"email"`
	JoinedAt store.Time `json:"joinedAt"`
	// ManagedByMe 当前用户可代管该账号（重置密码）：管理员，或该账号由当前老师批量创建（K21）。
	ManagedByMe bool `json:"managedByMe"`
}

// ClassPlanView 详情里安排给本班的计划。
type ClassPlanView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	NewPerDay int    `json:"newPerDay"`
}

// ClassDetail GET /classes/:id。
type ClassDetail struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	InviteCode string            `json:"inviteCode"`
	Archived   bool              `json:"archived"`
	Teacher    ClassTeacher      `json:"teacher"`
	Members    []ClassMemberView `json:"members"`
	Plans      []ClassPlanView   `json:"plans"`
}

// GetClassDetail 班级详情：成员按入班时间先后，计划按安排先后。班级不存在 → NOT_FOUND。
func GetClassDetail(ctx context.Context, q store.Querier, a *service.Actor, id string) (*ClassDetail, error) {
	var d ClassDetail
	err := q.QueryRowContext(ctx, `SELECT c."id",c."name",c."inviteCode",c."archived",u."id",u."name"
		FROM "Classroom" c JOIN "User" u ON u."id" = c."teacherId" WHERE c."id" = ?`, id).
		Scan(&d.ID, &d.Name, &d.InviteCode, &d.Archived, &d.Teacher.ID, &d.Teacher.Name)
	if store.IsNoRows(err) {
		return nil, httpx.NotFound("班级不存在")
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT u."id",u."name",u."email",m."joinedAt",u."createdById"
		FROM "ClassMember" m JOIN "User" u ON u."id" = m."userId" WHERE m."classId" = ? ORDER BY m."joinedAt" ASC, m.rowid ASC`, id)
	if err != nil {
		return nil, err
	}
	d.Members = []ClassMemberView{}
	for rows.Next() {
		var m ClassMemberView
		var createdBy *string
		if err := rows.Scan(&m.ID, &m.Name, &m.Email, &m.JoinedAt, &createdBy); err != nil {
			rows.Close()
			return nil, err
		}
		m.ManagedByMe = service.IsAdmin(a) || (createdBy != nil && *createdBy == a.ID)
		d.Members = append(d.Members, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	prow, err := q.QueryContext(ctx, `SELECT p."id",p."name",p."kind",p."status",p."newPerDay"
		FROM "PlanTarget" t JOIN "Plan" p ON p."id" = t."planId" WHERE t."classId" = ? ORDER BY t.rowid ASC`, id)
	if err != nil {
		return nil, err
	}
	defer prow.Close()
	d.Plans = []ClassPlanView{}
	for prow.Next() {
		var p ClassPlanView
		if err := prow.Scan(&p.ID, &p.Name, &p.Kind, &p.Status, &p.NewPerDay); err != nil {
			return nil, err
		}
		d.Plans = append(d.Plans, p)
	}
	return &d, prow.Err()
}

// MemberBrief 按账号添加成员的返回。
type MemberBrief struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// AddMemberByAccount 按账号把已有学生加进班级：老师只能加自己批量创建的账号，管理员不限；只能加学生账号（K21）。
func AddMemberByAccount(ctx context.Context, db *store.DB, now time.Time, a *service.Actor, classID, account string) (*MemberBrief, error) {
	u, err := service.FindUserByEmail(ctx, db, account)
	if err != nil {
		return nil, err
	}
	if u == nil || (!service.IsAdmin(a) && (u.CreatedByID == nil || *u.CreatedByID != a.ID)) {
		return nil, httpx.NotFound("找不到你创建的这个学生账号；其他学生请让对方用邀请码加入")
	}
	if u.Role != core.RoleStudent {
		return nil, httpx.Validation("只能添加学生账号")
	}
	if err := AddMember(ctx, db, now, classID, u.ID); err != nil {
		return nil, err
	}
	return &MemberBrief{ID: u.ID, Name: u.Name, Email: u.Email}, nil
}

// BatchAccount 批量建号的一条结果。
type BatchAccount struct {
	Name    string `json:"name"`
	Account string `json:"account"`
}

// BatchCreateMembers 批量建学生账号并入班：账号 = 前缀 + 两位序号（跳过已占用），同一事务，要么全建要么一个不建。
func BatchCreateMembers(ctx context.Context, db *store.DB, now time.Time, creatorID, classID, prefix, passwordHash string, names []string) ([]BatchAccount, error) {
	out := []BatchAccount{}
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT "email" FROM "User" WHERE substr("email", 1, ?) = ?`, len(prefix), prefix)
		if err != nil {
			return err
		}
		taken := map[string]bool{}
		for rows.Next() {
			var e string
			if err := rows.Scan(&e); err != nil {
				rows.Close()
				return err
			}
			taken[e] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		seq := 0
		creator := creatorID
		for _, name := range names {
			var account string
			for {
				seq++
				account = fmt.Sprintf("%s%02d", prefix, seq)
				if !taken[account] {
					break
				}
			}
			taken[account] = true
			u, err := service.CreateUser(ctx, tx, now, service.NewUser{Email: account, Name: name, Role: core.RoleStudent, PasswordHash: passwordHash, CreatedByID: &creator})
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO "ClassMember" ("classId","userId","joinedAt") VALUES (?,?,?)`, classID, u.ID, store.NewTime(now)); err != nil {
				return err
			}
			out = append(out, BatchAccount{Name: name, Account: account})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RemoveMember 移出班级（不在班里也算成功）。
func RemoveMember(ctx context.Context, q store.Querier, classID, userID string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM "ClassMember" WHERE "classId" = ? AND "userId" = ?`, classID, userID)
	return err
}

// CheckMemberPasswordReset 班级内重置密码的对象检查：必须在班里；老师只能重置自己创建的账号；只能重置学生账号（K21）。
func CheckMemberPasswordReset(ctx context.Context, q store.Querier, a *service.Actor, classID, userID string) error {
	var createdBy *string
	var role string
	err := q.QueryRowContext(ctx, `SELECT u."createdById", u."role" FROM "ClassMember" m JOIN "User" u ON u."id" = m."userId" WHERE m."classId" = ? AND m."userId" = ?`, classID, userID).
		Scan(&createdBy, &role)
	if store.IsNoRows(err) {
		return httpx.NotFound("该学生不在班级中")
	}
	if err != nil {
		return err
	}
	if !service.IsAdmin(a) && (createdBy == nil || *createdBy != a.ID) {
		return httpx.Forbidden("只能重置你批量创建的学生账号的密码，自行注册的学生请联系管理员")
	}
	if role != core.RoleStudent {
		return httpx.Forbidden("不能重置教师或管理员账号的密码")
	}
	return nil
}

// JoinByCode 学生凭邀请码入班（已在班里不变）。不能加入自己创建的班级。
func JoinByCode(ctx context.Context, q store.Querier, now time.Time, userID, code string) (*ClassRef, error) {
	cls, err := FindClassByInviteCode(ctx, q, code)
	if err != nil {
		return nil, err
	}
	if cls.TeacherID == userID {
		return nil, httpx.Validation("不能加入自己创建的班级")
	}
	if err := AddMember(ctx, q, now, cls.ID, userID); err != nil {
		return nil, err
	}
	return cls, nil
}

// MyClassItem GET /me/classes 的一项。
type MyClassItem struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TeacherName string     `json:"teacherName"`
	Archived    bool       `json:"archived"`
	JoinedAt    store.Time `json:"joinedAt"`
}

// MyClasses 我加入的班级，最近加入的在前。
func MyClasses(ctx context.Context, q store.Querier, userID string) ([]MyClassItem, error) {
	rows, err := q.QueryContext(ctx, `SELECT c."id",c."name",u."name",c."archived",m."joinedAt"
		FROM "ClassMember" m JOIN "Classroom" c ON c."id" = m."classId" JOIN "User" u ON u."id" = c."teacherId"
		WHERE m."userId" = ? ORDER BY m."joinedAt" DESC, m.rowid DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MyClassItem{}
	for rows.Next() {
		var c MyClassItem
		if err := rows.Scan(&c.ID, &c.Name, &c.TeacherName, &c.Archived, &c.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

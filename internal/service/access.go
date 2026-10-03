// Package service 装配与事务（对应旧 apps/api/src/services）：组合 store 查询与 core 规则。
//
// 本文件：当前操作者与数据范围判定（照旧 services/access.ts）。
// 能力（能不能做这类事）来自 core 的角色能力表；数据范围（本人 / 本班 / 全部）集中在这里：
//   - 学生：仅本人
//   - 老师：自己创建的班级及其成员、自己创建的计划、自己的词书
//   - 管理员：全部
package service

import (
	"context"
	"fmt"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// Actor 当前操作者（来自 JWT：sub / role / name，与旧版一致，角色变更需重新登录才生效）。
type Actor struct {
	ID   string
	Role core.Role
	Name string
	// Edition 本次请求时的版本（路由在身份解析后按请求填入；空串 = 未知，按班级版处理）。
	// 个人版下依赖版本功能的能力关闭（core.EditionAllows），数据范围只限本人（SeesAll 为假）。
	Edition core.Edition
}

// Can 角色是否具备某能力，且当前版本允许该能力。
func Can(a *Actor, c core.Capability) bool {
	return a != nil && core.RoleCan(a.Role, c) && (a.Edition == "" || core.EditionAllows(a.Edition, c))
}

// IsAdmin 是否管理员（角色本身；管理权限如系统词书、系统设置用它判断）。
func IsAdmin(a *Actor) bool { return a != nil && a.Role == core.RoleAdmin }

// IsPersonal 本次请求是否在个人版下。
func IsPersonal(a *Actor) bool { return a != nil && a.Edition == core.EditionPersonal }

// SeesAll 数据范围不受限（班级版的管理员）。个人版下即使是管理员也只看自己的数据：
// 从班级版降级后库里可能仍有多个账号，每个账号只看自己的（spec 0001 §2「版本」）。
// 单账号的个人版里「全部」就是「自己的」，与旧版个人版表现一致。
func SeesAll(a *Actor) bool { return IsAdmin(a) && !IsPersonal(a) }

// AssertCan 缺少能力时返回 FORBIDDEN（message 为空用「无权访问」）。
func AssertCan(a *Actor, c core.Capability, message string) error {
	if !Can(a, c) {
		if message == "" {
			message = "无权访问"
		}
		return httpx.Forbidden(message)
	}
	return nil
}

// ManageableClassIDs 老师可管理的班级 id；管理员返回 nil 表示不限（与旧版 null 对应）。
// 个人版下没有可管理的班级（空切片）。
func ManageableClassIDs(ctx context.Context, q store.Querier, a *Actor) ([]string, error) {
	if SeesAll(a) {
		return nil, nil
	}
	if IsPersonal(a) {
		return []string{}, nil
	}
	return queryStrings(ctx, q, `SELECT "id" FROM "Classroom" WHERE "teacherId" = ?`, a.ID)
}

// AssertCanManageClass 班级不存在 → NOT_FOUND「班级不存在」；非管理员且不是班主任 → FORBIDDEN。
func AssertCanManageClass(ctx context.Context, q store.Querier, a *Actor, classID string) error {
	var teacherID string
	err := q.QueryRowContext(ctx, `SELECT "teacherId" FROM "Classroom" WHERE "id" = ?`, classID).Scan(&teacherID)
	if store.IsNoRows(err) {
		return httpx.NotFound("班级不存在")
	}
	if err != nil {
		return err
	}
	if IsAdmin(a) {
		return nil
	}
	if teacherID != a.ID {
		return httpx.Forbidden("只能管理自己的班级")
	}
	return nil
}

// IsStudentOfTeacher 学生是否在该老师的某个班级里。
func IsStudentOfTeacher(ctx context.Context, q store.Querier, teacherID, userID string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM "ClassMember" m JOIN "Classroom" c ON c."id" = m."classId" WHERE m."userId" = ? AND c."teacherId" = ?`, userID, teacherID).Scan(&n)
	return n > 0, err
}

// AssertCanViewUser 查看某用户学习记录：本人 / 管理员 / 有 students.view 且是其班主任。
// 个人版：只能看自己（students.view 关闭 → 403「无权访问」）。
func AssertCanViewUser(ctx context.Context, q store.Querier, a *Actor, userID string) error {
	if userID == a.ID || SeesAll(a) {
		return nil
	}
	if err := AssertCan(a, core.CapStudentsView, ""); err != nil {
		return err
	}
	ok, err := IsStudentOfTeacher(ctx, q, a.ID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Forbidden("只能查看本班学生")
	}
	return nil
}

// VisibleBookFilter 可见词书条件（系统词书 + 自己的 + 所在班级老师的；班级版管理员全部）。
// 返回可直接拼进 WHERE 的 SQL 片段与参数；alias 为 Book 表在查询里的别名（如 "b"）。
// 个人版（含管理员）：系统词书 + 自己的 + 所在班级老师的（降级前被安排的计划仍要能学）。
func VisibleBookFilter(ctx context.Context, q store.Querier, a *Actor, alias string) (string, []any, error) {
	if SeesAll(a) {
		return "1=1", nil, nil
	}
	teacherIDs, err := queryStrings(ctx, q, `SELECT DISTINCT c."teacherId" FROM "ClassMember" m JOIN "Classroom" c ON c."id" = m."classId" WHERE m."userId" = ?`, a.ID)
	if err != nil {
		return "", nil, err
	}
	owners := append([]string{a.ID}, teacherIDs...)
	sql := fmt.Sprintf(`(%s."isSystem" = 1 OR %s."ownerId" IN (%s))`, alias, alias, store.Placeholders(len(owners)))
	return sql, store.Args(owners), nil
}

// AssertBooksVisible 这些词书都存在且操作者可见（目标词书的可选范围与浏览词书相同，spec 0003）；
// 否则 400「有词书不存在或不可见」。ids 需已去重。
func AssertBooksVisible(ctx context.Context, q store.Querier, a *Actor, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	filter, args, err := VisibleBookFilter(ctx, q, a, "b")
	if err != nil {
		return err
	}
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM "Book" b WHERE b."id" IN (`+store.Placeholders(len(ids))+`) AND `+filter,
		append(store.Args(ids), args...)...).Scan(&n); err != nil {
		return err
	}
	if n != len(ids) {
		return httpx.Validation("有词书不存在或不可见")
	}
	return nil
}

// TargetUsesClasses 确定目标词书时是否看班级成员关系：班级版看（有班级用班级目标）；
// 个人版不看（班级功能关闭、无法退班，只用自己设的目标）。
func TargetUsesClasses(a *Actor) bool { return !IsPersonal(a) }

// VisibleUnit 单元所在词书对当前操作者可见时返回单元；不存在或不可见返回 (nil, nil)
// （单元内容的查看跟随词书可见性，spec 0004 §10）。
func VisibleUnit(ctx context.Context, q store.Querier, a *Actor, unitID string) (*UnitRow, error) {
	where, args, err := VisibleBookFilter(ctx, q, a, "bk")
	if err != nil {
		return nil, err
	}
	u, err := scanUnit(q.QueryRowContext(ctx, `SELECT u."id", u."bookId", u."name", u."sortOrder", u."createdAt"
		FROM "Unit" u JOIN "Book" bk ON bk."id" = u."bookId" WHERE u."id" = ? AND `+where, append([]any{unitID}, args...)...))
	if store.IsNoRows(err) {
		return nil, nil
	}
	return u, err
}

// EditableBook 可编辑词书的归属信息。
type EditableBook struct {
	OwnerID  *string
	IsSystem bool
}

// AssertCanEditBook 可编辑词书：管理员全部（个人版下管理员为系统词书 + 自己的）；否则仅自己拥有的非系统词书。
func AssertCanEditBook(ctx context.Context, q store.Querier, a *Actor, bookID string) (*EditableBook, error) {
	if err := AssertCan(a, core.CapBooksEdit, ""); err != nil {
		return nil, err
	}
	var b EditableBook
	err := q.QueryRowContext(ctx, `SELECT "ownerId", "isSystem" FROM "Book" WHERE "id" = ?`, bookID).Scan(&b.OwnerID, &b.IsSystem)
	if store.IsNoRows(err) {
		return nil, httpx.NotFound("词书不存在")
	}
	if err != nil {
		return nil, err
	}
	own := b.OwnerID != nil && *b.OwnerID == a.ID
	if SeesAll(a) || (IsAdmin(a) && (b.IsSystem || own)) {
		return &b, nil
	}
	if b.IsSystem || !own {
		return nil, httpx.Forbidden("只能编辑自己创建的词书")
	}
	return &b, nil
}

// MemberClassIDs 某用户作为成员所在的班级 id（旧 school/classes.service.ts:memberClassIds；
// A6 实现完整班级服务前，学习计划等模块的最小依赖）。
func MemberClassIDs(ctx context.Context, q store.Querier, userID string) ([]string, error) {
	return queryStrings(ctx, q, `SELECT "classId" FROM "ClassMember" WHERE "userId" = ?`, userID)
}

// IsLearnerOnly 目标账号是否只是学习者（老师只能代管学生账号）。
func IsLearnerOnly(ctx context.Context, q store.Querier, userID string) (bool, error) {
	var role string
	err := q.QueryRowContext(ctx, `SELECT "role" FROM "User" WHERE "id" = ?`, userID).Scan(&role)
	if store.IsNoRows(err) {
		return false, nil
	}
	return role == core.RoleStudent, err
}

func queryStrings(ctx context.Context, q store.Querier, query string, args ...any) ([]string, error) {
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

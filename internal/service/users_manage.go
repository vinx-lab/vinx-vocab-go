package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// 用户管理（班级版，旧 routes/users.ts 的查询部分）。

// ManagedUser GET /users 的一项。
type ManagedUser struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	Role         string     `json:"role"`
	CurrentGrade *string    `json:"currentGrade"`
	CreatedAt    store.Time `json:"createdAt"`
	CreatedByID  *string    `json:"createdById"`
	// ManagedByMe 可代管（重置密码）：管理员，或该账号由当前老师创建。
	ManagedByMe bool `json:"managedByMe"`
}

// ListManagedUsers 用户列表：管理员看全部；老师只看自己创建的账号与自己班级的成员。
// search 非空时按姓名或账号做不区分大小写的子串匹配。新建的在前。
func ListManagedUsers(ctx context.Context, q store.Querier, a *Actor, search string, page, limit int) (httpx.Paginated[ManagedUser], error) {
	where := "1=1"
	args := []any{}
	if !IsAdmin(a) {
		where = `(u."createdById" = ? OR EXISTS (SELECT 1 FROM "ClassMember" m JOIN "Classroom" c ON c."id" = m."classId" WHERE m."userId" = u."id" AND c."teacherId" = ?))`
		args = append(args, a.ID, a.ID)
	}
	if search != "" {
		where += ` AND (VINX_ICONTAINS(u."name", ?) = 1 OR VINX_ICONTAINS(u."email", ?) = 1)`
		args = append(args, search, search)
	}
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM "User" u WHERE `+where, args...).Scan(&total); err != nil {
		return httpx.Paginated[ManagedUser]{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT u."id",u."email",u."name",u."role",u."currentGrade",u."createdAt",u."createdById"
		FROM "User" u WHERE `+where+` ORDER BY u."createdAt" DESC, u.rowid DESC LIMIT ? OFFSET ?`,
		append(args, limit, (page-1)*limit)...)
	if err != nil {
		return httpx.Paginated[ManagedUser]{}, err
	}
	defer rows.Close()
	items := []ManagedUser{}
	for rows.Next() {
		var u ManagedUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CurrentGrade, &u.CreatedAt, &u.CreatedByID); err != nil {
			return httpx.Paginated[ManagedUser]{}, err
		}
		u.ManagedByMe = IsAdmin(a) || (u.CreatedByID != nil && *u.CreatedByID == a.ID)
		items = append(items, u)
	}
	if err := rows.Err(); err != nil {
		return httpx.Paginated[ManagedUser]{}, err
	}
	return httpx.Page(items, total, page, limit), nil
}

// ChangeUserRole 改角色：用户不存在 → 404；把自己降级且只剩自己一个管理员 → 400「系统至少保留一个管理员」。
// 在一个写事务里判断与写入，避免两个管理员同时互相降级后无人可管。
func ChangeUserRole(ctx context.Context, db *store.DB, now time.Time, a *Actor, userID, role string) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		var current string
		err := tx.QueryRowContext(ctx, `SELECT "role" FROM "User" WHERE "id" = ?`, userID).Scan(&current)
		if store.IsNoRows(err) {
			return httpx.NotFound("用户不存在")
		}
		if err != nil {
			return err
		}
		if userID == a.ID && role != core.RoleAdmin {
			var admins int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "User" WHERE "role" = 'admin'`).Scan(&admins); err != nil {
				return err
			}
			if admins <= 1 {
				return httpx.Validation("系统至少保留一个管理员")
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE "User" SET "role" = ?, "updatedAt" = ? WHERE "id" = ?`, role, store.NewTime(now), userID)
		return err
	})
}

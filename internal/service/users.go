package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// UserRow User 表的一行（含密码哈希，不直接返回给前端）。
type UserRow struct {
	ID           string
	Email        string
	PasswordHash string
	Name         string
	Role         string
	CurrentGrade *string
	Theme        string
	CreatedByID  *string
	CreatedAt    store.Time
	UpdatedAt    store.Time
}

const userColumns = `"id","email","passwordHash","name","role","currentGrade","theme","createdById","createdAt","updatedAt"`

func scanUser(row interface{ Scan(...any) error }) (*UserRow, error) {
	var u UserRow
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.CurrentGrade, &u.Theme, &u.CreatedByID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindUserByID 不存在时返回 (nil, nil)。
func FindUserByID(ctx context.Context, q store.Querier, id string) (*UserRow, error) {
	u, err := scanUser(q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM "User" WHERE "id" = ?`, id))
	if store.IsNoRows(err) {
		return nil, nil
	}
	return u, err
}

// FindUserByEmail 按账号（邮箱或学号，已规整为小写）查找；不存在时返回 (nil, nil)。
func FindUserByEmail(ctx context.Context, q store.Querier, email string) (*UserRow, error) {
	u, err := scanUser(q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM "User" WHERE "email" = ?`, email))
	if store.IsNoRows(err) {
		return nil, nil
	}
	return u, err
}

// NewUser 创建账号所需字段。
type NewUser struct {
	Email        string
	Name         string
	Role         string
	PasswordHash string
	CreatedByID  *string
}

// CreateUser 插入账号并返回完整行。
func CreateUser(ctx context.Context, q store.Querier, now time.Time, n NewUser) (*UserRow, error) {
	id := store.NewID()
	ts := store.NewTime(now)
	if n.Role == "" {
		n.Role = core.RoleStudent
	}
	_, err := q.ExecContext(ctx, `INSERT INTO "User" ("id","email","passwordHash","name","role","theme","createdById","createdAt","updatedAt") VALUES (?,?,?,?,?,?,?,?,?)`,
		id, n.Email, n.PasswordHash, n.Name, n.Role, "system", n.CreatedByID, ts, ts)
	if err != nil {
		return nil, err
	}
	return FindUserByID(ctx, q, id)
}

// CountUsers 账号总数。
func CountUsers(ctx context.Context, q store.Querier) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM "User"`).Scan(&n)
	return n, err
}

// UpdatePassword 更新密码哈希。
func UpdatePassword(ctx context.Context, q store.Querier, now time.Time, userID, hash string) error {
	_, err := q.ExecContext(ctx, `UPDATE "User" SET "passwordHash" = ?, "updatedAt" = ? WHERE "id" = ?`, hash, store.NewTime(now), userID)
	return err
}

// ProfilePatch 个人资料的部分更新：nil 表示不改；CurrentGrade 为 Set 且 Null 时清空。
type ProfilePatch struct {
	Name         *string
	CurrentGrade *sql.NullString
	Theme        *string
}

// UpdateProfile 只更新给出的字段（updatedAt 总会刷新，与 Prisma 空 data 的 update 一致）。
func UpdateProfile(ctx context.Context, q store.Querier, now time.Time, userID string, p ProfilePatch) error {
	sets := []string{`"updatedAt" = ?`}
	args := []any{store.NewTime(now)}
	if p.Name != nil {
		sets = append(sets, `"name" = ?`)
		args = append(args, *p.Name)
	}
	if p.CurrentGrade != nil {
		sets = append(sets, `"currentGrade" = ?`)
		args = append(args, *p.CurrentGrade)
	}
	if p.Theme != nil {
		sets = append(sets, `"theme" = ?`)
		args = append(args, *p.Theme)
	}
	args = append(args, userID)
	_, err := q.ExecContext(ctx, `UPDATE "User" SET `+strings.Join(sets, ", ")+` WHERE "id" = ?`, args...)
	return err
}

// UserView 返回给前端的账号（旧 shared User）：剔除敏感字段，附当前角色的能力清单。
type UserView struct {
	ID           string            `json:"id"`
	Email        string            `json:"email"`
	Name         string            `json:"name"`
	Role         string            `json:"role"`
	CurrentGrade *string           `json:"currentGrade"`
	Theme        string            `json:"theme"`
	CreatedAt    store.Time        `json:"createdAt"`
	Capabilities []core.Capability `json:"capabilities"`
}

// ToUserView UserRow → UserView（旧 toUserWithAuth）。
func ToUserView(u *UserRow) UserView {
	return UserView{
		ID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, CurrentGrade: u.CurrentGrade,
		Theme: core.NormalizeThemePref(u.Theme), CreatedAt: u.CreatedAt, Capabilities: core.CapabilitiesOf(u.Role),
	}
}

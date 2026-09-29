// Package school 班级版专属逻辑（对应旧 apps/api/src/school）。个人版下相关路由按请求 404（见 api.Router.Feature）。
package school

import (
	"context"
	"strings"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// ClassRef 班级的最小信息。
type ClassRef struct {
	ID        string
	Name      string
	TeacherID string
	Archived  bool
}

// FindClassByInviteCode 邀请码（去空白、转大写）找班级；不存在或已归档 → NOT_FOUND。
func FindClassByInviteCode(ctx context.Context, q store.Querier, code string) (*ClassRef, error) {
	var c ClassRef
	err := q.QueryRowContext(ctx, `SELECT "id","name","teacherId","archived" FROM "Classroom" WHERE "inviteCode" = ?`,
		strings.ToUpper(httpx.JSTrim(code))).Scan(&c.ID, &c.Name, &c.TeacherID, &c.Archived)
	if store.IsNoRows(err) || (err == nil && c.Archived) {
		return nil, httpx.NotFound("邀请码无效或班级已归档")
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// AddMember 入班（已在班里则不变，对应 upsert）。
func AddMember(ctx context.Context, q store.Querier, now time.Time, classID, userID string) error {
	_, err := q.ExecContext(ctx, `INSERT INTO "ClassMember" ("classId","userId","joinedAt") VALUES (?,?,?) ON CONFLICT DO NOTHING`, classID, userID, store.NewTime(now))
	return err
}

package service

import (
	"context"

	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// DevUser 开发模式账号列表的一项（GET /dev/users，spec 0002）。
type DevUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	// ClassNames 所在班级名：学生是加入的班级，老师是自己带的班级；没有时为空数组。按班级名排序。
	ClassNames []string `json:"classNames"`
}

// ListDevUsers 列出全部账号（只读）：管理员 → 老师 → 学生（未知角色排最后），同角色按邮箱排序。
// 只在 --dev 下由 /dev/users 调用，不做数据范围限制。
func ListDevUsers(ctx context.Context, q store.Querier) ([]DevUser, error) {
	rows, err := q.QueryContext(ctx, `SELECT "id","email","name","role" FROM "User"
		ORDER BY CASE "role" WHEN 'admin' THEN 0 WHEN 'teacher' THEN 1 WHEN 'student' THEN 2 ELSE 3 END, "email"`)
	if err != nil {
		return nil, err
	}
	out := []DevUser{}
	index := map[string]int{}
	for rows.Next() {
		u := DevUser{ClassNames: []string{}}
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			rows.Close()
			return nil, err
		}
		index[u.ID] = len(out)
		out = append(out, u)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	crows, err := q.QueryContext(ctx, `SELECT "userId", "name" FROM (
			SELECT m."userId" AS "userId", c."name" AS "name" FROM "ClassMember" m JOIN "Classroom" c ON c."id" = m."classId"
			UNION
			SELECT c."teacherId", c."name" FROM "Classroom" c
		) ORDER BY "name"`)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var userID, name string
		if err := crows.Scan(&userID, &name); err != nil {
			return nil, err
		}
		if i, ok := index[userID]; ok {
			out[i].ClassNames = append(out[i].ClassNames, name)
		}
	}
	return out, crows.Err()
}

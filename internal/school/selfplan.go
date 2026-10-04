package school

import (
	"context"

	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// SelfPlanImpact GET /classes/:id/self-plan-impact：把班级从「允许自主」改为「不允许」会影响到什么（spec 0008 §2）。
// 只统计当前仍能自主的成员（已被另一个不允许的班限制的，改这个班对他没有变化）：
//   - selfPlans：他们自建的、只安排给自己的、没归档的计划（改完后算出来的暂停）；
//   - ownBooks：他们自己追加的目标词书（保留但不再生效）；
//   - students：以上两项至少有一项的人数。
type SelfPlanImpact struct {
	Students  int `json:"students"`
	SelfPlans int `json:"selfPlans"`
	OwnBooks  int `json:"ownBooks"`
}

// ClassSelfPlanImpact 统计影响；班级是否存在由路由先判定。
func ClassSelfPlanImpact(ctx context.Context, q store.Querier, classID string) (*SelfPlanImpact, error) {
	out := &SelfPlanImpact{}
	var allow bool
	if err := q.QueryRowContext(ctx, `SELECT "allowSelfPlan" FROM "Classroom" WHERE "id" = ?`, classID).Scan(&allow); err != nil {
		return nil, err
	}
	if !allow {
		return out, nil // 已经不允许，改了也没有变化
	}
	members, err := classMemberIDs(ctx, q, classID)
	if err != nil || len(members) == 0 {
		return out, err
	}
	allowed, err := service.SelfPlanAllowedBatch(ctx, q, true, members)
	if err != nil {
		return nil, err
	}
	affected := []string{}
	for _, id := range members {
		if allowed[id] {
			affected = append(affected, id)
		}
	}
	if len(affected) == 0 {
		return out, nil
	}
	ph, args := store.Placeholders(len(affected)), store.Args(affected)
	touched := map[string]bool{}
	rows, err := q.QueryContext(ctx, `SELECT p."creatorId", count(*) FROM "Plan" p
		WHERE p."creatorId" IN (`+ph+`) AND p."status" <> 'archived'
		AND (SELECT count(*) FROM "PlanTarget" t WHERE t."planId" = p."id") = 1
		AND EXISTS (SELECT 1 FROM "PlanTarget" t WHERE t."planId" = p."id" AND t."userId" = p."creatorId")
		GROUP BY p."creatorId"`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var uid string
		var n int
		if err := rows.Scan(&uid, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out.SelfPlans += n
		touched[uid] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	brows, err := q.QueryContext(ctx, `SELECT "userId", count(*) FROM "UserTargetBook" WHERE "userId" IN (`+ph+`) GROUP BY "userId"`, args...)
	if err != nil {
		return nil, err
	}
	defer brows.Close()
	for brows.Next() {
		var uid string
		var n int
		if err := brows.Scan(&uid, &n); err != nil {
			return nil, err
		}
		out.OwnBooks += n
		touched[uid] = true
	}
	out.Students = len(touched)
	return out, brows.Err()
}

func classMemberIDs(ctx context.Context, q store.Querier, classID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT "userId" FROM "ClassMember" WHERE "classId" = ? ORDER BY "joinedAt", rowid`, classID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

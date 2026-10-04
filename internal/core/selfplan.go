package core

// 班级自主开关与「计划必须落在目标词书里」（spec 0008）。

// SelfPlanAllowed 学生能否自主安排计划：memberships 是所在每个班级的 allowSelfPlan。
// 不在任何班里为真；在班里时任一班不允许就是不允许（多班取最严）。
func SelfPlanAllowed(memberships []bool) bool {
	for _, allow := range memberships {
		if !allow {
			return false
		}
	}
	return true
}

// PlanBooksAllowed 计划所选单元的词书是否都落在约束集合里，返回不在约束里的词书（按 planBooks 的顺序去重）；
// 返回空表示满足。
//
// targetSets 是每个安排对象的目标词书：某个对象的目标为空时不产生约束；
// 布置给多个对象时，约束集合取各对象（非空）目标的交集。全部为空时不约束。
func PlanBooksAllowed(planBooks []string, targetSets [][]string) []string {
	var allowed map[string]bool
	for _, set := range targetSets {
		if len(set) == 0 {
			continue
		}
		cur := make(map[string]bool, len(set))
		for _, b := range set {
			if allowed == nil || allowed[b] {
				cur[b] = true
			}
		}
		allowed = cur
	}
	missing := []string{}
	if allowed == nil {
		return missing
	}
	seen := map[string]bool{}
	for _, b := range planBooks {
		if !allowed[b] && !seen[b] {
			seen[b] = true
			missing = append(missing, b)
		}
	}
	return missing
}

// AllowedPlanBooks 约束集合本身（建计划页只列这些书）：constrained 为假表示不约束（所有对象的目标都为空）。
// 交集按第一个非空集合的顺序输出。
func AllowedPlanBooks(targetSets [][]string) (books []string, constrained bool) {
	var first []string
	for _, set := range targetSets {
		if len(set) > 0 {
			first = set
			break
		}
	}
	if first == nil {
		return []string{}, false
	}
	books = []string{}
	seen := map[string]bool{}
	for _, b := range first {
		if seen[b] {
			continue
		}
		seen[b] = true
		if len(PlanBooksAllowed([]string{b}, targetSets)) == 0 {
			books = append(books, b)
		}
	}
	return books, true
}

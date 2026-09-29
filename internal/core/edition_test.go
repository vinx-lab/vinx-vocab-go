package core

import "testing"

func TestFeaturesOf(t *testing.T) {
	if f := FeaturesOf(EditionSchool); !(f.Classes && f.AssignToOthers && f.MultiUser && f.StudentRecords) {
		t.Fatalf("school = %+v", f)
	}
	if f := FeaturesOf(EditionPersonal); f != (Features{}) {
		t.Fatalf("personal = %+v", f)
	}
	if f := FeaturesOf("unknown"); f != (Features{}) {
		t.Fatalf("unknown = %+v", f)
	}
}

func TestEditionAllows(t *testing.T) {
	cases := []struct {
		ed   Edition
		cap  Capability
		want bool
	}{
		// 班级版：全部能力由角色决定
		{EditionSchool, CapClasses, true},
		{EditionSchool, CapUsers, true},
		{EditionSchool, CapPlansAssign, true},
		{EditionSchool, CapStudentsView, true},
		{EditionSchool, CapSystem, true},
		// 个人版：班级、用户管理、给他人安排计划、看他人记录关闭
		{EditionPersonal, CapClasses, false},
		{EditionPersonal, CapUsers, false},
		{EditionPersonal, CapPlansAssign, false},
		{EditionPersonal, CapStudentsView, false},
		// 与多用户无关的能力不受影响（个人版管理员仍可改系统设置、切回班级版）
		{EditionPersonal, CapSystem, true},
		{EditionPersonal, CapStudy, true},
		{EditionPersonal, CapPlans, true},
		{EditionPersonal, CapBooksRead, true},
		{EditionPersonal, CapBooksEdit, true},
	}
	for _, c := range cases {
		if got := EditionAllows(c.ed, c.cap); got != c.want {
			t.Errorf("EditionAllows(%s, %s) = %v, want %v", c.ed, c.cap, got, c.want)
		}
	}
}

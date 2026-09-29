package core

import (
	"reflect"
	"testing"
)

// 逐条翻译旧 tests/lib/access.test.ts

func TestRoleCan(t *testing.T) {
	cases := []struct {
		role string
		cap  Capability
		want bool
	}{
		// 学生只有学习相关能力
		{"student", CapStudy, true},
		{"student", CapPlans, true},
		{"student", CapBooksRead, true},
		{"student", CapPlansAssign, false},
		{"student", CapClasses, false},
		{"student", CapUsers, false},
		// 老师含教学能力但不含系统能力
		{"teacher", CapClasses, true},
		{"teacher", CapPlansAssign, true},
		{"teacher", CapBooksEdit, true},
		{"teacher", CapStudentsView, true},
		{"teacher", CapSystem, false},
		{"admin", CapSystem, true},
		// 未知角色一律拒绝
		{"guest", CapStudy, false},
		{"", CapStudy, false},
	}
	for _, c := range cases {
		if got := RoleCan(c.role, c.cap); got != c.want {
			t.Errorf("RoleCan(%q,%q) = %v, want %v", c.role, c.cap, got, c.want)
		}
	}
}

func TestCapabilitiesNested(t *testing.T) {
	for _, c := range RoleCapabilities[RoleTeacher] {
		if !RoleCan("admin", c) {
			t.Errorf("admin 缺少 %s", c)
		}
	}
	for _, c := range RoleCapabilities[RoleStudent] {
		if !RoleCan("teacher", c) {
			t.Errorf("teacher 缺少 %s", c)
		}
	}
	want := []string{"study", "plans", "books.read", "plans.assign", "books.edit", "classes", "students.view", "users", "system"}
	if got := CapabilitiesOf("admin"); !reflect.DeepEqual(got, want) {
		t.Errorf("CapabilitiesOf(admin) = %v", got)
	}
	if got := CapabilitiesOf("nope"); got == nil || len(got) != 0 {
		t.Errorf("CapabilitiesOf(nope) = %#v, want 空切片", got)
	}
	// 返回副本
	caps := CapabilitiesOf("student")
	caps[0] = "x"
	if RoleCapabilities[RoleStudent][0] != CapStudy {
		t.Error("CapabilitiesOf 未返回副本")
	}
	if !IsRole("teacher") || IsRole("root") {
		t.Error("IsRole")
	}
}

func TestEdition(t *testing.T) {
	if got := FeaturesOf(EditionPersonal); got != (Features{}) {
		t.Errorf("personal = %+v", got)
	}
	if got := FeaturesOf(EditionSchool); got != (Features{true, true, true, true}) {
		t.Errorf("school = %+v", got)
	}
	if !IsEdition("personal") || IsEdition("enterprise") {
		t.Error("IsEdition")
	}
	if EditionLabel[EditionSchool] != "班级版" {
		t.Error("EditionLabel")
	}
	f := FeaturesOf(EditionPersonal)
	f.Classes = true
	if FeaturesOf(EditionPersonal).Classes {
		t.Error("FeaturesOf 未返回副本")
	}
	if !FeaturesOf(EditionSchool).Has(FeatureClasses) || FeaturesOf(EditionPersonal).Has(FeatureMultiUser) {
		t.Error("Features.Has")
	}
}

func TestPasswordAndTheme(t *testing.T) {
	if ValidatePassword("12345") == "" || ValidatePassword("123456") != "" || ValidatePassword("") == "" {
		t.Error("ValidatePassword")
	}
	if NormalizeThemePref("sepia") != "system" || NormalizeThemePref("dark") != "dark" {
		t.Error("NormalizeThemePref")
	}
	if JSLen("😀a") != 3 || JSLen("中文") != 2 {
		t.Error("JSLen")
	}
}

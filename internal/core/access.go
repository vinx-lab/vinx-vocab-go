// Package core 纯业务规则（对应旧 apps/api/src/lib 与 packages/shared/src）：不触库、不读环境变量，全部配单测。
package core

// Role 角色：student | teacher | admin（照抄 packages/shared/src/access.ts）。
type Role = string

const (
	RoleStudent Role = "student"
	RoleTeacher Role = "teacher"
	RoleAdmin   Role = "admin"
)

// Roles 全部角色，顺序与旧版一致。
var Roles = []Role{RoleStudent, RoleTeacher, RoleAdmin}

// RoleLabel 角色中文名。
var RoleLabel = map[Role]string{RoleStudent: "学生", RoleTeacher: "教师", RoleAdmin: "管理员"}

// Capability 能力（粗粒度，按“能不能做这类事”划分）。
type Capability = string

const (
	CapStudy        Capability = "study"         // 今日 / 学习 / 自己的记录
	CapPlans        Capability = "plans"         // 学习计划（自己的）
	CapPlansAssign  Capability = "plans.assign"  // 给班级或学生安排计划
	CapBooksRead    Capability = "books.read"    // 浏览词书
	CapBooksEdit    Capability = "books.edit"    // 建词书 / 导入 / 编辑词条（含 AI 例句、发音缓存）
	CapClasses      Capability = "classes"       // 班级与成员管理
	CapStudentsView Capability = "students.view" // 查看学生的学习记录
	CapUsers        Capability = "users"         // 用户管理（建号 / 改角色 / 重置密码）
	CapSystem       Capability = "system"        // 系统词书、系统设置
)

var (
	studentCaps = []Capability{CapStudy, CapPlans, CapBooksRead}
	teacherCaps = append(append([]Capability{}, studentCaps...), CapPlansAssign, CapBooksEdit, CapClasses, CapStudentsView, CapUsers)
	adminCaps   = append(append([]Capability{}, teacherCaps...), CapSystem)
)

// RoleCapabilities 角色能力表（前后端共用的唯一事实来源，顺序即下发给前端的顺序）。
var RoleCapabilities = map[Role][]Capability{
	RoleStudent: studentCaps,
	RoleTeacher: teacherCaps,
	RoleAdmin:   adminCaps,
}

// IsRole 是否为已知角色。
func IsRole(v string) bool {
	_, ok := RoleCapabilities[v]
	return ok
}

// RoleCan 角色是否具备某能力；未知角色一律拒绝。
func RoleCan(role string, capability Capability) bool {
	for _, c := range RoleCapabilities[role] {
		if c == capability {
			return true
		}
	}
	return false
}

// CapabilitiesOf 某角色的能力清单（副本）；未知角色返回空切片（JSON 为 []）。
func CapabilitiesOf(role string) []Capability {
	return append([]Capability{}, RoleCapabilities[role]...)
}

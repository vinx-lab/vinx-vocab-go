package core

// Edition 版本：personal（个人版）| school（班级版）。
type Edition = string

const (
	EditionPersonal Edition = "personal"
	EditionSchool   Edition = "school"
)

// EditionLabel 版本中文名。
var EditionLabel = map[Edition]string{EditionPersonal: "个人版", EditionSchool: "班级版"}

// Features 版本决定的功能开关（JSON 字段与旧版 AppConfig.features 一致）。
type Features struct {
	Classes        bool `json:"classes"`        // 班级与成员管理
	AssignToOthers bool `json:"assignToOthers"` // 给班级 / 学生安排计划
	MultiUser      bool `json:"multiUser"`      // 多用户：注册、用户管理、角色
	StudentRecords bool `json:"studentRecords"` // 查看他人学习记录
}

// Feature 路由按功能开关注册时使用的键。
type Feature string

const (
	FeatureClasses        Feature = "classes"
	FeatureAssignToOthers Feature = "assignToOthers"
	FeatureMultiUser      Feature = "multiUser"
	FeatureStudentRecords Feature = "studentRecords"
)

// Has 某功能是否开启。
func (f Features) Has(key Feature) bool {
	switch key {
	case FeatureClasses:
		return f.Classes
	case FeatureAssignToOthers:
		return f.AssignToOthers
	case FeatureMultiUser:
		return f.MultiUser
	case FeatureStudentRecords:
		return f.StudentRecords
	}
	return false
}

// IsEdition 是否为已知版本。
func IsEdition(v string) bool { return v == EditionPersonal || v == EditionSchool }

// FeaturesOf 版本对应的功能（值类型，天然是副本）。
func FeaturesOf(e Edition) Features {
	if e == EditionSchool {
		return Features{Classes: true, AssignToOthers: true, MultiUser: true, StudentRecords: true}
	}
	return Features{}
}

// capabilityFeature 依赖版本功能的能力：功能关闭时，即使角色具备该能力也视为没有（个人版降级后仍可能有老师账号）。
var capabilityFeature = map[Capability]Feature{
	CapClasses:      FeatureClasses,
	CapUsers:        FeatureMultiUser,
	CapPlansAssign:  FeatureAssignToOthers,
	CapStudentsView: FeatureStudentRecords,
}

// EditionAllows 当前版本是否允许使用某能力（与角色无关）；不依赖版本功能的能力总是允许。
func EditionAllows(e Edition, c Capability) bool {
	f, ok := capabilityFeature[c]
	if !ok {
		return true
	}
	return FeaturesOf(e).Has(f)
}

package httpx

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// V 收集 zod 风格的校验问题：路径 → 消息（同一路径后写覆盖前写），默认消息与 zod v3 英文原文一致。
// 典型用法见 internal/api/auth.go 的请求体 Validate 方法。
type V struct {
	details map[string]any
	// typed 解码阶段出现类型错误的路径：与 zod 一致，类型不对的值不再做后续校验，
	// 这些路径及其子路径上的后续 Add 一律忽略（例如 units:"bad" 只报 Expected array，不再报「没有可导入的单元」）。
	typed []string
}

// addType 记录解码阶段的类型错误，并屏蔽该路径及其子路径上之后的问题。
func (v *V) addType(path, msg string) {
	v.Add(path, msg)
	v.typed = append(v.typed, path)
}

func (v *V) blocked(path string) bool {
	for _, p := range v.typed {
		if path == p || strings.HasPrefix(path, p+".") {
			return true
		}
	}
	return false
}

// Add 记录一个问题；path 为空时记为 "body"（与旧 toDetails 一致）。
func (v *V) Add(path, msg string) {
	if path == "" {
		path = "body"
	}
	if v.blocked(path) {
		return
	}
	if v.details == nil {
		v.details = map[string]any{}
	}
	v.details[path] = msg
}

// Has 某路径是否已有问题。
func (v *V) Has(path string) bool { _, ok := v.details[path]; return ok }

// OK 目前没有问题。
func (v *V) OK() bool { return len(v.details) == 0 }

// Err 有问题时返回 VALIDATION「参数校验失败」+ details，否则 nil。
func (v *V) Err() error {
	if v.OK() {
		return nil
	}
	return NewError(CodeValidation, "参数校验失败", v.details)
}

// ---------------------------------------------------------------------------
// 字符串
// ---------------------------------------------------------------------------

// StrCheck 字符串检查或变换：返回（新值, 错误消息）；错误消息为空表示通过。
// 与 zod 一样，一个检查失败后后续检查仍会执行（非致命），details 里保留最后一条。
type StrCheck func(s string) (string, string)

// jsLen JS String.length（UTF-16 码元数）。
func jsLen(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func pick(msg []string, def string) string {
	if len(msg) > 0 && msg[0] != "" {
		return msg[0]
	}
	return def
}

// Trim .trim()
func Trim() StrCheck { return func(s string) (string, string) { return jsTrim(s), "" } }

// Lower .toLowerCase()
func Lower() StrCheck { return func(s string) (string, string) { return strings.ToLower(s), "" } }

// Upper .toUpperCase()
func Upper() StrCheck { return func(s string) (string, string) { return strings.ToUpper(s), "" } }

// Min .min(n, msg?)
func Min(n int, msg ...string) StrCheck {
	return func(s string) (string, string) {
		if jsLen(s) < n {
			return s, pick(msg, fmt.Sprintf("String must contain at least %d character(s)", n))
		}
		return s, ""
	}
}

// Max .max(n, msg?)
func Max(n int, msg ...string) StrCheck {
	return func(s string) (string, string) {
		if jsLen(s) > n {
			return s, pick(msg, fmt.Sprintf("String must contain at most %d character(s)", n))
		}
		return s, ""
	}
}

// zod v3 的邮箱正则（去掉 RE2 不支持的前瞻，改为代码判断）。
var emailRe = regexp.MustCompile(`(?i)^([A-Z0-9_'+\-.]*)[A-Z0-9_+-]@([A-Z0-9][A-Z0-9\-]*\.)+[A-Z]{2,}$`)

// Email .email(msg?)
func Email(msg ...string) StrCheck {
	return func(s string) (string, string) {
		if strings.HasPrefix(s, ".") || strings.Contains(s, "..") || !emailRe.MatchString(s) {
			return s, pick(msg, "Invalid email")
		}
		return s, ""
	}
}

// Regex .regex(re, msg?)
func Regex(re *regexp.Regexp, msg ...string) StrCheck {
	return func(s string) (string, string) {
		if !re.MatchString(s) {
			return s, pick(msg, "Invalid")
		}
		return s, ""
	}
}

// Refine .refine(fn, msg)
func Refine(fn func(string) bool, msg string) StrCheck {
	return func(s string) (string, string) {
		if !fn(s) {
			return s, msg
		}
		return s, ""
	}
}

func (v *V) runStr(path, s string, checks []StrCheck) string {
	for _, c := range checks {
		var msg string
		s, msg = c(s)
		if msg != "" {
			v.Add(path, msg)
		}
	}
	return s
}

// Str 必填字符串：缺省 → "Required"，null → "Expected string, received null"。
func (v *V) Str(path string, o Opt[string], checks ...StrCheck) string {
	if v.Has(path) { // 解码阶段已记录类型错误：与 zod 一样不再跑后续检查
		return ""
	}
	if !o.Set {
		v.Add(path, "Required")
		return ""
	}
	if o.Null {
		v.Add(path, "Expected string, received null")
		return ""
	}
	return v.runStr(path, o.Val, checks)
}

// OptStr 可选字符串（.optional()）：缺省返回 nil；null → "Expected string, received null"。
func (v *V) OptStr(path string, o Opt[string], checks ...StrCheck) *string {
	if !o.Set || v.Has(path) {
		return nil
	}
	if o.Null {
		v.Add(path, "Expected string, received null")
		return nil
	}
	s := v.runStr(path, o.Val, checks)
	return &s
}

// NullStr 可选且可为 null 的字符串（.optional().nullable()）：原样返回 Opt（Val 为检查/变换后的值）。
func (v *V) NullStr(path string, o Opt[string], checks ...StrCheck) Opt[string] {
	if !o.Set || o.Null || v.Has(path) {
		return o
	}
	o.Val = v.runStr(path, o.Val, checks)
	return o
}

func joinValues(values []string) string {
	q := make([]string, len(values))
	for i, s := range values {
		q[i] = "'" + s + "'"
	}
	return strings.Join(q, " | ")
}

// Enum 必填枚举（z.enum(values, { message })）；msg 为空时用 zod 默认消息。
func (v *V) Enum(path string, o Opt[string], values []string, msg string) string {
	if v.Has(path) {
		return ""
	}
	if !o.Set {
		v.Add(path, "Required")
		return ""
	}
	return v.enumValue(path, o, values, msg)
}

// OptEnum 可选枚举：缺省返回 def。
func (v *V) OptEnum(path string, o Opt[string], values []string, msg, def string) string {
	if !o.Set {
		return def
	}
	s := v.enumValue(path, o, values, msg)
	if s == "" {
		return def
	}
	return s
}

func (v *V) enumValue(path string, o Opt[string], values []string, msg string) string {
	if v.Has(path) {
		return ""
	}
	if o.Null {
		v.Add(path, pick([]string{msg}, "Expected "+joinValues(values)+", received null"))
		return ""
	}
	for _, x := range values {
		if x == o.Val {
			return x
		}
	}
	v.Add(path, pick([]string{msg}, "Invalid enum value. Expected "+joinValues(values)+", received '"+o.Val+"'"))
	return ""
}

// ---------------------------------------------------------------------------
// 数字、布尔、数组
// ---------------------------------------------------------------------------

// IntRange z.number().int().min(min).max(max)；msgMin / msgMax 为空用默认消息。
type IntRange struct {
	Min, Max       *int
	MsgMin, MsgMax string
}

// Between 构造闭区间。
func Between(min, max int) IntRange { return IntRange{Min: &min, Max: &max} }

// Int 整数字段：缺省时返回 def（def 为 nil 表示必填 → "Required"）。
func (v *V) Int(path string, o Opt[float64], def *int, rng IntRange) int {
	if v.Has(path) {
		return 0
	}
	if !o.Set {
		if def != nil {
			return *def
		}
		v.Add(path, "Required")
		return 0
	}
	if v.Has(path) {
		return 0
	}
	if o.Null {
		v.Add(path, "Expected number, received null")
		return 0
	}
	f := o.Val
	if f != math.Trunc(f) {
		v.Add(path, "Expected integer, received float")
	}
	if rng.Min != nil && f < float64(*rng.Min) {
		v.Add(path, pick([]string{rng.MsgMin}, fmt.Sprintf("Number must be greater than or equal to %d", *rng.Min)))
	}
	if rng.Max != nil && f > float64(*rng.Max) {
		v.Add(path, pick([]string{rng.MsgMax}, fmt.Sprintf("Number must be less than or equal to %d", *rng.Max)))
	}
	return int(f)
}

// OptBool 可选布尔：缺省返回 def；null → "Expected boolean, received null"。
func (v *V) OptBool(path string, o Opt[bool], def bool) bool {
	if !o.Set || v.Has(path) {
		return def
	}
	if o.Null {
		v.Add(path, "Expected boolean, received null")
		return def
	}
	return o.Val
}

// ArrayLen z.array(...).min(min, msg).max(max, msg) 的长度检查；min/max 为负表示不检查。
func (v *V) ArrayLen(path string, n, min, max int, msgMin, msgMax string) {
	if min >= 0 && n < min {
		v.Add(path, pick([]string{msgMin}, fmt.Sprintf("Array must contain at least %d element(s)", min)))
	}
	if max >= 0 && n > max {
		v.Add(path, pick([]string{msgMax}, fmt.Sprintf("Array must contain at most %d element(s)", max)))
	}
}

// ---------------------------------------------------------------------------

func isJSSpace(r rune) bool {
	switch {
	case r == '\t', r == '\n', r == '\v', r == '\f', r == '\r', r == ' ', r == 0xa0, r == 0x1680,
		r >= 0x2000 && r <= 0x200a, r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000, r == 0xfeff:
		return true
	}
	return false
}

// jsTrim 等价于 JS String.prototype.trim。
func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

// JSTrim 导出给业务代码使用（与 zod .trim() 相同的空白集合）。
func JSTrim(s string) string { return jsTrim(s) }

// CoerceInt z.coerce.number().int().min().max().default(def)：字符串按 JS Number() 转数字
// （空串为 0，非数字 → "Expected number, received nan"）。
func (v *V) CoerceInt(path string, o Opt[string], def *int, rng IntRange) int {
	if v.Has(path) {
		return 0
	}
	if !o.Set || o.Null {
		if def != nil {
			return *def
		}
		v.Add(path, "Required")
		return 0
	}
	s := jsTrim(o.Val)
	f := 0.0
	if s != "" {
		var err error
		if f, err = strconv.ParseFloat(s, 64); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			v.Add(path, "Expected number, received nan")
			return 0
		}
	}
	return v.Int(path, Some(f), def, rng)
}

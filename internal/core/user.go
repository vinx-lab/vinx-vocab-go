package core

import "unicode/utf16"

// ThemePrefs 外观偏好：跟随系统 / 浅色 / 深色。
var ThemePrefs = []string{"system", "light", "dark"}

// IsThemePref 是否为合法外观偏好。
func IsThemePref(v string) bool {
	for _, t := range ThemePrefs {
		if t == v {
			return true
		}
	}
	return false
}

// NormalizeThemePref 非法或缺失的值一律按「跟随系统」处理。
func NormalizeThemePref(v string) string {
	if IsThemePref(v) {
		return v
	}
	return "system"
}

// ValidatePassword 密码策略：至少 6 位（按 JS 字符串长度计）。返回错误消息，合法时返回 ""。
func ValidatePassword(pwd string) string {
	if JSLen(pwd) < 6 {
		return "密码长度至少 6 位"
	}
	return ""
}

// JSLen 按 JavaScript 的 String.length（UTF-16 码元数）计算长度，校验规则与旧版 zod 一致。
func JSLen(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

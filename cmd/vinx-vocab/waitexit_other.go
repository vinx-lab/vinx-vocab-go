//go:build !windows

package main

// defaultConsoleProcessCount 非 Windows 平台不会被 isDoubleClickLaunch 调用（先按 GOOS 短路），
// 这里只是为了让 waitexit.go 在所有平台上都能编译。
func defaultConsoleProcessCount() int { return 0 }

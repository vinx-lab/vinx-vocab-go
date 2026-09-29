//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
)

// defaultConsoleProcessCount 附着在当前控制台上的进程数（GetConsoleProcessList）。缓冲区
// 只需能装下 "是否 <= 1"：即便实际进程数更多、缓冲区不够，Win32 会返回所需大小（必然 > 1），
// 结论不变，不需要重试放大缓冲区。
func defaultConsoleProcessCount() int {
	var pids [8]uint32
	ret, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return int(ret)
}

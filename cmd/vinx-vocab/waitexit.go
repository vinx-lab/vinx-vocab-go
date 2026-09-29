package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
)

// consoleProcessCountFn 返回附着在当前控制台上的进程数；由平台专属文件
// （waitexit_windows.go / waitexit_other.go）提供实现，非 Windows 上不会被调用。
var consoleProcessCountFn = defaultConsoleProcessCount

// waitDecision 是否应在退出前等待用户按回车：纯函数，与 isDoubleClickLaunch 的唯一区别是
// goos、processCount 由调用方传入，便于不依赖真实 GOOS / Win32 syscall 的单测。
//
// 双击启动 exe 时 Windows 会为进程新建一个控制台，只有本进程（以及可能的 conhost，不计入
// GetConsoleProcessList）附着，返回 1；从已经存在的 cmd / PowerShell 启动时，控制台上还
// 挂着外层 shell 等其他进程，返回值 >= 2。非 Windows 平台一律不等待（终端本来就不会消失）。
func waitDecision(goos string, processCount int) bool {
	return goos == "windows" && processCount <= 1
}

// isDoubleClickLaunch 当前进程是不是被双击启动（独占一个新控制台）。
func isDoubleClickLaunch() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return waitDecision(runtime.GOOS, consoleProcessCountFn())
}

// waitForEnter 打印提示并阻塞到用户按下回车（或输入被关闭）。
func waitForEnter(stdout io.Writer, stdin io.Reader) {
	fmt.Fprint(stdout, "\n按回车键关闭窗口…")
	bufio.NewReader(stdin).ReadString('\n')
}

// exitWait serve 各失败出口 / 已在运行时统一走这里返回：双击启动时先等用户按回车再关窗口，
// 避免窗口一闪而过、用户看不到错误提示；其余情况（Linux、带参数 /
// 从已有终端启动）行为不变，立即返回 code。
func exitWait(code int, stdout io.Writer) int {
	if isDoubleClickLaunch() {
		waitForEnter(stdout, os.Stdin)
	}
	return code
}

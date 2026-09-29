package main

import (
	"os/exec"
	"runtime"
)

// openBrowser 用系统默认浏览器打开地址（只在 Windows 双击运行时调用；失败忽略）。
func openBrowser(url string) {
	if runtime.GOOS == "windows" {
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
}

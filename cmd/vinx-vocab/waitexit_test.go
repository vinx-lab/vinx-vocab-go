package main

import (
	"strings"
	"testing"
	"time"
)

func TestWaitDecision(t *testing.T) {
	cases := []struct {
		goos    string
		count   int
		want    bool
		comment string
	}{
		{"windows", 1, true, "双击启动：控制台只有本进程"},
		{"windows", 0, true, "极端情况下 GetConsoleProcessList 也可能报 0，同样按双击处理"},
		{"windows", 2, false, "从已有 cmd / PowerShell 启动：控制台上还有外层 shell"},
		{"windows", 8, false, "从已有终端启动，附着进程更多"},
		{"linux", 1, false, "非 Windows 平台不等待"},
		{"darwin", 1, false, "非 Windows 平台不等待"},
	}
	for _, c := range cases {
		if got := waitDecision(c.goos, c.count); got != c.want {
			t.Errorf("waitDecision(%q, %d) = %v, want %v (%s)", c.goos, c.count, got, c.want, c.comment)
		}
	}
}

func TestWaitForEnter(t *testing.T) {
	var out strings.Builder
	in := strings.NewReader("\n")
	waitForEnter(&out, in)
	if !strings.Contains(out.String(), "按回车键关闭窗口") {
		t.Fatalf("提示文案缺失：%q", out.String())
	}
}

func TestWaitForEnter_ClosedInput(t *testing.T) {
	// stdin 被关闭（EOF）时不应该死循环挂住。
	var out strings.Builder
	in := strings.NewReader("")
	done := make(chan struct{})
	go func() {
		waitForEnter(&out, in)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitForEnter 在 EOF 时没有返回")
	}
}

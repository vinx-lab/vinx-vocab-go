package migrate

import (
	"fmt"
	"os"
	"testing"
)

// TestMain 没有 PostgreSQL 时导入测试会跳过：往 stderr 打一行醒目提示，避免 go test 只显示 ok 被当成已验证。
// 完整运行：make test-import（需要 VINX_TEST_PG_URL，缺少时失败而不是跳过）。
func TestMain(m *testing.M) {
	if os.Getenv("VINX_TEST_PG_URL") == "" {
		fmt.Fprint(os.Stderr, "\n!!! internal/migrate：未设置 VINX_TEST_PG_URL，PostgreSQL 导入测试已跳过（只跑了纯函数测试）。完整运行：VINX_TEST_PG_URL=postgresql://…/postgres make test-import\n")
	}
	os.Exit(m.Run())
}

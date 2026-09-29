package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	coreai "github.com/vinx-lab/vinx-vocab-go/internal/core/ai"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/secretbox"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// seedAISetting 直接往库里写一条数据库来源的 AI 配置（绕开 HTTP /settings/ai），用于测试需要一个
// "已配置好、带真实 Key" 的 AIConfigCache 的场景。
func seedAISetting(t *testing.T, d *Deps, apiKey string) {
	t.Helper()
	enc, err := secretbox.Encrypt(apiKey, d.Cfg.SettingsSecret)
	if err != nil {
		t.Fatal(err)
	}
	value := map[string]any{"provider": "openai", "baseUrl": "http://127.0.0.1:1/v1", "apiKeyEnc": enc, "model": "m", "timeoutMs": 10_000}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.Exec(`INSERT INTO "AppSetting" ("key","value","updatedAt") VALUES ('ai',?,?)`, string(raw), store.NewTime(time.Now())); err != nil {
		t.Fatal(err)
	}
	d.AIConfig.Invalidate()
}

// I3（评审 round 1）：任务 goroutine 里的 panic 必须被接住，标成 failed，不能打断整个进程。

func waitJobDone(t *testing.T, d *Deps, jobID, ownerID string, timeout time.Duration) coreai.JobView {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		v, ok := d.AIJobs.View(jobID, ownerID)
		if !ok {
			t.Fatalf("job %s not found", jobID)
		}
		if v.Status != coreai.JobRunning {
			return *v
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s did not finish in time", jobID)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStartAIJobRecoversFromPanicString(t *testing.T) {
	e := newEnv(t, nil)
	started, err := startAIJob(e.d, "u1", coreai.JobKindExample, func(ctx context.Context) (string, error) {
		panic("boom: something went very wrong")
	})
	if err != nil {
		t.Fatal(err)
	}
	v := waitJobDone(t, e.d, started.JobID, "u1", 2*time.Second)
	if v.Status != coreai.JobFailed {
		t.Fatalf("status = %s, want failed", v.Status)
	}
	if !strings.Contains(v.Error, "boom: something went very wrong") {
		t.Fatalf("error = %q, missing panic message", v.Error)
	}
	// 面板消息走的是与普通错误相同的 DescribeError 路径：非超时/非连接类错误落在
	// "AI 调用失败：<message>" 这一支，消息形态与真实错误一致。
	if !strings.HasPrefix(v.Error, "AI 调用失败：") {
		t.Fatalf("error = %q, expected \"AI 调用失败：\" prefix (same shape oracle produces for thrown errors)", v.Error)
	}
}

func TestStartAIJobRecoversFromPanicError(t *testing.T) {
	e := newEnv(t, nil)
	started, err := startAIJob(e.d, "u2", coreai.JobKindPassage, func(ctx context.Context) (int, error) {
		panic(errors.New("nil pointer somewhere"))
	})
	if err != nil {
		t.Fatal(err)
	}
	v := waitJobDone(t, e.d, started.JobID, "u2", 2*time.Second)
	if v.Status != coreai.JobFailed {
		t.Fatalf("status = %s, want failed", v.Status)
	}
	if v.Error != "AI 调用失败：nil pointer somewhere" {
		t.Fatalf("error = %q", v.Error)
	}
}

func TestStartAIJobRecoversFromPanicRedactsKey(t *testing.T) {
	e := newEnv(t, nil)
	key := "sk-panic-test-secret-1234"
	// 配置一个 AI Key，panic 消息里带这个 Key，确认 recover 路径也会脱敏（不是只有正常错误路径才脱敏）。
	seedAISetting(t, e.d, key)

	started, err := startAIJob(e.d, "u3", coreai.JobKindExample, func(ctx context.Context) (string, error) {
		panic("upstream said: " + key)
	})
	if err != nil {
		t.Fatal(err)
	}
	v := waitJobDone(t, e.d, started.JobID, "u3", 2*time.Second)
	if v.Status != coreai.JobFailed {
		t.Fatalf("status = %s, want failed", v.Status)
	}
	if strings.Contains(v.Error, key) {
		t.Fatalf("panic message leaked the API key: %q", v.Error)
	}
	if !strings.Contains(v.Error, "***") {
		t.Fatalf("expected redacted marker in %q", v.Error)
	}
}

// TestStartAIJobPanicDoesNotCrashProcess 起多个会 panic 的任务，确认测试进程（模拟 serve 进程）本身
// 不受影响——这是本次修复的核心诉求：一个任务的 panic 不能打断其他人的任务和请求。
func TestStartAIJobPanicDoesNotCrashProcess(t *testing.T) {
	e := newEnv(t, nil)
	var jobIDs []string
	for i := 0; i < 5; i++ {
		started, err := startAIJob(e.d, "u-multi", coreai.JobKindExample, func(ctx context.Context) (string, error) {
			panic("boom")
		})
		if err != nil {
			t.Fatal(err)
		}
		jobIDs = append(jobIDs, started.JobID)
		// 每个账号同时最多 2 个进行中的任务；等前一个失败结束再开下一个。
		waitJobDone(t, e.d, started.JobID, "u-multi", 2*time.Second)
	}
	for _, id := range jobIDs {
		v, ok := e.d.AIJobs.View(id, "u-multi")
		if !ok || v.Status != coreai.JobFailed {
			t.Fatalf("job %s = %+v ok=%v", id, v, ok)
		}
	}
	// 进程还活着、还能正常处理请求，就是这个测试本身还在跑完成的证据。
	if r := e.do("GET", "/api/health", "", nil); r.Status != 200 {
		t.Fatalf("health after panics = %d", r.Status)
	}
}

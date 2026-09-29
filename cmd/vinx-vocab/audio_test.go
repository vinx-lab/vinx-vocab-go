package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeAudioServer 假发音源：>=512 字节的 audio/mpeg；failFirstN>0 时对每个不同路径的前 N 次请求返回 500
// （用于测试「失败重试 1 次」）。单元测试单线程访问 counts，无需加锁。
func fakeAudioServer(t *testing.T, failFirstN int) *httptest.Server {
	t.Helper()
	counts := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counts[r.URL.Path]++
		if counts[r.URL.Path] <= failFirstN {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("content-type", "audio/mpeg")
		w.Write(bytes.Repeat([]byte{1}, 1000))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func seedDemoData(t *testing.T, dataDir string) {
	t.Helper()
	var out, errb bytes.Buffer
	if code := run([]string{"seed-demo", "--data", dataDir}, &out, &errb); code != 0 {
		t.Fatalf("seed-demo failed: %d %s", code, errb.String())
	}
}

func TestAudioPrefetchDryRunAndApply(t *testing.T) {
	dir := t.TempDir()
	seedDemoData(t, dir)

	srv := fakeAudioServer(t, 0)
	t.Setenv("AUDIO_PROVIDER_URL", srv.URL+"/{word}")

	var out, errb bytes.Buffer
	code := run([]string{"audio", "prefetch", "--data", dir, "--limit", "5"}, &out, &errb)
	if code != 0 {
		t.Fatalf("dry run failed: %d %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "（演练）") {
		t.Fatalf("expected dry-run marker, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "词总数") {
		t.Fatalf("missing stats line: %s", out.String())
	}
	// 演练不应创建 audio 目录
	if _, err := os.Stat(filepath.Join(dir, "audio")); err == nil {
		t.Fatal("dry run should not create audio dir")
	}

	out.Reset()
	errb.Reset()
	code = run([]string{"audio", "prefetch", "--data", dir, "--apply", "--limit", "3", "--delay", "1"}, &out, &errb)
	if code != 0 {
		t.Fatalf("apply failed: %d %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "成功 3") {
		t.Fatalf("expected 3 successes: %s", out.String())
	}
	entries, err := os.ReadDir(filepath.Join(dir, "audio"))
	if err != nil {
		t.Fatal(err)
	}
	mp3s := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".mp3") {
			mp3s++
		}
	}
	if mp3s != 3 {
		t.Fatalf("expected 3 cached .mp3 files, got %d (entries: %v)", mp3s, entries)
	}
	// _failed.json 与旧脚本一致：无论有没有失败都会（重）写一份（内容为空数组）。
	if _, err := os.Stat(filepath.Join(dir, "audio", "_failed.json")); err != nil {
		t.Fatalf("expected _failed.json to exist: %v", err)
	}

	// 重跑：已缓存的应跳过，不再重复抓取
	out.Reset()
	errb.Reset()
	code = run([]string{"audio", "prefetch", "--data", dir, "--delay", "1"}, &out, &errb)
	if code != 0 {
		t.Fatalf("second dry run failed: %d %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "已缓存 3 个文件") {
		t.Fatalf("expected 3 cached files reported: %s", out.String())
	}
}

func TestAudioPrefetchNotConfigured(t *testing.T) {
	dir := t.TempDir()
	seedDemoData(t, dir)
	t.Setenv("AUDIO_PROVIDER_URL", "")
	var out, errb bytes.Buffer
	code := run([]string{"audio", "prefetch", "--data", dir}, &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "未配置 AUDIO_PROVIDER_URL") {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
}

func TestAudioPrefetchRetryThenFailWritesFailedJSON(t *testing.T) {
	dir := t.TempDir()
	seedDemoData(t, dir)
	// 始终失败：重试 1 次（隔 5 秒）后仍失败，应写入 _failed.json；只抓 1 个避免测试过慢。
	srv := fakeAudioServer(t, 1000)
	t.Setenv("AUDIO_PROVIDER_URL", srv.URL+"/{word}")

	var out, errb bytes.Buffer
	code := run([]string{"audio", "prefetch", "--data", dir, "--apply", "--limit", "1", "--delay", "1"}, &out, &errb)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "失败 1") {
		t.Fatalf("expected 1 failure: %s", out.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, "audio", "_failed.json"))
	if err != nil {
		t.Fatal(err)
	}
	var failed []map[string]any
	if err := json.Unmarshal(b, &failed); err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Fatalf("expected 1 failed entry, got %d", len(failed))
	}
}

func TestAudioUnknownSubcommand(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"audio", "bogus"}, &out, &errb)
	if code != 2 || !strings.Contains(errb.String(), "未知 audio 子命令") {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
}

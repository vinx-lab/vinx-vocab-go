package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
)

// 评审 M8：缓存写入原子化（临时文件 + rename），并发请求同一个词去重（singleflight）。

func TestWriteAudioFileAtomicLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte{7}, 1000)
	if err := WriteAudioFileAtomic(dir, "abc.mp3", data); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "abc.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "abc.mp3" {
			t.Fatalf("unexpected leftover entry: %s", e.Name())
		}
	}
}

func TestGetWordAudioConcurrentRequestsDeduped(t *testing.T) {
	dir := t.TempDir()
	audioDir := filepath.Join(dir, "audio")
	db := openTempDB(t)
	insertWord(t, db, "w1", "hello", "你好")

	var hits atomic.Int32
	var wg sync.WaitGroup
	block := make(chan struct{}) // 关闭前一直卡住响应，逼所有并发调用都排到同一个 singleflight key 上
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-block
		w.Header().Set("content-type", "audio/mpeg")
		w.Write(bytes.Repeat([]byte{9}, 1000))
	}))
	defer srv.Close()

	cfg := &config.Config{AudioDir: audioDir, AudioProviderURL: srv.URL + "/{word}"}

	const n = 8
	results := make([][]byte, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			buf, _, err := GetWordAudio(context.Background(), db, cfg, "w1")
			results[i] = buf
			errs[i] = err
		}(i)
	}
	// 给并发 goroutine 一点时间全部进入 audioFetchGroup.Do 排队（只有第一个真正发请求、卡在 <-block
	// 上，其余在 singleflight 里等待同一个结果），再放行那一次下载。
	time.Sleep(100 * time.Millisecond)
	close(block)
	wg.Wait()

	if got := hits.Load(); got != 1 {
		t.Fatalf("expected exactly 1 real download, got %d", got)
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if !bytes.Equal(results[i], bytes.Repeat([]byte{9}, 1000)) {
			t.Fatalf("call %d: unexpected content", i)
		}
	}
}

// 复审 round 2：fn panic 时必须仍然关闭 done、清掉 key，否则排队的 follower 和此后同一个 key 的所有
// 调用都会永久卡死（直到进程重启）。这里直接测 keyedSingleflight，不经 GetWordAudio，更贴近问题本身。
func TestKeyedSingleflightPanicUnblocksFollowersAndCleansUpKey(t *testing.T) {
	var g keyedSingleflight
	const key = "k"

	leaderEntered := make(chan struct{})
	releaseLeader := make(chan struct{})

	type callResult struct {
		val any
		err error
	}
	// 只有拿到 leader 身份的那一次调用会真正执行 fn；其余（key 已存在）在 Do 内部直接排队等待，
	// 不会调用这个闭包，所以多个 goroutine 共用同一个闭包也不会重复 close(leaderEntered)。
	panicFn := func() (any, error) {
		close(leaderEntered)
		<-releaseLeader
		panic("boom: injected panic")
	}
	call := func() <-chan callResult {
		ch := make(chan callResult, 1)
		go func() {
			v, err := g.Do(key, panicFn)
			ch <- callResult{v, err}
		}()
		return ch
	}

	leaderCh := call()
	<-leaderEntered // 确认 leader 已经进了 fn、正卡在 <-releaseLeader 上

	const followers = 5
	followerChs := make([]<-chan callResult, followers)
	for i := range followerChs {
		followerChs[i] = call()
	}
	// 给 follower 一点时间真正排到 g.calls[key] 上等待（不是自己抢到当了新 leader）。
	time.Sleep(150 * time.Millisecond)

	close(releaseLeader) // 放行，触发 panic

	timeout := time.After(2 * time.Second)
	check := func(ch <-chan callResult, name string) {
		t.Helper()
		select {
		case r := <-ch:
			if r.err == nil {
				t.Fatalf("%s: expected error after panic, got nil (val=%v)", name, r.val)
			}
		case <-timeout:
			t.Fatalf("%s: did not return promptly after fn panicked (deadlock)", name)
		}
	}
	check(leaderCh, "leader")
	for i, ch := range followerChs {
		check(ch, fmt.Sprintf("follower %d", i))
	}

	// 之后对同一个 key 的调用应该重新执行 fn（key 被清理掉了，不是永远卡住，也不是复用旧的失败结果）。
	ran := false
	v, err := g.Do(key, func() (any, error) {
		ran = true
		return "ok", nil
	})
	if !ran {
		t.Fatal("expected fn to run again for the same key after the panic was cleaned up")
	}
	if err != nil || v != "ok" {
		t.Fatalf("got v=%v err=%v", v, err)
	}
}

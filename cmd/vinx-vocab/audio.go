// audio prefetch：全量预缓存真人发音（K37，对应旧 apps/api/scripts/prefetch-audio.ts）。
//
//	vinx-vocab audio prefetch [--data 数据目录]                       演练：只统计，不发请求
//	vinx-vocab audio prefetch --apply [--limit 50]                    小批量试抓
//	vinx-vocab audio prefetch --apply [--delay 1500]                  全量（两次请求间隔毫秒）
//
// 读音文本相同的词共用一个文件，只抓一次；已缓存的跳过，中断后重跑会接着抓。
// audioplan.Target() 清理后仍残留斜线或括号的（兜底）不抓，只在输出里列出。
// 失败的隔 5 秒重试 1 次，仍失败的写进 <AudioDir>/_failed.json（每次运行覆盖）。
// 抓到后同时把 Word.audioFile 记上；已缓存但没记上的词也一并补记。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/audioplan"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

func audioCmd(args []string, stdout, stderr io.Writer) int {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "prefetch":
		return audioPrefetch(args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知 audio 子命令 %q，可用：prefetch\n", sub)
		return 2
	}
}

func audioPrefetch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("audio prefetch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	data := fs.String("data", "", "数据目录")
	apply := fs.Bool("apply", false, "实际发请求（默认只演练统计）")
	limit := fs.Int("limit", -1, "本次最多抓取的文件数（默认不限）")
	delay := fs.Int("delay", 1500, "两次请求间隔毫秒")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load(config.Options{DataDir: *data})
	if err != nil {
		fmt.Fprintln(stderr, "启动失败：", err)
		return 1
	}
	if !service.AudioEnabled(cfg) {
		fmt.Fprintln(stderr, "未配置 AUDIO_PROVIDER_URL，无需预缓存")
		return 1
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(stderr, "打开数据库失败：", err)
		return 1
	}
	defer db.Close()

	ctx := context.Background()
	words, err := loadAllWords(ctx, db)
	if err != nil {
		fmt.Fprintln(stderr, "读取词表失败：", err)
		return 1
	}
	cached, err := cachedFiles(cfg.AudioDir)
	if err != nil {
		fmt.Fprintln(stderr, "读取缓存目录失败：", err)
		return 1
	}
	plan := audioplan.PlanAudioFetch(words, cached)

	n := *limit
	if n < 0 || n > len(plan.Todo) {
		n = len(plan.Todo)
	}
	batch := plan.Todo[:n]

	fmt.Fprintf(stdout, "词总数 %d；读音文本为空 %d\n", plan.Total, len(plan.Empty))
	unlinkedWords := 0
	for _, g := range plan.Unlinked {
		unlinkedWords += len(g.WordIDs)
	}
	fmt.Fprintf(stdout, "已缓存 %d 个文件，覆盖 %d 个词；其中 %d 个词未记 audioFile\n", plan.CachedFiles, plan.CachedWords, unlinkedWords)
	todoWords := 0
	for _, g := range plan.Todo {
		todoWords += len(g.WordIDs)
	}
	fmt.Fprintf(stdout, "待抓 %d 个文件，覆盖 %d 个词\n", len(plan.Todo), todoWords)
	suspectTexts := make([]string, len(plan.Suspect))
	for i, g := range plan.Suspect {
		suspectTexts[i] = g.Text
	}
	fmt.Fprintf(stdout, "读音文本可疑、不抓 %d 个：%s\n", len(plan.Suspect), joinStrings(suspectTexts, " | "))
	fmt.Fprintf(stdout, "读音相同共用文件 %d 组：%s\n", len(plan.Shared), sharedSummary(plan.Shared))
	mode := ""
	if !*apply {
		mode = "（演练）"
	}
	minutes := (len(batch)*(*delay) + 59_999) / 60_000
	fmt.Fprintf(stdout, "本次%s：抓 %d 个，间隔 %dms，预计 %d 分钟\n", mode, len(batch), *delay, minutes)
	if !*apply {
		return 0
	}

	if err := os.MkdirAll(cfg.AudioDir, 0o755); err != nil {
		fmt.Fprintln(stderr, "创建缓存目录失败：", err)
		return 1
	}
	for _, g := range plan.Unlinked {
		if err := linkWords(ctx, db, g.File, g.WordIDs); err != nil {
			fmt.Fprintln(stderr, "补记 audioFile 失败：", err)
			return 1
		}
	}

	failedFile := filepath.Join(cfg.AudioDir, "_failed.json")
	type failedItem struct {
		Text    string   `json:"text"`
		File    string   `json:"file"`
		WordIDs []string `json:"wordIds"`
		Error   string   `json:"error"`
	}
	failed := []failedItem{}
	ok := 0
	started := time.Now()
	for i, t := range batch {
		errMsg := fetchOne(ctx, db, cfg, t)
		if errMsg != "" {
			failed = append(failed, failedItem{Text: t.Text, File: t.File, WordIDs: t.WordIDs, Error: errMsg})
			if b, err := json.MarshalIndent(failed, "", "  "); err == nil {
				os.WriteFile(failedFile, b, 0o644)
			}
			fmt.Fprintf(stdout, "  x %s：%s\n", t.Text, errMsg)
		} else {
			ok++
		}
		if (i+1)%50 == 0 || i == len(batch)-1 {
			min := time.Since(started).Minutes()
			fmt.Fprintf(stdout, "[%d/%d] 成功 %d，失败 %d，已用 %.1f 分钟\n", i+1, len(batch), ok, len(failed), min)
		}
		if i < len(batch)-1 {
			time.Sleep(time.Duration(*delay) * time.Millisecond)
		}
	}
	if b, err := json.MarshalIndent(failed, "", "  "); err == nil {
		os.WriteFile(failedFile, b, 0o644)
	}
	tail := ""
	if len(failed) > 0 {
		tail = fmt.Sprintf("，清单见 %s", failedFile)
	}
	fmt.Fprintf(stdout, "完成：成功 %d，跳过（已缓存）%d，失败 %d%s\n", ok, plan.CachedFiles, len(failed), tail)
	return 0
}

func loadAllWords(ctx context.Context, db *store.DB) ([]audioplan.Word, error) {
	rows, err := db.QueryContext(ctx, `SELECT "id","spelling","audioFile" FROM "Word"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []audioplan.Word
	for rows.Next() {
		var w audioplan.Word
		var audioFile *string
		if err := rows.Scan(&w.ID, &w.Spelling, &audioFile); err != nil {
			return nil, err
		}
		if audioFile != nil {
			w.AudioFile = *audioFile
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func cachedFiles(dir string) (map[string]bool, error) {
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) < 4 || name[len(name)-4:] != ".mp3" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() >= service.AudioMinBytes {
			out[name] = true
		}
	}
	return out, nil
}

func linkWords(ctx context.Context, db *store.DB, file string, wordIDs []string) error {
	if len(wordIDs) == 0 {
		return nil
	}
	args := append([]any{file}, store.Args(wordIDs)...)
	_, err := db.ExecContext(ctx, `UPDATE "Word" SET "audioFile" = ? WHERE "id" IN (`+store.Placeholders(len(wordIDs))+`)`, args...)
	return err
}

// fetchOne 抓一个目标文件；失败隔 5 秒重试 1 次，仍失败返回错误说明。
func fetchOne(ctx context.Context, db *store.DB, cfg *config.Config, t audioplan.Item) string {
	const retryWait = 5 * time.Second
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		buf, err := service.DownloadAudio(ctx, cfg, t.Text)
		if err == nil {
			if werr := service.WriteAudioFileAtomic(cfg.AudioDir, t.File, buf); werr != nil {
				return werr.Error()
			}
			if lerr := linkWords(ctx, db, t.File, t.WordIDs); lerr != nil {
				return lerr.Error()
			}
			return ""
		}
		lastErr = err
		if attempt == 0 {
			time.Sleep(retryWait)
		}
	}
	return lastErr.Error()
}

func joinStrings(xs []string, sep string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}

func sharedSummary(groups []audioplan.Item) string {
	n := len(groups)
	if n > 8 {
		groups = groups[:8]
	}
	parts := make([]string, len(groups))
	for i, g := range groups {
		parts[i] = g.Text + "×" + strconv.Itoa(len(g.WordIDs))
	}
	out := joinStrings(parts, "、")
	if n > 8 {
		out += " …"
	}
	return out
}

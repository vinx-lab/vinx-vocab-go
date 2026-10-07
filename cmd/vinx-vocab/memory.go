// memory backfill：历史补算（spec 0009 §5）。
//
//	vinx-vocab memory backfill [--data 数据目录]          演练：列出会补哪些词、补算前后，不写库
//	vinx-vocab memory backfill --apply [--data 数据目录]  先把数据库备份为 vinx.db.before-backfill-<时间>，再写入
//
// 先停服务再执行。两种模式都把逐词明细写到数据目录下的 memory-backfill-<时间>[-dryrun].json。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/service"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

func memoryCmd(args []string, stdout, stderr io.Writer) int {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "backfill":
		return memoryBackfill(args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知 memory 子命令 %q，可用：backfill\n", sub)
		return 2
	}
}

func memoryBackfill(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("memory backfill", flag.ContinueOnError)
	fs.SetOutput(stderr)
	data := fs.String("data", "", "数据目录")
	apply := fs.Bool("apply", false, "实际写入（默认只演练）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(config.Options{DataDir: *data})
	if err != nil {
		fmt.Fprintln(stderr, "启动失败：", err)
		return 1
	}
	if _, err := os.Stat(cfg.DBPath); err != nil {
		fmt.Fprintln(stderr, "数据库不存在：", cfg.DBPath)
		return 1
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(stderr, "打开数据库失败：", err)
		return 1
	}
	defer db.Close()
	ctx := context.Background()
	stamp := time.Now().Format("20060102-150405")

	if *apply {
		backup := fmt.Sprintf("%s.before-backfill-%s", cfg.DBPath, stamp)
		if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, backup); err != nil {
			fmt.Fprintln(stderr, "备份失败，未写入：", err)
			return 1
		}
		fmt.Fprintln(stdout, "已备份：", backup)
	}

	report, err := service.BackfillMemory(ctx, db, cfg.Location, *apply)
	if err != nil {
		fmt.Fprintln(stderr, "补算失败（已整体回滚）：", err)
		return 1
	}

	name := "memory-backfill-" + stamp
	if !*apply {
		name += "-dryrun"
	}
	detail := filepath.Join(cfg.DataDir, name+".json")
	b, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(detail, b, 0o600); err != nil {
		fmt.Fprintln(stderr, "写明细失败：", err)
		return 1
	}

	mode := "演练（未写库）"
	if *apply {
		mode = "已写入"
	}
	fmt.Fprintf(stdout, "历史补算 %s\n", mode)
	if len(report.Users) == 0 {
		fmt.Fprintln(stdout, "没有需要补算的作答。")
	}
	for _, u := range report.Users {
		fmt.Fprintf(stdout, "  %s：涉及 %d 个词，写入复习记录 %d 条；新建记忆 %d、更新记忆 %d、答错的生词不建 %d、跳过 %d\n",
			u.Name, u.Words, u.Events, u.Created, u.Updated, u.NoMem, u.Skipped)
	}
	for _, w := range report.Words {
		if w.Skipped != "" {
			fmt.Fprintf(stdout, "  跳过 %s（%s）\n", w.Spelling, w.Skipped)
		}
	}
	fmt.Fprintln(stdout, "逐词明细：", detail)
	return 0
}

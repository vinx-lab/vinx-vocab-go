package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/migrate"
)

// importCmd vinx-vocab import --from-postgres <URL> [--settings-secret <旧密钥>] [--force] [--edition school|personal] [--data 目录]
func importCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var from, secret, edition, data string
	var force bool
	fs.StringVar(&from, "from-postgres", "", "旧版 PostgreSQL 连接串（postgresql://用户@主机:端口/库名）；密码建议用环境变量 PGPASSWORD 或 ~/.pgpass 提供，不写进命令行")
	fs.StringVar(&secret, "settings-secret", "", "旧实例实际生效的设置加密密钥（旧版设了 SETTINGS_SECRET 就用它，没设则用旧的 JWT_SECRET）；也可用环境变量 VINX_IMPORT_SETTINGS_SECRET")
	fs.BoolVar(&force, "force", false, "目标数据库已有数据时，先备份再覆盖")
	fs.StringVar(&edition, "edition", "", "导入后使用的版本：school（默认）或 personal")
	fs.StringVar(&data, "data", "", "数据目录")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "用法：vinx-vocab import --from-postgres <URL> [--settings-secret <旧密钥>] [--force] [--edition school|personal] [--data 数据目录]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "多余的参数：%s\n", strings.Join(fs.Args(), " "))
		fs.Usage()
		return 2
	}
	if from == "" {
		fmt.Fprintln(stderr, "缺少 --from-postgres")
		fs.Usage()
		return 2
	}
	if secret == "" {
		secret = os.Getenv("VINX_IMPORT_SETTINGS_SECRET")
	}
	cfg, secretInfo, err := migrate.LoadTargetConfig(config.Options{DataDir: data})
	if err != nil {
		fmt.Fprintln(stderr, "启动失败：", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Fprintf(stdout, "从 %s 导入到 %s\n", migrate.RedactURL(from), cfg.DBPath)
	res, err := migrate.Import(ctx, cfg, migrate.Options{
		SourceURL:      from,
		SettingsSecret: secret,
		Force:          force,
		Edition:        edition,
		Log:            stdout,
	})
	if res != nil && len(res.Counts) > 0 {
		printCounts(stdout, res)
	}
	if err != nil {
		fmt.Fprintln(stderr, "导入失败：", err)
		if res != nil && res.BackupPath != "" {
			fmt.Fprintf(stderr, "导入前的备份：%s\n", res.BackupPath)
		}
		return 1
	}
	if res.Migration != "" {
		fmt.Fprintf(stdout, "源库迁移版本：%s\n", res.Migration)
	}
	if res.BackupPath != "" {
		fmt.Fprintf(stdout, "导入前的数据库已备份为：%s\n", res.BackupPath)
	}
	fmt.Fprintf(stdout, "AI Key 用数据目录的 %s 里的 SETTINGS_SECRET 重新加密。\n", secretInfo.Source)
	if secretInfo.EnvIgnored {
		fmt.Fprintln(stdout, "注意：当前环境变量 SETTINGS_SECRET 与数据目录里保存的密钥不同，导入已忽略它。启动服务时请不要设置该环境变量，否则 AI Key 无法解密（需要在系统设置里重新填写）。")
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(stdout, "注意：%s\n", w)
	}
	fmt.Fprintln(stdout, "导入完成。旧账号用原密码登录（所有人需要重新登录一次）；发音缓存可把旧部署的 audio 目录复制到数据目录的 audio 下。")
	return 0
}

func printCounts(w io.Writer, res *migrate.Result) {
	fmt.Fprintln(w, "\n行数核对：")
	// 表头手工对齐（中文字符占两列）
	fmt.Fprintln(w, "表                源库      读出    目标库  结果")
	for _, c := range res.Counts {
		mark := "一致"
		if !c.OK() {
			mark = "不一致"
		}
		if c.Expected != c.Source {
			mark += fmt.Sprintf("（含新增 %d 行版本设置）", c.Expected-c.Source)
		}
		fmt.Fprintf(w, "%-13s %8d  %8d  %8d  %s\n", c.Table, c.Source, c.Read, c.Target, mark)
	}
	fmt.Fprintln(w)
}

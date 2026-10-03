package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationSource 迁移文件来源（目录 migrations/ 下的 *.sql）；测试可替换。
var migrationSource fs.FS = migrationsFS

// KeepBackups 迁移前备份保留份数。
const KeepBackups = 3

type migration struct {
	version string // 文件名去掉 .sql，如 0001_init
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationSource, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := fs.ReadFile(migrationSource, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: strings.TrimSuffix(e.Name(), ".sql"), sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// MigrationHook 迁移的 Go 部分：在同一事务里、该迁移的 SQL 执行之后运行（数据迁移需要 Go 逻辑时用，
// 如 0003_sentences 的例句转句子与词形还原关联）。now 为执行时的真实时间。
type MigrationHook func(ctx context.Context, tx *sqlTx, now time.Time) error

var migrationHooks = map[string]MigrationHook{}

// RegisterMigrationHook 给某个迁移（文件名去掉 .sql，如 0003_sentences）登记 Go 部分。
// 由业务包在 init 里登记（store 不依赖业务包）；同一迁移只能登记一次。没有导入登记方的程序只执行 SQL 部分。
func RegisterMigrationHook(version string, hook MigrationHook) {
	if _, dup := migrationHooks[version]; dup {
		panic("迁移钩子重复登记：" + version)
	}
	migrationHooks[version] = hook
}

// Migrate 按序号执行未执行过的迁移，记录在 schema_migrations；每个迁移一个事务（含登记的 Go 钩子）。
func (d *DB) Migrate(ctx context.Context) error {
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT NOT NULL PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("创建 schema_migrations 失败：%w", err)
	}
	all, err := loadMigrations()
	if err != nil {
		return err
	}
	applied := map[string]bool{}
	rows, err := d.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	var pending []migration
	for _, m := range all {
		if !applied[m.version] {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if len(applied) > 0 && d.Path != "" && d.Path != ":memory:" {
		if _, err := d.Backup(ctx, time.Now()); err != nil {
			return fmt.Errorf("迁移前备份失败：%w", err)
		}
	}
	for _, m := range pending {
		err := d.Tx(ctx, func(tx *sqlTx) error {
			if _, err := tx.ExecContext(ctx, m.sql); err != nil {
				return err
			}
			if hook := migrationHooks[m.version]; hook != nil {
				if err := hook(ctx, tx, time.Now()); err != nil {
					return err
				}
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, m.version, FormatTime(time.Now()))
			return err
		})
		if err != nil {
			return fmt.Errorf("执行迁移 %s 失败：%w", m.version, err)
		}
	}
	return nil
}

// Backup 用 VACUUM INTO 把当前库复制为 <库文件>.bak-<时间>，并只保留最近 KeepBackups 份。返回备份文件路径。
func (d *DB) Backup(ctx context.Context, now time.Time) (string, error) {
	target := fmt.Sprintf("%s.bak-%s", d.Path, now.Format("20060102-150405"))
	for i := 1; ; i++ {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		}
		target = fmt.Sprintf("%s.bak-%s-%d", d.Path, now.Format("20060102-150405"), i)
	}
	if _, err := d.ExecContext(ctx, `VACUUM INTO ?`, target); err != nil {
		return "", err
	}
	return target, PruneBackups(d.Path, KeepBackups)
}

// PruneBackups 只保留最近 keep 份 <path>.bak-* 备份（按文件名排序，时间戳格式保证字典序即时间序）。
func PruneBackups(path string, keep int) error {
	matches, err := filepath.Glob(path + ".bak-*")
	if err != nil {
		return err
	}
	sort.Strings(matches)
	for len(matches) > keep {
		if err := os.Remove(matches[0]); err != nil {
			return err
		}
		matches = matches[1:]
	}
	return nil
}

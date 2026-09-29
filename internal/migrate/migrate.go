// Package migrate 从旧版 PostgreSQL 库导入数据（vinx-vocab import --from-postgres）。
//
// 做法：
//   - 源库只读：连接参数 default_transaction_read_only=on，整个读取过程在一个 REPEATABLE READ READ ONLY
//     事务里完成（各表读到的是同一时刻的快照，旧服务不停也能导）。
//   - 目标库一个 SQLite 事务：先清空全部 17 张表（包括首次启动自动导入的内置词书——导入结果必须与源库完全一致，
//     内置词书的 id 与旧库不同，留着会出现重复的系统词书），再按「父表 → 子表」顺序逐表插入；任何一步失败整体回滚。
//   - 每张表按源库的物理顺序（ORDER BY ctid）读取、插入：SQLite 的 rowid 即插入顺序，没有显式排序的查询
//     （例如「单词被哪些单元使用」）与旧库的自然顺序一致。
//   - 类型：时间 → UTC 毫秒 RFC3339 文本；Boolean → 0/1；String[] → JSON 数组文本；Json → JSON 文本（只去掉
//     PostgreSQL 输出的空白，内容、键顺序原样保留，旧快照缺字段也不补）；其余原样。
//   - 列集合必须与目标库完全一致（旧版本的库请先升级旧版到最新再导入），不静默丢列。
//   - AI 设置里加密的 Key：用旧密钥解密、用本实例的 SETTINGS_SECRET 重新加密；旧密钥只在内存里使用，不落盘、不打印。
//     没给旧密钥时其余照常导入，Key 置空并警告（管理员需在系统设置里重新填写）。
//   - 版本：写入 AppSetting "edition"（--edition 指定；否则沿用源库里的值；都没有时为 school），导入后不进首次运行向导。
//   - 完成后逐表核对行数（源库 / 读出 / 目标库），不一致则回滚并返回错误。
package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/secretbox"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// Tables 导入顺序（父表在前）。清空时倒序。
var Tables = []string{
	"User", "Book", "Unit", "Word", "UnitWord", "Classroom", "ClassMember",
	"Plan", "PlanUnit", "PlanTarget", "MemoryState", "WordSheet", "StudySession",
	"Answer", "Passage", "ReviewLog", "AppSetting",
}

const (
	aiSettingKey      = "ai"
	editionSettingKey = "edition"
)

// Options 导入参数。
type Options struct {
	// SourceURL PostgreSQL 连接串（postgresql://user:pass@host:port/db?...）。
	SourceURL string
	// SettingsSecret 旧实例实际生效的加密密钥：旧版设了 SETTINGS_SECRET 就是它，没设时是旧的 JWT_SECRET。
	// 为空时 AI Key 不导入（置空并警告）。
	SettingsSecret string
	// Force 目标库已有数据时，先备份再覆盖。
	Force bool
	// Edition 写入的版本（personal / school）；空串 = 沿用源库的 edition 设置，没有则 school。
	Edition string
	// Now 时钟（默认 time.Now）。
	Now func() time.Time
	// Log 进度输出（默认丢弃）。
	Log io.Writer
}

// TableCount 一张表的对账结果。
type TableCount struct {
	Table    string
	Source   int64 // 源库 count(*)
	Read     int64 // 读出并插入的行数
	Target   int64 // 提交前目标库 count(*)
	Expected int64 // 目标库应有的行数（= Source，AppSetting 可能多一行 edition）
}

// OK 本表是否对上。
func (t TableCount) OK() bool { return t.Source == t.Read && t.Target == t.Expected }

// Result 导入结果。
type Result struct {
	Counts     []TableCount
	BackupPath string   // --force 覆盖前的备份（没有备份时为空）
	Warnings   []string // 需要管理员处理的事项
	Source     string   // 去掉密码的源库地址
	Migration  string   // 源库最后一条 Prisma 迁移名（信息用）
}

// ErrTargetNotEmpty 目标库已有数据且未给 --force。
var ErrTargetNotEmpty = errors.New("目标数据库已有数据（存在账号或自建词书）。确认要用导入的数据覆盖时加 --force（会先备份当前数据库）")

// ErrMismatch 行数核对不一致（已回滚）。
var ErrMismatch = errors.New("行数核对不一致，已回滚，目标数据库保持导入前的状态")

// testHookAfterTable 测试注入：每张表插入完成后调用，返回错误即模拟中途失败。
var testHookAfterTable func(table string, tx *sql.Tx) error

// RedactURL 去掉连接串里的密码（用于打印）。
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "（无法解析的连接串）"
	}
	q := u.Query()
	if q.Has("password") {
		q.Set("password", "xxx")
		u.RawQuery = q.Encode()
	}
	pw := false
	if u.User != nil {
		if _, pw = u.User.Password(); pw {
			u.User = url.UserPassword(u.User.Username(), "xxx")
		}
	}
	s := u.String()
	if pw {
		s = strings.Replace(s, ":xxx@", ":***@", 1)
	}
	return strings.Replace(s, "password=xxx", "password=***", 1)
}

func (o *Options) logf(format string, args ...any) {
	if o.Log != nil {
		fmt.Fprintf(o.Log, format, args...)
	}
}

// connectSource 只读连接源库。
func connectSource(ctx context.Context, raw string) (*pgx.Conn, error) {
	// Prisma 风格的 ?schema=xxx 不是 PostgreSQL 参数（原样发给服务器会报错）：去掉，改设 search_path
	schema := ""
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		q := u.Query()
		if q.Has("schema") {
			schema = q.Get("schema")
			q.Del("schema")
			u.RawQuery = q.Encode()
			raw = u.String()
		}
	}
	cfg, err := pgx.ParseConfig(raw)
	if err != nil {
		// pgx 的解析错误可能带出原串，这里不回显
		return nil, fmt.Errorf("源库连接串无效（应为 postgresql://用户:密码@主机:端口/库名）")
	}
	if cfg.ConnectTimeout == 0 || cfg.ConnectTimeout > 15*time.Second {
		cfg.ConnectTimeout = 15 * time.Second
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.RuntimeParams["application_name"] = "vinx-vocab-import"
	if schema != "" {
		cfg.RuntimeParams["search_path"] = schema
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("连接源库失败：%s", pgErrText(err))
	}
	return conn, nil
}

// pgErrText 错误说明（pgconn 的错误不含密码）。
func pgErrText(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return fmt.Sprintf("%s（%s）", pe.Message, pe.Code)
	}
	return err.Error()
}

type column struct {
	name string
	udt  string // PostgreSQL udt_name：text / int4 / float8 / bool / timestamp / jsonb / _text …
}

// sourceColumns 源库某张表的列（当前 schema）。
func sourceColumns(ctx context.Context, tx pgx.Tx, table string) ([]column, error) {
	rows, err := tx.Query(ctx, `SELECT column_name, udt_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1 ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.udt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// targetColumn 目标库的列：是否必填（NOT NULL 且没有默认值）。
type targetColumn struct {
	name     string
	required bool
}

// targetColumns 目标库某张表的列。
func targetColumns(ctx context.Context, tx *sql.Tx, table string) ([]targetColumn, error) {
	rows, err := tx.QueryContext(ctx, `SELECT "name", "notnull", "dflt_value" IS NOT NULL, "pk" FROM pragma_table_info(?) ORDER BY "cid"`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []targetColumn
	for rows.Next() {
		var c targetColumn
		var notNull, hasDefault bool
		var pk int
		if err := rows.Scan(&c.name, &notNull, &hasDefault, &pk); err != nil {
			return nil, err
		}
		c.required = (notNull || pk > 0) && !hasDefault
		out = append(out, c)
	}
	return out, rows.Err()
}

func quote(id string) string { return `"` + strings.ReplaceAll(id, `"`, `""`) + `"` }

// selectExpr 源库取值表达式：Json 取文本，数组转 JSON 文本，其余原样。
func selectExpr(c column) (string, error) {
	q := quote(c.name)
	switch c.udt {
	case "json", "jsonb":
		return q + "::text", nil
	case "_text", "_varchar":
		return "to_json(" + q + ")::text", nil
	case "text", "varchar", "bpchar", "int2", "int4", "int8", "float4", "float8", "bool", "timestamp", "timestamptz":
		return q, nil
	}
	return "", fmt.Errorf("不支持的列类型 %s", c.udt)
}

// convert 把 pgx 读出的值转成 SQLite 存储形式。
func convert(c column, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch c.udt {
	case "json", "jsonb", "_text", "_varchar":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("意外的值类型 %T", v)
		}
		var b strings.Builder
		if err := compactJSON(&b, s); err != nil {
			return nil, err
		}
		return b.String(), nil
	case "bool":
		if b, ok := v.(bool); ok {
			if b {
				return int64(1), nil
			}
			return int64(0), nil
		}
	case "timestamp", "timestamptz":
		if t, ok := v.(time.Time); ok {
			return store.FormatTime(t), nil
		}
	case "int2":
		if n, ok := v.(int16); ok {
			return int64(n), nil
		}
	case "int4":
		if n, ok := v.(int32); ok {
			return int64(n), nil
		}
	case "int8":
		if n, ok := v.(int64); ok {
			return n, nil
		}
	case "float4":
		if f, ok := v.(float32); ok {
			return float64(f), nil
		}
	case "float8":
		if f, ok := v.(float64); ok {
			return f, nil
		}
	case "text", "varchar", "bpchar":
		if s, ok := v.(string); ok {
			return s, nil
		}
	}
	return nil, fmt.Errorf("列类型 %s 读出意外的值类型 %T", c.udt, v)
}

func compactJSON(w *strings.Builder, s string) error {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		return fmt.Errorf("JSON 无法解析：%w", err)
	}
	w.Write(buf.Bytes())
	return nil
}

// targetState 目标库是否已有数据：有账号，或有不是内置自动导入的词书。
func targetHasData(ctx context.Context, db *store.DB) (bool, error) {
	var users, books int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM "User"`).Scan(&users); err != nil {
		return false, err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM "Book" WHERE "isSystem" = 0 OR "ownerId" IS NOT NULL`).Scan(&books); err != nil {
		return false, err
	}
	return users > 0 || books > 0, nil
}

// backup VACUUM INTO 备份到 <库>.before-import-<时间>（不参与迁移备份的轮换清理）。
func backup(ctx context.Context, db *store.DB, now time.Time) (string, error) {
	base := fmt.Sprintf("%s.before-import-%s", db.Path, now.Format("20060102-150405"))
	target := base
	for i := 1; ; i++ {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		}
		target = fmt.Sprintf("%s-%d", base, i)
	}
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, target); err != nil {
		return "", fmt.Errorf("备份当前数据库失败：%w", err)
	}
	return target, nil
}

// Import 执行导入。cfg 为本实例的配置（数据目录、SETTINGS_SECRET）；须与之后启动服务时的配置相同。
func Import(ctx context.Context, cfg *config.Config, o Options) (*Result, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Edition != "" && !core.IsEdition(o.Edition) {
		return nil, fmt.Errorf("--edition 只能是 personal 或 school")
	}
	res := &Result{Source: RedactURL(o.SourceURL)}

	// 1. 先连源库：连不上时不碰目标库
	conn, err := connectSource(ctx, o.SourceURL)
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())
	src, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("开启源库只读事务失败：%s", pgErrText(err))
	}
	defer src.Rollback(context.Background())

	// 源库结构检查：17 张表都在、列集合与目标一致
	srcCols := map[string][]column{}
	for _, t := range Tables {
		cols, err := sourceColumns(ctx, src, t)
		if err != nil {
			return nil, fmt.Errorf("读取源库表结构失败：%s", pgErrText(err))
		}
		if len(cols) == 0 {
			return nil, fmt.Errorf("源库里没有表 %q：这不是 Vinx Vocab 的数据库，或连接串的库名 / schema 不对", t)
		}
		srcCols[t] = cols
	}
	var hasMig bool
	if err := src.QueryRow(ctx, `SELECT to_regclass('_prisma_migrations') IS NOT NULL`).Scan(&hasMig); err != nil {
		return nil, fmt.Errorf("源库查询失败：%s", pgErrText(err))
	}
	if hasMig {
		var mig *string
		if err := src.QueryRow(ctx, `SELECT max(migration_name) FROM _prisma_migrations WHERE finished_at IS NOT NULL`).Scan(&mig); err != nil {
			return nil, fmt.Errorf("源库查询失败：%s", pgErrText(err))
		}
		if mig != nil {
			res.Migration = *mig
		}
	}

	// 2. 目标库
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	nonEmpty, err := targetHasData(ctx, db)
	if err != nil {
		return nil, err
	}
	if nonEmpty {
		if !o.Force {
			return nil, ErrTargetNotEmpty
		}
		if res.BackupPath, err = backup(ctx, db, o.Now()); err != nil {
			return nil, err
		}
		o.logf("已备份当前数据库：%s\n", res.BackupPath)
	}

	now := o.Now()
	err = db.Tx(ctx, func(tx *sql.Tx) error {
		// 列集合核对
		plans := map[string][]column{}
		for _, t := range Tables {
			tcols, err := targetColumns(ctx, tx, t)
			if err != nil {
				return err
			}
			scols := srcCols[t]
			if err := sameColumns(t, scols, tcols); err != nil {
				return err
			}
			plans[t] = scols
		}
		// 清空（子表先删）
		for i := len(Tables) - 1; i >= 0; i-- {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+quote(Tables[i])); err != nil {
				return fmt.Errorf("清空 %s 失败：%w", Tables[i], err)
			}
		}
		for _, t := range Tables {
			tc, err := copyTable(ctx, src, tx, t, plans[t], &o, cfg.SettingsSecret, res)
			if err != nil {
				return err
			}
			res.Counts = append(res.Counts, tc)
			o.logf("  %-13s %d 行\n", t, tc.Read)
			if testHookAfterTable != nil {
				if err := testHookAfterTable(t, tx); err != nil {
					return err
				}
			}
		}
		// 版本
		added, err := writeEdition(ctx, tx, o.Edition, now)
		if err != nil {
			return err
		}
		// 对账
		mismatch := false
		for i := range res.Counts {
			c := &res.Counts[i]
			c.Expected = c.Source
			if c.Table == "AppSetting" && added {
				c.Expected++
			}
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+quote(c.Table)).Scan(&c.Target); err != nil {
				return err
			}
			if !c.OK() {
				mismatch = true
			}
		}
		if err := foreignKeyCheck(ctx, tx); err != nil {
			return err
		}
		if mismatch {
			return ErrMismatch
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrMismatch) {
			return res, err
		}
		return res, fmt.Errorf("导入失败，已回滚（目标数据库保持导入前的状态）：%w", err)
	}
	return res, nil
}

// sameColumns 源库的每一列目标库都要有（否则会丢数据）；目标库多出的列（本版本新增）只要可空或有默认值就允许，
// 导入时取默认值；目标库多出的必填列无法填充，拒绝。
func sameColumns(table string, src []column, dst []targetColumn) error {
	s := map[string]bool{}
	for _, c := range src {
		s[c.name] = true
	}
	d := map[string]bool{}
	for _, c := range dst {
		d[c.name] = true
	}
	var extra, missing []string
	for n := range s {
		if !d[n] {
			extra = append(extra, n)
		}
	}
	for _, c := range dst {
		if !s[c.name] && c.required {
			missing = append(missing, c.name)
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	if len(extra) > 0 || len(missing) > 0 {
		msg := fmt.Sprintf("表 %s 的列与本程序不兼容", table)
		if len(extra) > 0 {
			msg += fmt.Sprintf("；源库有本程序不认识的列 %s（源库可能来自比本程序更新的旧版，为避免丢数据停止导入，请换用更新版本的本程序）", strings.Join(extra, ", "))
		}
		if len(missing) > 0 {
			msg += fmt.Sprintf("；源库缺少必填列 %s（源库不是 Vinx Vocab 旧版的完整数据库，或版本过老：旧版需先完成它自己的数据库迁移）", strings.Join(missing, ", "))
		}
		return errors.New(msg)
	}
	return nil
}

// copyTable 按 ctid 顺序读出一张表并插入目标库。
func copyTable(ctx context.Context, src pgx.Tx, tx *sql.Tx, table string, cols []column, o *Options, newSecret string, res *Result) (TableCount, error) {
	tc := TableCount{Table: table}
	if err := src.QueryRow(ctx, `SELECT count(*) FROM `+quote(table)).Scan(&tc.Source); err != nil {
		return tc, fmt.Errorf("统计源表 %s 失败：%s", table, pgErrText(err))
	}
	exprs := make([]string, len(cols))
	names := make([]string, len(cols))
	for i, c := range cols {
		e, err := selectExpr(c)
		if err != nil {
			return tc, fmt.Errorf("表 %s 列 %s：%w", table, c.name, err)
		}
		exprs[i] = e
		names[i] = quote(c.name)
	}
	rows, err := src.Query(ctx, `SELECT `+strings.Join(exprs, ", ")+` FROM `+quote(table)+` ORDER BY ctid`)
	if err != nil {
		return tc, fmt.Errorf("读取源表 %s 失败：%s", table, pgErrText(err))
	}
	defer rows.Close()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO `+quote(table)+` (`+strings.Join(names, ", ")+`) VALUES (`+store.Placeholders(len(cols))+`)`)
	if err != nil {
		return tc, err
	}
	defer stmt.Close()
	keyIdx, valIdx := -1, -1
	if table == "AppSetting" {
		for i, c := range cols {
			switch c.name {
			case "key":
				keyIdx = i
			case "value":
				valIdx = i
			}
		}
	}
	args := make([]any, len(cols))
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return tc, fmt.Errorf("读取源表 %s 失败：%s", table, pgErrText(err))
		}
		for i, c := range cols {
			v, err := convert(c, vals[i])
			if err != nil {
				return tc, fmt.Errorf("表 %s 第 %d 行列 %s：%w", table, tc.Read+1, c.name, err)
			}
			args[i] = v
		}
		if keyIdx >= 0 && valIdx >= 0 {
			if k, _ := args[keyIdx].(string); k == aiSettingKey {
				v, _ := args[valIdx].(string)
				nv, warn, err := reencryptAI(v, o.SettingsSecret, newSecret)
				if err != nil {
					return tc, err
				}
				if warn != "" {
					res.Warnings = append(res.Warnings, warn)
				}
				args[valIdx] = nv
			}
		}
		if _, err := stmt.ExecContext(ctx, args...); err != nil {
			return tc, fmt.Errorf("写入 %s 第 %d 行失败：%w", table, tc.Read+1, err)
		}
		tc.Read++
	}
	if err := rows.Err(); err != nil {
		return tc, fmt.Errorf("读取源表 %s 失败：%s", table, pgErrText(err))
	}
	return tc, nil
}

// writeEdition 写入版本设置。返回是否新增了一行。
func writeEdition(ctx context.Context, tx *sql.Tx, edition string, now time.Time) (bool, error) {
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT "value" FROM "AppSetting" WHERE "key" = ?`, editionSettingKey).Scan(&existing)
	exists := err == nil
	if err != nil && !store.IsNoRows(err) {
		return false, err
	}
	if exists && edition == "" {
		var v string
		if json.Unmarshal([]byte(existing), &v) == nil && core.IsEdition(v) {
			return false, nil
		}
		edition = core.EditionSchool
	}
	if edition == "" {
		edition = core.EditionSchool
	}
	raw, _ := json.Marshal(edition)
	_, err = tx.ExecContext(ctx, `INSERT INTO "AppSetting" ("key","value","updatedById","updatedAt") VALUES (?,?,NULL,?)
		ON CONFLICT("key") DO UPDATE SET "value"=excluded."value","updatedAt"=excluded."updatedAt"`,
		editionSettingKey, string(raw), store.FormatTime(now))
	return !exists, err
}

func foreignKeyCheck(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowid sql.NullInt64
		var parent string
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		return fmt.Errorf("外键检查失败：表 %s 引用了不存在的 %s", table, parent)
	}
	return rows.Err()
}

// reencryptAI 处理 AI 设置里的加密 Key：旧密钥解密 → 新密钥加密。
// 没给旧密钥：Key 置空并返回警告。给了但解不开：返回错误（多半是密钥给错了），整体回滚。
func reencryptAI(value, oldSecret, newSecret string) (string, string, error) {
	var v map[string]any
	dec := json.NewDecoder(strings.NewReader(value))
	dec.UseNumber() // 数字原样写回
	if err := dec.Decode(&v); err != nil || v == nil {
		return value, "", nil
	}
	enc, _ := v["apiKeyEnc"].(string)
	if enc == "" {
		return value, "", nil
	}
	if oldSecret == "" {
		v["apiKeyEnc"] = nil
		out, err := marshalJSON(v)
		return out, "源库的 AI 设置里保存了 API Key，但没有提供 --settings-secret：其余设置已导入，Key 未导入，请管理员在「系统设置 → AI」里重新填写。", err
	}
	plain, ok := secretbox.Decrypt(enc, oldSecret)
	if !ok {
		if t := strings.TrimSpace(oldSecret); t != oldSecret {
			plain, ok = secretbox.Decrypt(enc, t)
		}
	}
	if !ok {
		return "", "", errors.New("用 --settings-secret 解不开源库保存的 AI Key：请确认给的是旧实例实际生效的密钥（旧版设了 SETTINGS_SECRET 就用它；没设则是旧的 JWT_SECRET）。如果不需要保留 Key，去掉 --settings-secret 重新导入")
	}
	if newSecret == "" {
		return "", "", errors.New("本实例没有可用的 SETTINGS_SECRET")
	}
	sealed, err := secretbox.Encrypt(plain, newSecret)
	if err != nil {
		return "", "", err
	}
	v["apiKeyEnc"] = sealed
	out, err := marshalJSON(v)
	return out, "", err
}

func marshalJSON(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

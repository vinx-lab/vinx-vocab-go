package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// EditionSettingKey AppSetting 里保存所选版本的键（值为 JSON 字符串 "personal" / "school"）。
const EditionSettingKey = "edition"

// EditionState 当前版本及其来源。
type EditionState struct {
	Edition core.Edition
	// Locked 由 VINX_EDITION（环境变量或 config.toml）指定，界面上不可切换。
	Locked bool
	// Chosen 未锁定时，是否已在 AppSetting 里保存过选择（首次运行向导据此判断 needsSetup）。
	Chosen bool
}

// Features 当前版本的功能开关。
func (s EditionState) Features() core.Features { return core.FeaturesOf(s.Edition) }

// Editions 按请求判定版本（不在启动时固定，支持运行时切换：SwitchEdition）。
// 优先级：VINX_EDITION > AppSetting["edition"] > 默认 school。
type Editions struct {
	DB  store.Querier
	Cfg *config.Config
}

// Current 读取当前版本；每次调用都查库（单行主键查询，开销可忽略），切换后下一个请求即生效。
func (e *Editions) Current(ctx context.Context) (EditionState, error) {
	if e.Cfg != nil && core.IsEdition(e.Cfg.Edition) {
		return EditionState{Edition: e.Cfg.Edition, Locked: true, Chosen: true}, nil
	}
	var raw string
	err := e.DB.QueryRowContext(ctx, `SELECT "value" FROM "AppSetting" WHERE "key" = ?`, EditionSettingKey).Scan(&raw)
	if store.IsNoRows(err) {
		return EditionState{Edition: core.EditionSchool}, nil
	}
	if err != nil {
		return EditionState{}, err
	}
	var v string
	if json.Unmarshal([]byte(raw), &v) != nil || !core.IsEdition(v) {
		return EditionState{Edition: core.EditionSchool}, nil
	}
	return EditionState{Edition: v, Chosen: true}, nil
}

// NeedsSetup 是否进入首次运行向导：未锁定、未保存过选择，且库里还没有任何账号。
// 已有账号的库（导入的旧数据、seed-demo）不进向导，未保存选择时按班级版运行（旧版默认值），
// 管理员可在系统设置里切换。
func NeedsSetup(ctx context.Context, q store.Querier, st EditionState) (bool, error) {
	if st.Locked || st.Chosen {
		return false, nil
	}
	n, err := CountUsers(ctx, q)
	return n == 0, err
}

func editionValue(edition core.Edition) string {
	raw, _ := json.Marshal(edition)
	return string(raw)
}

// SetupEdition 首次运行向导保存所选版本（无需登录）。事务内重新判断，已完成设置 / 已锁定 / 已有账号时拒绝。
func SetupEdition(ctx context.Context, db *store.DB, eds *Editions, now time.Time, edition core.Edition) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		st, err := (&Editions{DB: tx, Cfg: eds.Cfg}).Current(ctx)
		if err != nil {
			return err
		}
		if st.Locked {
			return httpx.Forbidden(EditionLockedMessage)
		}
		need, err := NeedsSetup(ctx, tx, st)
		if err != nil {
			return err
		}
		if !need {
			return httpx.Forbidden("已完成初始设置，如需切换版本请由管理员在系统设置里操作")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO "AppSetting" ("key","value","updatedById","updatedAt") VALUES (?,?,NULL,?)
			ON CONFLICT("key") DO UPDATE SET "value"=excluded."value","updatedById"=NULL,"updatedAt"=excluded."updatedAt"`,
			EditionSettingKey, editionValue(edition), store.NewTime(now))
		return err
	})
}

// EditionLockedMessage 设了 VINX_EDITION 时不能在界面上选择 / 切换版本。
const EditionLockedMessage = "版本由 VINX_EDITION 指定，不能在界面上切换"

// SwitchEdition 管理员切换版本（双向）。数据不删：降级只按请求隐藏功能与收窄数据范围，升级原样恢复。
func SwitchEdition(ctx context.Context, db *store.DB, eds *Editions, now time.Time, edition core.Edition, actorID string) error {
	if eds.Cfg != nil && core.IsEdition(eds.Cfg.Edition) {
		return httpx.Forbidden(EditionLockedMessage)
	}
	return db.Tx(ctx, func(tx *sql.Tx) error {
		return upsertAppSetting(ctx, tx, now, EditionSettingKey, editionValue(edition), actorID)
	})
}

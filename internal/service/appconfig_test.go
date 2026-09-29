package service

import (
	"context"
	"testing"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/core"
)

// TestSignupEnabled_IgnoresDisabledFlagOnFreshInstall 全新安装（库里还没有任何账号）时，
// 即使 SIGNUP_ENABLED=false，也必须能注册出第一个管理员（K44）。
func TestSignupEnabled_IgnoresDisabledFlagOnFreshInstall(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()

	for _, edition := range []core.Edition{core.EditionSchool, core.EditionPersonal} {
		cfg := &config.Config{SignupEnabled: false}
		ed := EditionState{Edition: edition, Locked: true, Chosen: true}
		ok, err := SignupEnabled(ctx, db, cfg, ed)
		if err != nil {
			t.Fatalf("edition=%s: %v", edition, err)
		}
		if !ok {
			t.Errorf("edition=%s: 全新安装时 SignupEnabled 应为 true（忽略 SIGNUP_ENABLED=false），got false", edition)
		}
	}
}

// TestSignupEnabled_RespectsFlagAfterFirstUser 有账号之后，SIGNUP_ENABLED=false 照常生效；
// 个人版有账号后固定关闭（与 SIGNUP_ENABLED 无关）。
func TestSignupEnabled_RespectsFlagAfterFirstUser(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()

	if _, err := CreateUser(ctx, db, time.Now(), NewUser{Email: "a@vinx.test", Name: "a", Role: core.RoleAdmin, PasswordHash: "x"}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		edition core.Edition
		enabled bool
		want    bool
	}{
		{"school disabled", core.EditionSchool, false, false},
		{"school enabled", core.EditionSchool, true, true},
		{"personal enabled but already has a user", core.EditionPersonal, true, false},
		{"personal disabled", core.EditionPersonal, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{SignupEnabled: tc.enabled}
			ed := EditionState{Edition: tc.edition, Locked: true, Chosen: true}
			ok, err := SignupEnabled(ctx, db, cfg, ed)
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.want {
				t.Errorf("got %v, want %v", ok, tc.want)
			}
		})
	}
}

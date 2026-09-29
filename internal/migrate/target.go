package migrate

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
)

// SecretInfo 重新加密 AI Key 用的密钥来自哪里。
type SecretInfo struct {
	Source     string // "config.toml" 或 "secret.key"
	EnvIgnored bool   // 导入时设了环境变量 SETTINGS_SECRET 且与数据目录里保存的不同：已忽略
}

// LoadTargetConfig 导入用的本实例配置。
//
// 重新加密 AI Key 的密钥只取数据目录里持久保存的值（config.toml 的 SETTINGS_SECRET，没有则 secret.key），
// 不取环境变量：导入所在 shell 里临时 export 的 SETTINGS_SECRET（例如误把旧密钥 export 了）之后启动服务时
// 多半不在，用它加密会导致服务解不开 Key。只要服务不额外设 SETTINGS_SECRET 环境变量，就一定能解开。
func LoadTargetConfig(o config.Options) (*config.Config, SecretInfo, error) {
	lookup := o.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	cfg, err := config.Load(o)
	if err != nil {
		return nil, SecretInfo{}, err
	}
	p := o
	p.DataDir = cfg.DataDir
	p.Lookup = func(k string) (string, bool) {
		if k == "SETTINGS_SECRET" {
			return "", false
		}
		return lookup(k)
	}
	persisted, err := config.Load(p)
	if err != nil {
		return nil, SecretInfo{}, err
	}
	info := SecretInfo{Source: "config.toml"}
	if keys, err := config.LoadOrCreateSecrets(filepath.Join(cfg.DataDir, "secret.key")); err == nil && strings.TrimSpace(keys.SettingsSecret) == persisted.SettingsSecret {
		info.Source = "secret.key"
	}
	if v, ok := lookup("SETTINGS_SECRET"); ok && strings.TrimSpace(v) != "" && strings.TrimSpace(v) != persisted.SettingsSecret {
		info.EnvIgnored = true
	}
	cfg.SettingsSecret = persisted.SettingsSecret
	return cfg, info, nil
}

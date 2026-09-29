package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Secrets secret.key 的内容（TOML）。
type Secrets struct {
	JWTSecret      string `toml:"jwt_secret"`
	SettingsSecret string `toml:"settings_secret"`
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// LoadOrCreateSecrets 读取 secret.key；不存在或缺项时随机生成补齐并写回（权限 0600）。
func LoadOrCreateSecrets(path string) (Secrets, error) {
	var s Secrets
	existed := true
	if _, err := toml.DecodeFile(path, &s); errors.Is(err, os.ErrNotExist) {
		existed = false
	} else if err != nil {
		return s, fmt.Errorf("读取 %s 失败：%w（删除该文件会生成新密钥，所有人需要重新登录，已保存的 AI Key 需要重新填写）", path, err)
	}
	changed := false
	if strings.TrimSpace(s.JWTSecret) == "" {
		v, err := randomHex(32)
		if err != nil {
			return s, err
		}
		s.JWTSecret, changed = v, true
	}
	if strings.TrimSpace(s.SettingsSecret) == "" {
		v, err := randomHex(32)
		if err != nil {
			return s, err
		}
		s.SettingsSecret, changed = v, true
	}
	if changed {
		content := "# Vinx Vocab 密钥（首次启动自动生成）。请妥善保管，不要分享。\n" +
			"# jwt_secret：登录令牌签名；更换后所有人需要重新登录。\n" +
			"# settings_secret：加密页面保存的 AI Key；更换后需要重新填写 AI Key。\n" +
			fmt.Sprintf("jwt_secret = %q\nsettings_secret = %q\n", s.JWTSecret, s.SettingsSecret)
		// 原子写：先写临时文件；原先没有 secret.key 时用硬链接「不存在才创建」，别的进程抢先生成（首次并发启动）
		// 则以它为准重新读取；原先有文件但缺项时改名覆盖。写到一半断电不会留下截断的密钥。
		tmp, err := writeTemp(filepath.Dir(path), content)
		if err != nil {
			return s, fmt.Errorf("写入 %s 失败：%w", path, err)
		}
		defer os.Remove(tmp)
		if !existed {
			err := os.Link(tmp, path)
			if err == nil {
				return s, nil
			}
			if errors.Is(err, os.ErrExist) {
				var again Secrets
				if _, err := toml.DecodeFile(path, &again); err == nil && again.JWTSecret != "" && again.SettingsSecret != "" {
					return again, nil
				}
			}
			// 不支持硬链接的文件系统：退回改名
		}
		if err := os.Rename(tmp, path); err != nil {
			return s, fmt.Errorf("写入 %s 失败：%w", path, err)
		}
	}
	return s, nil
}

func writeTemp(dir, content string) (string, error) {
	f, err := os.CreateTemp(dir, ".secret.key-*")
	if err != nil {
		return "", err
	}
	_, werr := f.WriteString(content)
	serr := f.Sync()
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr, os.Chmod(f.Name(), 0o600)); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

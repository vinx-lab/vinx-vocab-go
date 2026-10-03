package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestDefaultDataDir(t *testing.T) {
	if got := DefaultDataDir("windows", filepath.Join("C:", "apps", "vinx-vocab.exe")); got != filepath.Join("C:", "apps", "vinx-data") {
		t.Errorf("windows = %s", got)
	}
	if got := DefaultDataDir("linux", "/usr/bin/vinx-vocab"); got != "data" {
		t.Errorf("linux = %s", got)
	}
}

// 开发模式只认命令行参数（Options.Dev），不读环境变量或 config.toml。
func TestDevOnlyFromOptions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("DEV = true\nVINX_DEV = true\n"), 0o600)
	c, err := Load(Options{DataDir: dir, Lookup: env(map[string]string{"DEV": "true", "VINX_DEV": "1"})})
	if err != nil || c.Dev {
		t.Fatalf("环境变量 / config.toml 不应打开开发模式：%v %v", c != nil && c.Dev, err)
	}
	c, err = Load(Options{DataDir: dir, Dev: true, Lookup: env(nil)})
	if err != nil || !c.Dev {
		t.Fatalf("Options.Dev 应打开开发模式：%v %v", c != nil && c.Dev, err)
	}
}

func TestLoadDefaultsAndSecretGeneration(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "data")
	c, err := Load(Options{DataDir: dir, Lookup: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != dir || c.DBPath != filepath.Join(dir, "vinx.db") || c.Port != 3000 || c.Host != "0.0.0.0" {
		t.Errorf("defaults = %+v", c)
	}
	if c.Edition != "" || c.AppTimezone != "Asia/Shanghai" || c.JWTExpiresIn != 7*24*time.Hour || c.CookieSecure != "auto" || !c.SignupEnabled {
		t.Errorf("defaults = %+v", c)
	}
	if c.AudioDir != filepath.Join(dir, "audio") || !strings.Contains(c.AudioProviderURL, "{word}") || c.AITimeoutMs != 180000 || c.AIModel != "qwen2.5:7b-instruct" {
		t.Errorf("defaults = %+v", c)
	}
	if len(c.JWTSecret) != 64 || len(c.SettingsSecret) != 64 || c.JWTSecret == c.SettingsSecret {
		t.Errorf("secrets = %q %q", c.JWTSecret, c.SettingsSecret)
	}
	st, err := os.Stat(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("secret.key mode = %v", st.Mode())
	}
	// 再次加载沿用同一密钥
	c2, err := Load(Options{DataDir: dir, Lookup: env(nil)})
	if err != nil || c2.JWTSecret != c.JWTSecret || c2.SettingsSecret != c.SettingsSecret {
		t.Fatalf("第二次加载密钥变化：%v", err)
	}
}

func TestPrecedence(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("VINX_EDITION = \"personal\"\nAPP_TIMEZONE = \"America/New_York\"\nPORT = 3100\nSIGNUP_ENABLED = false\nALLOWED_ORIGINS = [\"http://a\", \"http://b\"]\nJWT_SECRET = \"from-file\"\n"), 0o644)
	c, err := Load(Options{DataDir: dir, Lookup: env(map[string]string{"APP_TIMEZONE": "UTC", "SETTINGS_SECRET": "env-settings", "AUDIO_PROVIDER_URL": ""})})
	if err != nil {
		t.Fatal(err)
	}
	if c.Edition != "personal" || c.AppTimezone != "UTC" || c.Port != 3100 || c.SignupEnabled || c.JWTSecret != "from-file" || c.SettingsSecret != "env-settings" {
		t.Errorf("precedence = %+v", c)
	}
	if c.AudioProviderURL != "" {
		t.Error("环境变量显式置空应关闭发音")
	}
	if len(c.AllowedOrigins) != 2 {
		t.Errorf("origins = %v", c.AllowedOrigins)
	}
	// 两个密钥都已给出：不生成 secret.key
	if _, err := os.Stat(filepath.Join(dir, "secret.key")); !os.IsNotExist(err) {
		t.Error("两个密钥都已给出时不应生成 secret.key")
	}
	// 命令行参数优先
	c, _ = Load(Options{DataDir: dir, Port: 3200, Host: "127.0.0.1", Lookup: env(nil)})
	if c.Port != 3200 || c.Host != "127.0.0.1" {
		t.Errorf("flags = %+v", c)
	}
}

func TestInvalidValues(t *testing.T) {
	dir := t.TempDir()
	for k, v := range map[string]string{"VINX_EDITION": "enterprise", "APP_TIMEZONE": "Nowhere/City", "AI_TIMEOUT_MS": "100", "COOKIE_SECURE": "yes", "PORT": "abc"} {
		if _, err := Load(Options{DataDir: dir, Lookup: env(map[string]string{k: v})}); err == nil {
			t.Errorf("%s=%s 应报错", k, v)
		}
	}
}

func TestDataDirNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("需要非 root 的类 Unix 环境模拟不可写目录")
	}
	parent := t.TempDir()
	os.Chmod(parent, 0o555)
	defer os.Chmod(parent, 0o755)
	_, err := Load(Options{DataDir: filepath.Join(parent, "data"), Lookup: env(nil)})
	if err == nil || !strings.Contains(err.Error(), "--data") {
		t.Fatalf("err = %v", err)
	}
	ro := filepath.Join(t.TempDir(), "ro")
	os.Mkdir(ro, 0o555)
	defer os.Chmod(ro, 0o755)
	_, err = Load(Options{DataDir: ro, Lookup: env(nil)})
	if err == nil || !strings.Contains(err.Error(), "不可写") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{"7d": 7 * 24 * time.Hour, "12h": 12 * time.Hour, "30m": 30 * time.Minute, "60s": time.Minute, "1500": 1500 * time.Millisecond, "2 days": 48 * time.Hour, "1w": 7 * 24 * time.Hour}
	for in, want := range cases {
		if got, err := ParseDuration(in); err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "abc", "-1d"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("ParseDuration(%q) 应报错", bad)
		}
	}
}

// M4：首次并发启动时各进程拿到同一份密钥，且不留临时文件。
func TestSecretsConcurrentCreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.key")
	const n = 8
	results := make(chan Secrets, n)
	for i := 0; i < n; i++ {
		go func() {
			s, err := LoadOrCreateSecrets(path)
			if err != nil {
				t.Error(err)
			}
			results <- s
		}()
	}
	first := <-results
	for i := 1; i < n; i++ {
		if s := <-results; s != first {
			t.Fatalf("并发生成的密钥不一致：%v vs %v", s, first)
		}
	}
	onDisk, _ := LoadOrCreateSecrets(path)
	if onDisk != first {
		t.Fatal("磁盘上的密钥与返回值不一致")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("残留文件：%v", entries)
	}
	// 缺项时补齐并保留已有项
	os.WriteFile(path, []byte("jwt_secret = \"keep\"\n"), 0o600)
	s, err := LoadOrCreateSecrets(path)
	if err != nil || s.JWTSecret != "keep" || len(s.SettingsSecret) != 64 {
		t.Fatalf("补齐 = %+v %v", s, err)
	}
	again, _ := LoadOrCreateSecrets(path)
	if again != s {
		t.Fatal("补齐后未写回")
	}
}

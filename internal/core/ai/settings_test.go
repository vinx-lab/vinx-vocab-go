package ai

import (
	"strings"
	"testing"
)

var baseEnv = EnvInput{Provider: "auto", APIKey: "", BaseURL: "", Model: "qwen2.5:7b-instruct", TimeoutMs: 180_000}

func decryptOk(enc string) (string, bool) { return strings.TrimPrefix(enc, "enc:"), true }
func decryptFail(string) (string, bool)   { return "", false }

func TestConfigFromEnv(t *testing.T) {
	e := baseEnv
	e.BaseURL = "http://x/v1"
	if c := ConfigFromEnv(e); c.Provider != ProviderOpenAI || c.Selected != SettingOpenAI || c.Source != "env" {
		t.Fatalf("openai = %+v", c)
	}
	e2 := baseEnv
	e2.APIKey = "sk-ant"
	if c := ConfigFromEnv(e2); c.Provider != ProviderAnthropic || c.Selected != SettingAnthropic {
		t.Fatalf("anthropic = %+v", c)
	}
	if c := ConfigFromEnv(baseEnv); c.Provider != ProviderNone || c.Selected != SettingOff {
		t.Fatalf("off = %+v", c)
	}
}

func TestConfigFromEnvMissingFields(t *testing.T) {
	e := baseEnv
	e.Provider = "openai"
	if c := ConfigFromEnv(e); c.Provider != ProviderNone {
		t.Fatalf("openai w/o baseUrl = %+v", c)
	}
	e2 := baseEnv
	e2.Provider = "anthropic"
	e2.BaseURL = "http://x"
	if c := ConfigFromEnv(e2); c.Provider != ProviderNone {
		t.Fatalf("anthropic w/o key = %+v", c)
	}
}

func storedFixture() StoredSetting {
	bu, enc := "http://db/v1", "enc:sk-db-key-1234"
	return StoredSetting{Provider: SettingOpenAI, BaseURL: &bu, APIKeyEnc: &enc, Model: "db-model", TimeoutMs: 30_000}
}

func TestResolveConfigPriority(t *testing.T) {
	env := baseEnv
	env.BaseURL = "http://env/v1"
	stored := storedFixture()

	cfg := ResolveConfig(env, &stored, decryptOk, nil)
	if cfg.Source != "db" || cfg.Provider != ProviderOpenAI || *cfg.BaseURL != "http://db/v1" || cfg.APIKey != "sk-db-key-1234" || cfg.Model != "db-model" || cfg.TimeoutMs != 30_000 {
		t.Fatalf("db-first = %+v", cfg)
	}

	cfg2 := ResolveConfig(env, nil, decryptOk, nil)
	if cfg2.Source != "env" || cfg2.BaseURL == nil || *cfg2.BaseURL != "http://env/v1" {
		t.Fatalf("env fallback = %+v", cfg2)
	}

	off := stored
	off.Provider = SettingOff
	cfg3 := ResolveConfig(env, &off, decryptOk, nil)
	if cfg3.Provider != ProviderNone {
		t.Fatalf("off = %+v", cfg3)
	}
}

func TestResolveConfigUndecryptable(t *testing.T) {
	stored := storedFixture()
	openai := ResolveConfig(baseEnv, &stored, decryptFail, nil)
	if openai.Provider != ProviderOpenAI || openai.APIKey != "" || !openai.KeyUndecryptable {
		t.Fatalf("openai = %+v", openai)
	}
	anth := stored
	anth.Provider, anth.BaseURL = SettingAnthropic, nil
	claude := ResolveConfig(baseEnv, &anth, decryptFail, nil)
	if claude.Provider != ProviderNone || !claude.KeyUndecryptable {
		t.Fatalf("claude = %+v", claude)
	}
}

func TestParseStoredSetting(t *testing.T) {
	bu, enc := "http://db/v1", "enc:sk-db-key-1234"
	full := map[string]any{"provider": "openai", "baseUrl": bu, "apiKeyEnc": enc, "model": "db-model", "timeoutMs": float64(30000)}
	got := ParseStoredSetting(full)
	if got == nil || got.Provider != "openai" || *got.BaseURL != bu || *got.APIKeyEnc != enc || got.Model != "db-model" || got.TimeoutMs != 30000 {
		t.Fatalf("got %+v", got)
	}
	if ParseStoredSetting(map[string]any{"provider": "x", "model": "m", "timeoutMs": float64(1)}) != nil {
		t.Fatal("invalid provider should be nil")
	}
	if ParseStoredSetting(nil) != nil {
		t.Fatal("nil should be nil")
	}
	blank := ParseStoredSetting(map[string]any{"provider": "off", "model": "m", "timeoutMs": float64(5000), "baseUrl": "", "apiKeyEnc": ""})
	if blank == nil || blank.BaseURL != nil || blank.APIKeyEnc != nil {
		t.Fatalf("blank = %+v", blank)
	}
}

func TestResolveSubmittedKey(t *testing.T) {
	cases := []struct {
		typed   string
		clear   bool
		current string
		want    string
	}{
		{"", false, "old", "old"},
		{"  ", false, "old", "old"},
		{" new ", false, "old", "new"},
		{"", true, "old", ""},
		{"new", true, "old", "new"},
	}
	for _, c := range cases {
		if got := ResolveSubmittedKey(c.typed, c.clear, c.current); got != c.want {
			t.Errorf("ResolveSubmittedKey(%q,%v,%q) = %q want %q", c.typed, c.clear, c.current, got, c.want)
		}
	}
}

func TestBuildCandidate(t *testing.T) {
	current := ConfigFromEnv(EnvInput{Provider: "auto", BaseURL: "http://env/v1", APIKey: "sk-env-key-9999", Model: "qwen2.5:7b-instruct", TimeoutMs: 180_000})
	baseURL := " http://a/v1/ "
	model := ""
	sec := 20
	c := BuildCandidate(SettingsInput{Provider: SettingOpenAI, BaseURL: &baseURL, Model: &model, TimeoutSec: &sec}, current)
	if c.Provider != SettingOpenAI || c.BaseURL == nil || *c.BaseURL != "http://a/v1" || c.APIKey != "sk-env-key-9999" || c.Model != "qwen2.5:7b-instruct" || c.TimeoutMs != 20_000 {
		t.Fatalf("got %+v", c)
	}
	if c.ToCallConfig().Provider != ProviderOpenAI {
		t.Fatal("call config provider mismatch")
	}
}

func TestBuildCandidateAnthropicDropsBaseURL(t *testing.T) {
	current := ConfigFromEnv(baseEnv)
	baseURL := "http://a"
	model := "claude"
	c := BuildCandidate(SettingsInput{Provider: SettingAnthropic, BaseURL: &baseURL, Model: &model}, current)
	if c.BaseURL != nil {
		t.Fatalf("baseURL should be dropped for anthropic: %+v", c.BaseURL)
	}
}

func TestValidateCandidate(t *testing.T) {
	current := ConfigFromEnv(baseEnv)
	empty := ""
	ftp := "ftp://x"
	ok := "http://x:11434/v1"
	if msg := ValidateCandidate(BuildCandidate(SettingsInput{Provider: SettingOpenAI, BaseURL: &empty}, current)); !strings.Contains(msg, "接口地址") {
		t.Fatalf("empty baseurl = %q", msg)
	}
	if msg := ValidateCandidate(BuildCandidate(SettingsInput{Provider: SettingOpenAI, BaseURL: &ftp}, current)); !strings.Contains(msg, "http") {
		t.Fatalf("ftp = %q", msg)
	}
	if msg := ValidateCandidate(BuildCandidate(SettingsInput{Provider: SettingOpenAI, BaseURL: &ok}, current)); msg != "" {
		t.Fatalf("valid openai = %q", msg)
	}
	if msg := ValidateCandidate(BuildCandidate(SettingsInput{Provider: SettingAnthropic, ClearAPIKey: true}, current)); !strings.Contains(msg, "Key") {
		t.Fatalf("anthropic no key = %q", msg)
	}
	if msg := ValidateCandidate(BuildCandidate(SettingsInput{Provider: SettingOff}, current)); msg != "" {
		t.Fatalf("off = %q", msg)
	}
	offCandidate := BuildCandidate(SettingsInput{Provider: SettingOff}, current)
	offCandidate.TimeoutMs = 1000
	if msg := ValidateCandidate(offCandidate); !strings.Contains(msg, "超时") {
		t.Fatalf("timeout even when off = %q", msg)
	}
}

func TestToSettingsView(t *testing.T) {
	cfg := ConfigFromEnv(EnvInput{Provider: "auto", BaseURL: "http://env/v1", APIKey: "sk-env-secret-9999", Model: "m", TimeoutMs: 180_000})
	view := ToSettingsView(cfg)
	if view.Source != "env" || view.Provider != SettingOpenAI || !view.APIKey.Set || view.APIKey.Last4 == nil || *view.APIKey.Last4 != "9999" || !view.Enabled || view.TimeoutSec != 180 || view.UpdatedAt != nil {
		t.Fatalf("view = %+v", view)
	}
}

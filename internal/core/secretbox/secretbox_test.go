package secretbox

import (
	"encoding/base64"
	"strings"
	"testing"
)

const testSecret = "a-long-random-settings-secret"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plain := "sk-test-1234567890abcd"
	a, err := Encrypt(plain, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt(plain, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a, "v1:") {
		t.Fatalf("missing version prefix: %s", a)
	}
	if strings.Contains(a, plain) {
		t.Fatal("ciphertext contains plaintext")
	}
	if a == b {
		t.Fatal("IV should differ between calls")
	}
	if got, ok := Decrypt(a, testSecret); !ok || got != plain {
		t.Fatalf("decrypt a = %q, %v", got, ok)
	}
	if got, ok := Decrypt(b, testSecret); !ok || got != plain {
		t.Fatalf("decrypt b = %q, %v", got, ok)
	}
}

func TestDecryptWrongSecret(t *testing.T) {
	box, err := Encrypt("sk-abc", testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Decrypt(box, "another-secret"); ok {
		t.Fatal("expected failure with wrong secret")
	}
}

func TestDecryptMalformed(t *testing.T) {
	box, err := Encrypt("sk-abc", testSecret)
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"v9:" + box[3:],
		"v1:",
		"v1:AAAA",
		"garbage",
	}
	for _, c := range cases {
		if _, ok := Decrypt(c, testSecret); ok {
			t.Errorf("expected failure for %q", c)
		}
	}
	// tamper last byte of payload
	payload, _ := decodeBase64URL(box[3:])
	payload[len(payload)-1] ^= 1
	tampered := "v1:" + base64.RawURLEncoding.EncodeToString(payload)
	if _, ok := Decrypt(tampered, testSecret); ok {
		t.Fatal("expected failure for tampered payload")
	}
}

func TestMaskSecret(t *testing.T) {
	if set, last4 := MaskSecret(""); set || last4 != nil {
		t.Fatalf("empty = %v %v", set, last4)
	}
	if set, last4 := MaskSecret("sk-1234567890wxyz"); !set || last4 == nil || *last4 != "wxyz" {
		t.Fatalf("long = %v %v", set, last4)
	}
	if set, last4 := MaskSecret("short"); !set || last4 != nil {
		t.Fatalf("short = %v %v", set, last4)
	}
}

// TestMaskSecretUTF16 评审 M1：按 UTF-16 长度判断/截取，不是按字节。样例用 node
// `plain.length >= 8 ? plain.slice(-4) : null` 逐条核对过。
func TestMaskSecretUTF16(t *testing.T) {
	// 4 个汉字：UTF-16 长度 4（< 8），字节长度 12（>= 8）——按字节判断会误露 last4，按 UTF-16 判断应为 nil。
	if set, last4 := MaskSecret("测试测试"); !set || last4 != nil {
		got := ""
		if last4 != nil {
			got = *last4
		}
		t.Fatalf("4 汉字（UTF-16 长度 4）last4 应为 nil，got set=%v last4=%q", set, got)
	}
	// 8 个汉字：UTF-16 长度 8，应该露出末 4 个字符（不是末 4 个字节，字节里会被切碎）。
	if set, last4 := MaskSecret("测试测试测试测试"); !set || last4 == nil || *last4 != "测试测试" {
		got := ""
		if last4 != nil {
			got = *last4
		}
		t.Fatalf("8 汉字 last4 = %q, want \"测试测试\" (set=%v)", got, set)
	}
	// 混合 ASCII + 汉字：末 4 个 UTF-16 code unit，与 JS slice(-4) 结果一致（不是末 4 个 rune，
	// 这里两者恰好相同是因为都在 BMP 内；用来确认没有引入 rune 与 UTF-16 code unit 混淆的 bug）。
	if set, last4 := MaskSecret("ab测试cd测试"); !set || last4 == nil || *last4 != "cd测试" {
		got := ""
		if last4 != nil {
			got = *last4
		}
		t.Fatalf("混合字符串 last4 = %q, want \"cd测试\" (set=%v)", got, set)
	}
}

func TestRedactSecret(t *testing.T) {
	if got := RedactSecret("bad key sk-secret-key here", "sk-secret-key"); got != "bad key *** here" {
		t.Fatalf("got %q", got)
	}
	if got := RedactSecret("no key", ""); got != "no key" {
		t.Fatalf("got %q", got)
	}
}

// Package secretbox 设置里的敏感值（目前只有 AI 的 API Key）加密存储：AES-256-GCM（对应旧 lib/secret-box.ts）。
//
//   - 密钥：用 HKDF-SHA256 从 SETTINGS_SECRET 派生 32 字节。
//   - 密文格式：v1:<base64url(iv 12 字节 | tag 16 字节 | 密文)>。版本前缀便于以后换算法。
//   - 解不开（换了密钥、数据损坏、未知版本）一律返回 ok=false，由调用方按「未配置 Key」处理，不返回错误。
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/hkdf"
)

const (
	version = "v1"
	salt    = "vinx-vocab/settings"
	info    = "settings-secret-box v1"
	ivLen   = 12
	tagLen  = 16
)

// DeriveKey 从密钥派生 32 字节 AES 密钥（HKDF-SHA256）。
func DeriveKey(secret string) []byte {
	r := hkdf.New(sha256.New, []byte(secret), []byte(salt), []byte(info))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		panic(err) // HKDF 对 32 字节输出不会失败
	}
	return key
}

func gcmFor(secret string) (cipher.AEAD, error) {
	block, err := aes.NewCipher(DeriveKey(secret))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt 加密明文，返回 "v1:<base64url>"。
func Encrypt(plain, secret string) (string, error) {
	gcm, err := gcmFor(secret)
	if err != nil {
		return "", err
	}
	iv := make([]byte, ivLen)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	// Seal 输出 ciphertext||tag；Node 的格式是 iv||tag||ciphertext，这里重新拼接。
	sealed := gcm.Seal(nil, iv, []byte(plain), nil)
	ct, tag := sealed[:len(sealed)-tagLen], sealed[len(sealed)-tagLen:]
	payload := make([]byte, 0, ivLen+tagLen+len(ct))
	payload = append(payload, iv...)
	payload = append(payload, tag...)
	payload = append(payload, ct...)
	return version + ":" + base64.RawURLEncoding.EncodeToString(payload), nil
}

// Decrypt 解密；解不开（版本不对、损坏、密钥不对）返回 ok=false，不返回 error。
func Decrypt(box, secret string) (plain string, ok bool) {
	parts := strings.SplitN(box, ":", 2)
	if len(parts) != 2 || parts[0] != version || parts[1] == "" {
		return "", false
	}
	raw, err := decodeBase64URL(parts[1])
	if err != nil || len(raw) < ivLen+tagLen {
		return "", false
	}
	gcm, err := gcmFor(secret)
	if err != nil {
		return "", false
	}
	iv, tag, ct := raw[:ivLen], raw[ivLen:ivLen+tagLen], raw[ivLen+tagLen:]
	sealed := append(append([]byte{}, ct...), tag...)
	pt, err := gcm.Open(nil, iv, sealed, nil)
	if err != nil {
		return "", false
	}
	return string(pt), true
}

// decodeBase64URL 兼容 Node 的 "base64url"（无填充；容忍传入的填充）。
func decodeBase64URL(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	return base64.RawURLEncoding.DecodeString(s)
}

// jsLen JS String.length（UTF-16 code unit 数）。secretbox 是 core 最底层的包（core/ai 也依赖它），
// 不引入跨子包依赖，这个小函数在 core/ai、httpx 各自也有一份同名的，都是有意重复（详见各自文件的注释）。
func jsLen(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// MaskSecret 打码：只露末 4 位；Key 太短（< 8 位）时一位都不露。
//
// 评审 M1：按 UTF-16 长度截取（与旧版 plain.length / plain.slice(-4) 一致），不是按字节。Key 含非 ASCII
// 字符时（比如中文），按字节截会切在 UTF-8 多字节序列中间，得到不是合法字符串的垃圾、还可能泄露部分
// Key 字节；旧版这种情况下（UTF-16 长度 < 8）本来就不露 last4，这里改成一致的判断。
func MaskSecret(plain string) (set bool, last4 *string) {
	if plain == "" {
		return false, nil
	}
	units := utf16.Encode([]rune(plain))
	if len(units) >= 8 {
		s := string(utf16.Decode(units[len(units)-4:]))
		return true, &s
	}
	return true, nil
}

// RedactSecret 把文本里出现的 Key 原文替换掉（上游错误信息可能回显 Key）。
//
// 长度门槛按 UTF-16 计（与 MaskSecret 同一处 M1 顺带修正）：替换本身是整串匹配替换，字节长度和 UTF-16
// 长度下结果一致，只有「要不要跳过太短的 Key」这个判断会因计法不同而在极端情况下（1-3 个非 ASCII 字符
// 的 Key）出现分歧。
func RedactSecret(text, secret string) string {
	if jsLen(secret) < 4 {
		return text
	}
	return strings.ReplaceAll(text, secret, "***")
}

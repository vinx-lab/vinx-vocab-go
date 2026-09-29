package school

import (
	"strings"
	"testing"
)

func TestRandomInviteCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c := RandomInviteCode(0)
		if len(c) != 6 {
			t.Fatalf("长度 = %d", len(c))
		}
		for _, r := range c {
			if !strings.ContainsRune(InviteCodeAlphabet, r) {
				t.Fatalf("字符 %q 不在字符集里", r)
			}
		}
		seen[c] = true
	}
	if len(seen) < 190 {
		t.Fatalf("随机性不足：200 次只有 %d 个不同", len(seen))
	}
	for _, bad := range "IO01" {
		if strings.ContainsRune(InviteCodeAlphabet, bad) {
			t.Fatalf("字符集不应含易混字符 %q", bad)
		}
	}
	if len(RandomInviteCode(8)) != 8 {
		t.Fatal("指定长度")
	}
}

package secretbox

import (
	"encoding/json"
	"os"
	"testing"
)

// TestNodeInterop 互通样本：testdata/node_encrypted.json 由旧仓库的 lib/secret-box.ts 算法（node:crypto）
// 生成（见任务报告），Go 用同一密钥能解开；testdata/go_encrypted.json 是本包生成、已用 node 反向验证过能解开的样本，
// 这里再做一次自解校验（真正的 node 解密验证记录在任务报告里，不在每次 go test 里跑 node）。
func TestNodeInterop(t *testing.T) {
	type sample struct {
		Plain string `json:"plain"`
		Box   string `json:"box"`
	}
	for _, name := range []string{"testdata/node_encrypted.json", "testdata/go_encrypted.json"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Secret        string   `json:"secret"`
			NodeEncrypted []sample `json:"nodeEncrypted"`
			GoEncrypted   []sample `json:"goEncrypted"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		for _, s := range append(doc.NodeEncrypted, doc.GoEncrypted...) {
			got, ok := Decrypt(s.Box, doc.Secret)
			if !ok {
				t.Errorf("%s: Decrypt(%q) failed", name, s.Plain)
				continue
			}
			if got != s.Plain {
				t.Errorf("%s: got %q want %q", name, got, s.Plain)
			}
		}
	}
}

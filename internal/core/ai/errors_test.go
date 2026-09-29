package ai

import (
	"errors"
	"net"
	"strings"
	"testing"
)

const testKey = "sk-unit-secret-4242"

var testCtx = ErrorContext{TimeoutMs: 30_000, APIKey: testKey}

// timeoutErr 模拟 net.Error(Timeout()==true)，对应旧版 Node TimeoutError/AbortError。
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "context deadline exceeded" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

func TestDescribeErrorTimeout(t *testing.T) {
	got := DescribeError(timeoutErr{}, testCtx)
	if got != "请求超时（30 秒），可以在系统设置里调大超时，或换一个更快的模型" {
		t.Fatalf("got %q", got)
	}
}

func TestDescribeErrorDial(t *testing.T) {
	dnsErr := &net.DNSError{Err: "no such host", Name: "nope.invalid", IsNotFound: true}
	if got := DescribeError(dnsErr, testCtx); got != "无法连接 AI 服务：ENOTFOUND" {
		t.Fatalf("dns = %q", got)
	}
	opErr := &net.OpError{Op: "dial", Err: errors.New("connect: connection refused")}
	if got := DescribeError(opErr, testCtx); !strings.Contains(got, "无法连接 AI 服务") {
		t.Fatalf("op = %q", got)
	}
}

func TestUpstreamHTTPMessage(t *testing.T) {
	if got := UpstreamHTTPMessage(401, "Incorrect API key provided: "+testKey, testKey); got != "AI 服务返回 401：Incorrect API key provided: ***" {
		t.Fatalf("got %q", got)
	}
	if got := UpstreamHTTPMessage(500, "", ""); got != "AI 服务返回 500" {
		t.Fatalf("got %q", got)
	}
}

func TestUpstreamErrorText(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{"error":{"message":"model not found"}}`, "model not found"},
		{`{"error":"bad"}`, "bad"},
		{`{"message":"m"}`, "m"},
		{"<html>502 Bad Gateway</html>", "<html>502 Bad Gateway</html>"},
	}
	for _, c := range cases {
		if got := UpstreamErrorText(c.body); got != c.want {
			t.Errorf("UpstreamErrorText(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}

func TestUnparseableMessage(t *testing.T) {
	reply := "好的，这是结果：\n" + strings.Repeat("x", 300)
	m := UnparseableMessage(reply)
	if !strings.HasPrefix(m, "AI 返回的内容无法解析：好的，这是结果： x") {
		t.Fatalf("got %q", m)
	}
	wantLen := jsLen("AI 返回的内容无法解析：") + 160
	// 中文字符按 rune 计，前缀 "AI 返回的内容无法解析：" 里的中文与 ASCII 混合，用 jsLen 才准确
	if jsLen(m) != wantLen {
		t.Fatalf("len(m)=%d want %d (m=%q)", jsLen(m), wantLen, m)
	}
	if got := UnparseableMessage("  "); got != Unparseable {
		t.Fatalf("blank = %q", got)
	}
}

func TestDescribeErrorBusinessAndRedact(t *testing.T) {
	if got := DescribeError(NewAPIError("SERVER", "AI 服务返回 401：key "+testKey), testCtx); got != "AI 服务返回 401：key ***" {
		t.Fatalf("got %q", got)
	}
	if got := DescribeError(errors.New("boom "+testKey), testCtx); got != "AI 调用失败：boom ***" {
		t.Fatalf("got %q", got)
	}
	dnsErr := &net.DNSError{Err: "no such host", IsNotFound: true}
	if got := DescribeError(dnsErr, testCtx); strings.Contains(got, testKey) {
		t.Fatalf("leaked key: %q", got)
	}
}

func TestToAPIError(t *testing.T) {
	a := ToAPIError(NewAPIError("INVALID_ACTION", "未配置"), testCtx)
	if a.Code != "INVALID_ACTION" {
		t.Fatalf("code = %s", a.Code)
	}
	dnsErr := &net.DNSError{Err: "no such host", IsNotFound: true}
	b := ToAPIError(dnsErr, testCtx)
	if b.Code != "SERVER" || b.Message != "无法连接 AI 服务：ENOTFOUND" {
		t.Fatalf("b = %+v", b)
	}
}

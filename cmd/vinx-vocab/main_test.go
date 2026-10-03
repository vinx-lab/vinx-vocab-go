package main

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/vinx-lab/vinx-vocab-go/internal/api"
)

func listen(t *testing.T, h http.Handler) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

func TestPortInUseByOtherProgram(t *testing.T) {
	port := listen(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("other")) }))
	var out, errb bytes.Buffer
	code := run([]string{"--host", "127.0.0.1", "--port", strconv.Itoa(port), "--data", t.TempDir()}, &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "已被其他程序占用") || !strings.Contains(errb.String(), "--port") {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
}

func TestPortInUseBySelf(t *testing.T) {
	port := listen(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.Header().Set(api.InstanceHeader, "dev")
			w.Write([]byte(`{"success":true,"data":{"status":"ok"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	var out, errb bytes.Buffer
	code := run([]string{"serve", "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--data", t.TempDir()}, &out, &errb)
	if code != 0 || !strings.Contains(out.String(), "已经在运行：http://localhost:"+strconv.Itoa(port)) {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
}

func TestDataDirNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("需要非 root 的类 Unix 环境模拟不可写目录")
	}
	ro := filepath.Join(t.TempDir(), "ro")
	os.Mkdir(ro, 0o555)
	defer os.Chmod(ro, 0o755)
	var out, errb bytes.Buffer
	code := run([]string{"--port", "1", "--data", ro}, &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "不可写") {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	code = run([]string{"seed-demo", "--data", filepath.Join(ro, "sub")}, &out, &errb)
	if code != 1 {
		t.Fatalf("seed-demo code=%d", code)
	}
}

func TestDevFlag(t *testing.T) {
	var errb bytes.Buffer
	f, err := parseFlags("serve", []string{"--dev", "--data", t.TempDir()}, &errb, true)
	if err != nil || !f.dev {
		t.Fatalf("--dev 未解析：%+v %v", f, err)
	}
	cfg, err := loadConfig(f)
	if err != nil || !cfg.Dev {
		t.Fatalf("cfg.Dev = %v %v", cfg != nil && cfg.Dev, err)
	}
	// seed-demo 等子命令不接受 --dev
	if _, err := parseFlags("seed-demo", []string{"--dev"}, &errb, false); err == nil {
		t.Fatal("seed-demo 不应接受 --dev")
	}

	var out bytes.Buffer
	printBanner(&out, cfg)
	if !strings.Contains(out.String(), "开发模式：任何能访问本服务的人都可以免密登录任何账号，不要用于真实数据") {
		t.Fatalf("开发模式缺少提示：%s", out.String())
	}
	cfg.Dev = false
	out.Reset()
	printBanner(&out, cfg)
	if strings.Contains(out.String(), "开发模式") {
		t.Fatalf("非开发模式不应提示：%s", out.String())
	}
}

func TestVersionAndUnknown(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "vinx-vocab") {
		t.Fatalf("version = %d %s", code, out.String())
	}
	if code := run([]string{"bogus"}, &out, &errb); code != 2 {
		t.Fatalf("unknown = %d", code)
	}
}

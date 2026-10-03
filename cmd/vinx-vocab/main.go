// vinx-vocab：Vinx Vocab 单文件版。
//
//	vinx-vocab [serve] [--port 3000] [--host 0.0.0.0] [--data DIR] [--dev]   启动服务（默认子命令；--dev 开发模式）
//	vinx-vocab seed-demo [--data DIR]                                 写入演示账号、班级与计划
//	vinx-vocab import --from-postgres URL [--settings-secret S] [--force] [--edition E]   从旧版 PostgreSQL 导入
//	vinx-vocab version                                                打印版本
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // Windows 没有系统时区库：内嵌一份，保证 APP_TIMEZONE 可用

	"github.com/vinx-lab/vinx-vocab-go/internal/api"
	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/seed"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// version 由构建参数注入：-ldflags "-X main.version=…"。
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	switch cmd {
	case "serve":
		return serve(args, stdout, stderr)
	case "seed-demo":
		return seedDemo(args, stdout, stderr)
	case "audio":
		return audioCmd(args, stdout, stderr)
	case "import":
		return importCmd(args, stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "vinx-vocab", version)
		return 0
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	}
	fmt.Fprintf(stderr, "未知子命令 %q\n\n", cmd)
	usage(stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprint(w, `用法：
  vinx-vocab [serve] [--port 3000] [--host 0.0.0.0] [--data 数据目录] [--dev]
                                                                        启动服务（默认）；--dev 开发模式，可免密切换账号，只用于测试数据
  vinx-vocab seed-demo [--data 数据目录]                               写入演示账号（密码 dev123456）、演示班级 DEMO01 与演示计划
  vinx-vocab audio prefetch [--data 目录] [--apply] [--limit N] [--delay 毫秒]
                                                                        全量预缓存真人发音（默认演练，--apply 才真的抓）
  vinx-vocab import --from-postgres <URL> [--settings-secret <旧密钥>] [--force] [--edition school|personal] [--data 目录]
                                                                        从旧版 PostgreSQL 导入全部数据（目标库有数据时需 --force，先备份）
  vinx-vocab version                                                   打印版本

数据目录默认：Windows 为程序旁的 vinx-data，其他系统为当前目录下的 data。
`)
}

type commonFlags struct {
	port int
	host string
	data string
	dev  bool
}

func parseFlags(name string, args []string, stderr io.Writer, withListen bool) (*commonFlags, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	f := &commonFlags{}
	fs.StringVar(&f.data, "data", "", "数据目录")
	if withListen {
		fs.IntVar(&f.port, "port", 0, "监听端口（默认 3000）")
		fs.StringVar(&f.host, "host", "", "监听地址（默认 0.0.0.0）")
		fs.BoolVar(&f.dev, "dev", false, "开发模式：开放免密切换账号（只用于测试数据）")
	}
	return f, fs.Parse(args)
}

func loadConfig(f *commonFlags) (*config.Config, error) {
	return config.Load(config.Options{DataDir: f.data, Host: f.host, Port: f.port, Dev: f.dev})
}

func seedDemo(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags("seed-demo", args, stderr, false)
	if err != nil {
		return 2
	}
	cfg, err := loadConfig(f)
	if err != nil {
		fmt.Fprintln(stderr, "启动失败：", err)
		return 1
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(stderr, "打开数据库失败：", err)
		return 1
	}
	defer db.Close()
	if err := seed.Demo(context.Background(), db, time.Now()); err != nil {
		fmt.Fprintln(stderr, "写入演示数据失败：", err)
		return 1
	}
	fmt.Fprintf(stdout, "演示数据已写入 %s\n账号：admin / teacher / student / student2@vinx.test，密码 %s；班级邀请码 %s\n", cfg.DBPath, seed.DemoPassword, seed.DemoInviteCode)
	return 0
}

func serve(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags("serve", args, stderr, true)
	if err != nil {
		return 2
	}
	cfg, err := loadConfig(f)
	if err != nil {
		fmt.Fprintln(stderr, "启动失败：", err)
		return exitWait(1, stdout)
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUse(err) {
			local := fmt.Sprintf("http://localhost:%d", cfg.Port)
			if probeSelf(cfg.Host, cfg.Port) {
				fmt.Fprintf(stdout, "Vinx Vocab 已经在运行：%s\n", local)
				if runtime.GOOS == "windows" {
					openBrowser(local)
				}
				return exitWait(0, stdout)
			}
			fmt.Fprintf(stderr, "端口 %d 已被其他程序占用。请关闭占用该端口的程序，或用 --port 换一个端口（例如 --port %d）。\n", cfg.Port, cfg.Port+1)
			return exitWait(1, stdout)
		}
		fmt.Fprintf(stderr, "无法监听 %s：%v\n", addr, err)
		return exitWait(1, stdout)
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		ln.Close()
		fmt.Fprintln(stderr, "打开数据库失败：", err)
		return exitWait(1, stdout)
	}
	defer db.Close()
	if err := seed.FirstRunBooks(context.Background(), db, time.Now()); err != nil {
		ln.Close()
		fmt.Fprintln(stderr, "导入内置词书失败：", err)
		return exitWait(1, stdout)
	}

	deps := api.NewDeps(db, cfg, nil)
	deps.Version = version
	srv := &http.Server{Handler: api.Handler(deps), ReadHeaderTimeout: 30 * time.Second}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	printBanner(stdout, cfg)
	if runtime.GOOS == "windows" {
		openBrowser(fmt.Sprintf("http://localhost:%d", cfg.Port))
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(stderr, "服务异常退出：", err)
			return exitWait(1, stdout)
		}
	case <-sig:
		// Windows 在 CTRL_CLOSE_EVENT（关窗口）大约 5 秒后会强杀进程（Go 把它映射成这里收到的
		// SIGTERM）；把关闭超时缩到 2 秒，尽量赶在被强杀前跑完 db.Close()。
		// 其余平台维持 5 秒，给正在进行的请求（如最长 10 秒的发音下载）更多收尾时间。
		timeout := 5 * time.Second
		if runtime.GOOS == "windows" {
			timeout = 2 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		srv.Shutdown(ctx)
	}
	return 0
}

// isAddrInUse 端口被占用（Linux EADDRINUSE；Windows WSAEADDRINUSE = 10048）。
func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == 10048 {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "address already in use") || strings.Contains(msg, "Only one usage of each socket address")
}

// probeSelf 同端口上已在运行的是不是本程序：/api/health 带 X-Vinx-Vocab 头。
func probeSelf(host string, port int) bool {
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/api/health", net.JoinHostPort(host, strconv.Itoa(port))))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200 && resp.Header.Get(api.InstanceHeader) != ""
}

func printBanner(w io.Writer, cfg *config.Config) {
	fmt.Fprintf(w, "\nVinx Vocab %s 已启动\n", version)
	fmt.Fprintf(w, "  本机访问：  http://localhost:%d\n", cfg.Port)
	if cfg.Host == "0.0.0.0" || cfg.Host == "" || cfg.Host == "::" {
		for _, ip := range lanIPs() {
			fmt.Fprintf(w, "  局域网访问：http://%s:%d\n", ip, cfg.Port)
		}
	}
	fmt.Fprintf(w, "  数据目录：  %s\n", cfg.DataDir)
	if cfg.Dev {
		fmt.Fprintln(w, "\n!!! 开发模式：任何能访问本服务的人都可以免密登录任何账号，不要用于真实数据 !!!")
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintln(w, "\n关闭此窗口即停止服务。")
		fmt.Fprintln(w, "首次运行时 Windows 可能弹出防火墙提示：勾选「专用网络」并允许，手机才能通过局域网地址访问。")
	} else {
		fmt.Fprintln(w, "\n按 Ctrl+C 停止服务。")
	}
}

// lanIPs 本机的局域网 IPv4 地址：只取已启用的物理 / 常规网卡，跳过回环、链路本地、
// 容器与虚拟网桥（docker、br-、veth、virbr、vEthernet 等）、代理 TUN 常用的 198.18.0.0/15，
// 以及 Tailscale 等使用的 RFC 6598 共享地址段 100.64/10（不是真正意义上的「局域网」）。
func lanIPs() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	skip := []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "lxc", "vethernet", "vmnet", "utun", "tun", "tap"}
	_, bench, _ := net.ParseCIDR("198.18.0.0/15")
	cgnat := &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(ifc.Name)
		virtual := false
		for _, p := range skip {
			if strings.HasPrefix(name, p) {
				virtual = true
				break
			}
		}
		if virtual {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || bench.Contains(ip) || cgnat.Contains(ip) {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
}

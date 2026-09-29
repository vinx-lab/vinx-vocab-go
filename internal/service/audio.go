// 真人发音：按需从发音源抓取 MP3 并落盘缓存，之后由本服务直接提供（对应旧 services/audio.ts）。
//
// 默认源为有道词典发音接口（type=2 为英式/美式真人录音）；AUDIO_PROVIDER_URL 可换成自建或已授权的音源，
// 置空则关闭该能力，前端自动退回浏览器合成语音。缓存文件在 <数据目录>/audio/<sha1>.mp3。
package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vinx-lab/vinx-vocab-go/internal/config"
	"github.com/vinx-lab/vinx-vocab-go/internal/core/audioplan"
	"github.com/vinx-lab/vinx-vocab-go/internal/httpx"
	"github.com/vinx-lab/vinx-vocab-go/internal/store"
)

// httpClient 发音源与 AI 供应商共用的客户端。
//
// 有意保留 Go 标准库默认行为：读取 HTTP_PROXY/HTTPS_PROXY/NO_PROXY 环境变量（Node 的 fetch 不读，
// 这是与 oracle 的一处已知行为差异，评审 M6 的结论是维持现状——本机等部署环境的出站流量本来就要经
// 代理才能连到公网，跟随系统代理变量是期望行为，不要为了跟 oracle 保持字面一致而关闭它）。
var httpClient = http.DefaultClient

// AudioMinBytes / AudioMaxBytes 缓存音频的体积范围（小于下限视为无效音频，大于上限拒绝写入）。
const (
	AudioMinBytes = 512
	AudioMaxBytes = 2 * 1024 * 1024
)

func readCache(dir, file string) ([]byte, bool) {
	b, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil || len(b) < AudioMinBytes {
		return nil, false
	}
	return b, true
}

// DownloadAudio 从发音源抓一个读音文本的音频，失败或不是有效音频时返回错误（预缓存命令也用）。
func DownloadAudio(ctx context.Context, cfg *config.Config, text string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	target := strings.ReplaceAll(cfg.AudioProviderURL, "{word}", audioplan.EncodeURIComponent(text))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "VinxVocab/2.1")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// 读取上限设为 AudioMaxBytes+1：足够让下面的体积校验正常拒绝超限响应，同时避免发音源
	// 返回异常大响应体时无限占用内存。
	body, err := io.ReadAll(io.LimitReader(resp.Body, AudioMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpx.NewError(httpx.CodeNoData, "发音源返回 "+strconv.Itoa(resp.StatusCode), nil)
	}
	ct := resp.Header.Get("content-type")
	if len(body) < AudioMinBytes || len(body) > AudioMaxBytes || !audioContentTypeRe.MatchString(ct) {
		return nil, httpx.NewError(httpx.CodeNoData, "发音源返回的不是有效音频", nil)
	}
	return body, nil
}

var audioContentTypeRe = regexp.MustCompile(`audio|octet-stream`)

// WriteAudioFileAtomic 原子写入缓存文件：先写同目录下的临时文件再 rename（评审 M8）。
// rename 在同一文件系统内是原子操作，进程中途退出不会留下不完整、却仍 >= AudioMinBytes 因而被当成
// 有效缓存的半截文件（此前是直接 os.WriteFile，中途 kill -9 会留下截断文件，此后一直被当命中）。
func WriteAudioFileAtomic(dir, file string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+file+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil {
		os.Remove(tmpName)
		return werr
	}
	if cerr != nil {
		os.Remove(tmpName)
		return cerr
	}
	if err := os.Rename(tmpName, filepath.Join(dir, file)); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// audioFetchGroup 按缓存文件去重并发下载：同一个词（准确说是同一个目标读音文本对应的缓存文件）被
// 并发点播时，只有一个 goroutine 真正发请求，其余等待同一个结果（评审 M8）。key 用 "<AudioDir>|<file>"，
// 覆盖了理论上可能出现的多个数据目录同进程运行的情况（目前 CLI/serve 各自单进程单数据目录，够用）。
var audioFetchGroup keyedSingleflight

type audioDownloadResult struct {
	buf    []byte
	cached bool
}

// keyedSingleflight 按 key 合并并发调用：同一 key 同时只有一次真正执行，其余等待结果（不缓存结果，
// 执行完立即清理，下一次调用会重新执行）。不引入外部依赖（等价于 golang.org/x/sync/singleflight.Group
// 的最小实现），线程安全。
type keyedSingleflight struct {
	mu    sync.Mutex
	calls map[string]*sfCall
}

type sfCall struct {
	done chan struct{}
	val  any
	err  error
}

func (g *keyedSingleflight) Do(key string, fn func() (any, error)) (val any, err error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*sfCall{}
	}
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-c.done
		return c.val, c.err
	}
	c := &sfCall{done: make(chan struct{})}
	g.calls[key] = c
	g.mu.Unlock()

	// 复审 round 2：fn 一旦 panic，必须仍然关掉 done、把 key 从 g.calls 里删掉——否则当前排队的
	// follower（下面的 <-c.done）和这个 key 此后的所有调用都会永久卡死，直到进程重启（比单次请求
	// 500 严重得多，和 I3 是同一类"一次异常不能拖累其他请求"的问题）。把 panic 转成 c.err 而不是
	// 重新 panic：leader 自己也在这里拿到一个普通 error（走各调用方已有的错误处理），不需要区分
	// "我是 leader 所以要再 panic 一次、follower 只拿 error" 这种不对称，行为更好理解也更好测。
	//
	// 用命名返回值：recover 只能让 Do 正常返回，不能"跳回" panic 发生点继续执行到下面那句
	// `return c.val, c.err`——不用命名返回值的话，panic 路径下 Do 会返回零值 (nil, nil)，
	// 调用方拿到一对 nil 反而分不清是"成功但没结果"还是"失败"，是个更隐蔽的坑。
	defer func() {
		if p := recover(); p != nil {
			slog.Error("发音下载 panic", "key", key, "panic", p)
			c.err = fmt.Errorf("panic: %v", p)
			c.val = nil
		}
		close(c.done)
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
		val, err = c.val, c.err
	}()
	c.val, c.err = fn()
	return c.val, c.err
}

// GetWordAudio 取某个词的发音音频：命中缓存直接返回，否则抓取后落盘并记到 Word.audioFile。
// 并发请求同一份缓存文件时只发一次下载（singleflight），下载写盘是原子的（先写临时文件再 rename）。
func GetWordAudio(ctx context.Context, q store.Querier, cfg *config.Config, wordID string) (buf []byte, cached bool, err error) {
	if !AudioEnabled(cfg) {
		return nil, false, httpx.NewError(httpx.CodeNoData, "未配置发音源", nil)
	}
	var spelling string
	var audioFile *string
	err = q.QueryRowContext(ctx, `SELECT "spelling","audioFile" FROM "Word" WHERE "id" = ?`, wordID).Scan(&spelling, &audioFile)
	if store.IsNoRows(err) {
		return nil, false, httpx.NotFound("单词不存在")
	}
	if err != nil {
		return nil, false, err
	}

	text := audioplan.Target(spelling)
	file := audioplan.FileName(text)
	if b, ok := readCache(cfg.AudioDir, file); ok {
		if audioFile == nil || *audioFile != file {
			if _, err := q.ExecContext(ctx, `UPDATE "Word" SET "audioFile"=? WHERE "id"=?`, file, wordID); err != nil {
				return nil, false, err
			}
		}
		return b, true, nil
	}

	sfKey := cfg.AudioDir + "|" + file
	v, err := audioFetchGroup.Do(sfKey, func() (any, error) {
		// 排队等待期间可能已经有其他请求抓好了，重新查一次缓存，避免重复下载。
		if b, ok := readCache(cfg.AudioDir, file); ok {
			return audioDownloadResult{buf: b, cached: true}, nil
		}
		b, err := DownloadAudio(ctx, cfg, text)
		if err != nil {
			return nil, err
		}
		if err := WriteAudioFileAtomic(cfg.AudioDir, file, b); err != nil {
			return nil, err
		}
		return audioDownloadResult{buf: b, cached: false}, nil
	})
	if err != nil {
		return nil, false, err
	}
	r := v.(audioDownloadResult)
	if _, err := q.ExecContext(ctx, `UPDATE "Word" SET "audioFile"=? WHERE "id"=?`, file, wordID); err != nil {
		return nil, false, err
	}
	return r.buf, r.cached, nil
}

// PrefetchResult 批量预缓存统计。
type PrefetchResult struct {
	OK      int `json:"ok"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// PrefetchAudio 批量预缓存（词书/单元维度）。
func PrefetchAudio(ctx context.Context, q store.Querier, cfg *config.Config, wordIDs []string) (PrefetchResult, error) {
	var r PrefetchResult
	for _, id := range wordIDs {
		_, cached, err := GetWordAudio(ctx, q, cfg, id)
		if err != nil {
			r.Failed++
			continue
		}
		if cached {
			r.Skipped++
		} else {
			r.OK++
		}
	}
	return r, nil
}

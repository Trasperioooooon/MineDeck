// Package dl 提供多源故障转移下载：按优先级尝试镜像/官方源，
// 自动重试（指数退避）、哈希校验（SHA-1/SHA-256/MD5）、原子落盘、无进展看门狗。
package dl

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const UA = "MineDeck/1.0 (Minecraft server setup tool; Windows)"

// GHMirrors 把 GitHub 直链（github.com 的 Release 资产、raw.githubusercontent.com 的文件）包装成国内加速候选，
// 依次尝试 ghfast.top、gh-proxy.com，最后直连。自动更新、下载 frp、取 AI 价格数据三处共用。
// 经镜像取到的内容可能被改过：exe 和 frp 要对上官方 SHA-256，价格数据只用来显示估算、按不可信数据严格校验。
func GHMirrors(u string) []string {
	return []string{
		"https://ghfast.top/" + u,
		"https://gh-proxy.com/" + u,
		u,
	}
}

var Client = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		MaxIdleConnsPerHost:   8,
	},
}

// Progress 下载进度回调；total 可能为 -1（未知大小）。
type Progress func(done, total int64)

type Request struct {
	URLs    []string // 按优先级排列的候选地址
	Dest    string   // 目标文件绝对路径
	SHA1    string
	SHA256  string
	MD5     string
	MinSize int64 // 完整性下限（防错误页面被当文件保存）
}

func (r Request) hasHash() bool { return r.SHA1 != "" || r.SHA256 != "" || r.MD5 != "" }

type multiHash struct{ s1, s256, m5 hash.Hash }

func newHashes() *multiHash {
	return &multiHash{s1: sha1.New(), s256: sha256.New(), m5: md5.New()}
}

func (m *multiHash) Write(p []byte) (int, error) {
	m.s1.Write(p)
	m.s256.Write(p)
	m.m5.Write(p)
	return len(p), nil
}

func (m *multiHash) check(r Request) error {
	eq := func(h hash.Hash, want string) bool {
		return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want)
	}
	if r.SHA1 != "" && !eq(m.s1, r.SHA1) {
		return errors.New("SHA-1 校验失败")
	}
	if r.SHA256 != "" && !eq(m.s256, r.SHA256) {
		return errors.New("SHA-256 校验失败")
	}
	if r.MD5 != "" && !eq(m.m5, r.MD5) {
		return errors.New("MD5 校验失败")
	}
	return nil
}

// verifyFile 检查已存在的目标文件是否可直接复用（缓存命中）。
func verifyFile(r Request) bool {
	st, err := os.Stat(r.Dest)
	if err != nil || st.IsDir() || st.Size() == 0 || st.Size() < r.MinSize {
		return false
	}
	if !r.hasHash() {
		return true
	}
	f, err := os.Open(r.Dest)
	if err != nil {
		return false
	}
	defer f.Close()
	hs := newHashes()
	if _, err := io.Copy(hs, f); err != nil {
		return false
	}
	return hs.check(r) == nil
}

// Fetch 执行多源下载：URLs 依序尝试，整体最多 3 轮，轮间指数退避。
func Fetch(ctx context.Context, r Request, prog Progress) error {
	if len(r.URLs) == 0 {
		return errors.New("没有可用下载源")
	}
	if verifyFile(r) {
		if prog != nil {
			if st, err := os.Stat(r.Dest); err == nil {
				prog(st.Size(), st.Size())
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.Dest), 0o755); err != nil {
		return err
	}
	var lastErr error
	for round := 0; round < 3; round++ {
		if round > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(round*2) * time.Second):
			}
		}
		for _, u := range r.URLs {
			err := fetchOne(ctx, u, r, prog)
			if err == nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = fmt.Errorf("%s: %w", shortURL(u), err)
		}
	}
	return fmt.Errorf("多次尝试后仍下载失败（%w）", lastErr)
}

func shortURL(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return u
}

func fetchOne(ctx context.Context, url string, r Request, prog Progress) error {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// 90 秒无新字节则中断本次尝试，换下一个源
	watchdog := time.AfterFunc(90*time.Second, cancel)
	defer watchdog.Stop()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UA)
	resp, err := Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	total := resp.ContentLength
	part := r.Dest + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	hs := newHashes()
	var done int64
	buf := make([]byte, 256*1024)
	var werr error
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(90 * time.Second)
			if _, werr = f.Write(buf[:n]); werr != nil {
				break
			}
			hs.Write(buf[:n])
			done += int64(n)
			if prog != nil {
				prog(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			werr = rerr
			break
		}
	}
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil && total > 0 && done != total {
		werr = fmt.Errorf("下载不完整 (%d/%d 字节)", done, total)
	}
	if werr == nil && done < r.MinSize {
		werr = fmt.Errorf("文件过小 (%d 字节)，疑似为错误页面", done)
	}
	if werr == nil {
		werr = hs.check(r)
	}
	if werr != nil {
		os.Remove(part)
		return werr
	}
	os.Remove(r.Dest)
	return os.Rename(part, r.Dest)
}

// FetchJSON 依次尝试多个 URL 获取并解析 JSON。
func FetchJSON(ctx context.Context, urls []string, out any) error {
	b, err := FetchBytes(ctx, urls)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// FetchBytes 依次尝试多个 URL 获取小体积响应（上限 64MB，单次 30 秒超时）。
func FetchBytes(ctx context.Context, urls []string) ([]byte, error) {
	var lastErr error
	for round := 0; round < 2; round++ {
		for _, u := range urls {
			b, err := fetchBytesOne(ctx, u)
			if err == nil {
				return b, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("%s: %w", shortURL(u), err)
		}
	}
	return nil, lastErr
}

func fetchBytesOne(ctx context.Context, url string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UA)
	req.Header.Set("Accept", "application/json, text/html;q=0.9, */*;q=0.8")
	resp, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

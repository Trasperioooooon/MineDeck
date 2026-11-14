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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/atomicfile"
	"minedeck/internal/diskguard"
)

const UA = "MineDeck/1.0 (Minecraft server setup tool; Windows)"

// ErrVerify 下载的内容没有通过哈希校验。和「下载不下来」不同：地址本身没坏，
// 是拿回来的内容对不上给定的校验值（改动过的文件、坏 CDN 等）。
// 调用方可以用 errors.Is 认出它，把这样的文件降级成「待用户处置」而不是整次失败。
var ErrVerify = errors.New("哈希校验失败")

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
	// Client 本次下载用的 HTTP 客户端；nil 用 dl.Client。
	// 整合包文件下载传 GuardedClient(...)：每一跳连出去之前都查是不是公网，跳转只跟 https 的。
	Client *http.Client
	// OnRetry 某个地址这一次没下成、后面还会接着试（下一个地址或下一轮）时调用（host 只到主机名）；可以为 nil。
	// 最后一个地址的最后一轮也没下成时不调：不会再试了，原因看 Fetch 返回的错误（以前也调，部署页多写一句「换下一个下载源」）。
	// 给部署页提示「哪个地址超时、第几次重试」用（测试报告 B-01）
	OnRetry func(host string, err error)
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
		return fmt.Errorf("%w：SHA-1 校验失败", ErrVerify)
	}
	if r.SHA256 != "" && !eq(m.s256, r.SHA256) {
		return fmt.Errorf("%w：SHA-256 校验失败", ErrVerify)
	}
	if r.MD5 != "" && !eq(m.m5, r.MD5) {
		return fmt.Errorf("%w：MD5 校验失败", ErrVerify)
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
	part := f.Name()
	defer os.Remove(part)
	hs := newHashes()
	if _, err := io.Copy(hs, f); err != nil {
		return false
	}
	return hs.check(r) == nil
}

// retryWait 第 n 轮开始前等多久（测试里改短）。
var retryWait = func(round int) time.Duration { return time.Duration(round*2) * time.Second }

// Fetch 执行多源下载：URLs 依序尝试，整体最多 3 轮，轮间指数退避。
func Fetch(ctx context.Context, r Request, prog Progress) error {
	release, err := atomicfile.LockPathContext(ctx, r.Dest)
	if err != nil {
		return err
	}
	defer release()
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
	// 每个地址只留最后一次失败。全部试完再汇总，避免界面上只剩最后一个主机名。
	// 每一次失败另外都记进 tried：同一个源前一轮写盘被杀毒软件拦下、后一轮超时，AnyFailure 仍要认得出前一轮（Codex 复审发现）
	var fails []urlFail
	var tried []error
	failAt := map[string]int{}
	for round := 0; round < 3; round++ {
		if round > 0 {
			select {
			case <-ctx.Done():
				return ctxFail(ctx, tried)
			case <-time.After(retryWait(round)):
			}
		}
		for i, u := range r.URLs {
			err := fetchOne(ctx, u, r, prog)
			var noSpace *diskguard.NoSpace
			if errors.Is(err, diskguard.ErrUnknown) || errors.As(err, &noSpace) {
				return err
			}
			if err == nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctxFail(ctx, tried)
			}
			tried = append(tried, err)
			if j, ok := failAt[u]; ok {
				fails[j].err = err
			} else {
				failAt[u] = len(fails)
				fails = append(fails, urlFail{url: u, err: err})
			}
			if r.OnRetry != nil && (round < 2 || i < len(r.URLs)-1) {
				r.OnRetry(shortURL(u), err)
			}
		}
	}
	return newFetchFail(fails, tried)
}

// ctxFail 重试途中整体超时或被取消时返回的错误：说法和 errors.Is 都还是 ctx 的错误，
// 之前每一次的失败带在里面给 AnyFailure。前一次写盘被杀毒软件拦下、后面拖到了总超时，界面上仍要说是被拦下了（Codex 复审发现）。
func ctxFail(ctx context.Context, tried []error) error {
	if len(tried) == 0 {
		return ctx.Err()
	}
	return &fetchFailError{msg: ctx.Err().Error(), last: ctx.Err(), all: tried}
}

// urlFail 一个候选地址的最后一次失败。
type urlFail struct {
	url string
	err error
}

// fetchFailError 多源都没下成。Error 给界面一行摘要（每个源的主机名和原因，原因不截断）；
// Unwrap 仍是最后一个地址的原始错误，errors.Is 认得出哈希校验失败。每一次尝试的错误都留在 all 里，见 AnyFailure。
type fetchFailError struct {
	msg  string
	last error
	all  []error
}

func (e *fetchFailError) Error() string { return e.msg }

func (e *fetchFailError) Unwrap() error { return e.last }

// AnyFailure 判断这次失败里有没有一个错误让 match 返回 true：先看 err 本身（含包装链），
// 是 Fetch 的多源汇总错误时，再逐个看每一次尝试的错误（不只是每个地址的最后一次）。
// errors.Is 只认最后一个地址：那是 ErrVerify 的老语义，导入整合包靠它分「转待决」还是「整次失败」，不能变。
// 要问「有没有哪个源是因为某种原因没下成的」（比如写盘时被杀毒软件拦下，后面的源只是 404）用这个。
func AnyFailure(err error, match func(error) bool) bool {
	if match(err) {
		return true
	}
	var ff *fetchFailError
	if errors.As(err, &ff) {
		for _, e := range ff.all {
			if match(e) {
				return true
			}
		}
	}
	return false
}

// httpStatusError 保留状态码供最终摘要归类；Error 的子串格式不变，重试回调等原有调用方不受影响。
type httpStatusError struct {
	status int
}

func (e *httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.status) }

func httpStatusHint(status int) string {
	switch status {
	case 567: // 腾讯 EdgeOne 拦机器人时回的状态码
		return "被源站 CDN 的机器人防护拦截"
	case 403: // 所有下载源共用这句话，单凭 403 断定不了是哪一种，只能把常见的几种都说到
		return "源站拒绝访问：可能是机器人防护、链接失效或没有权限"
	case 520, 521, 522, 523, 524:
		return "源站故障或超时"
	case 404:
		return "该源没有这个文件"
	default:
		return ""
	}
}

// newFetchFail 汇总多源失败。tried 是按顺序的每一次失败，给 AnyFailure 用；为空时用每个地址的最后一次。
func newFetchFail(fails []urlFail, tried []error) error {
	seen := make(map[string]struct{}, len(fails))
	parts := make([]string, 0, len(fails))
	all := tried
	if len(all) == 0 {
		all = make([]error, 0, len(fails))
		for _, f := range fails {
			all = append(all, f.err)
		}
	}
	var last error
	for _, f := range fails {
		last = f.err
		reason := compactReason(f.err)
		var statusErr *httpStatusError
		if errors.As(f.err, &statusErr) {
			if hint := httpStatusHint(statusErr.status); hint != "" {
				reason += "（" + hint + "）"
			}
		}
		line := shortURL(f.url) + ": " + reason
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		parts = append(parts, line)
	}
	return &fetchFailError{
		msg:  "多次尝试后仍下载失败（" + strings.Join(parts, "；") + "）",
		last: last,
		all:  all,
	}
}

// compactReason 一个源的失败原因，压成一行。
// 连不上、超时这类错误（*url.Error）只留底层原因：主机名已经写在前面，完整地址只会把句子拉长，
// 还可能带着签名用的查询串。其余的原样保留，不截断：本机写文件出错时报错是「操作 路径: 原因」，
// 原因在最后，截断正好把它截掉（磁盘满、文件被杀毒软件占用）。
// 整句太长时隧道卡片自己会截（tunnel 的 setLastErr），控制台里留的是全文。
func compactReason(err error) string {
	if err == nil {
		return ""
	}
	if ue, ok := err.(*url.Error); ok && ue.Err != nil {
		err = ue.Err
	}
	return strings.Join(strings.Fields(err.Error()), " ")
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
	lease, err := diskguard.Acquire(ctx, filepath.Dir(r.Dest), diskguard.Options{})
	if err != nil {
		return err
	}
	defer lease.Close()
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
	client := r.Client
	if client == nil {
		client = Client
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &httpStatusError{status: resp.StatusCode}
	}
	total := resp.ContentLength
	if err := lease.Check(max(total, 0)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(r.Dest), ".minedeck-download-*.part")
	if err != nil {
		return err
	}
	part := f.Name()
	defer os.Remove(part)
	w := lease.Writer(f)
	hs := newHashes()
	var done int64
	buf := make([]byte, 256*1024)
	var werr error
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(90 * time.Second)
			if _, werr = w.Write(buf[:n]); werr != nil {
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
	return atomicfile.Replace(part, r.Dest)
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

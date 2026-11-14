package tunnel

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/diskguard"
	"minedeck/internal/dl"
	"minedeck/internal/procutil"
	"minedeck/internal/zipguard"
)

// Provider 一个穿透服务商的适配器。
type Provider interface {
	ID() string
	// EnsureFrpc 确保该服务商的 frpc 客户端就绪，返回 exe 绝对路径。
	EnsureFrpc(con *procutil.Console) (string, error)
	// Launch 构造 frpc 的启动参数，以及要额外传给 frpc 进程的环境变量（"名=值"）。
	// t.Credential 此时是解开后的明文，只在内存里用，不能写进任何文件。
	Launch(t *Tunnel) (args, env []string, err error)
}

func providerOf(id string) Provider {
	switch id {
	case "openfrp":
		return openfrpProvider{}
	case "natfrp":
		return natfrpProvider{}
	case "custom":
		return customProvider{}
	}
	return nil
}

func frpcDir(provider string) string {
	return filepath.Join(app.RuntimesDir, "frpc", provider)
}

// findExe 在目录中查找 frpc 可执行文件。
func findExe(dir string) string {
	for _, cand := range []string{"frpc.exe", "frpc_windows_amd64.exe"} {
		p := filepath.Join(dir, cand)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if !e.IsDir() && isFrpcExeName(e.Name()) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// isFrpcExeName 文件名像不像 frpc 的 exe（findExe 和解压后的检查用同一套规则）。
func isFrpcExeName(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".exe") && strings.Contains(n, "frpc")
}

// extractZipFlat 解压 zip 并抹平目录层级（frp 发行包带一层目录），返回解压出来的文件名。
func extractZipFlat(zipPath, destDir string) ([]string, error) {
	zr, err := zipguard.Open(zipPath, zipguard.Default)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	lease, err := diskguard.Acquire(context.Background(), destDir, diskguard.Options{})
	if err != nil {
		return nil, err
	}
	defer lease.Close()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	var names []string
	for _, f := range zr.File {
		if err := lease.Check(0); err != nil {
			return names, err
		}
		if f.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(strings.ReplaceAll(f.Name, "\\", "/"))
		if base == "" || strings.Contains(base, "..") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return names, err
		}
		w, err := os.Create(filepath.Join(destDir, base))
		if err != nil {
			rc.Close()
			return names, err
		}
		_, cerr := io.Copy(lease.Writer(w), rc)
		rc.Close()
		if werr := w.Close(); cerr == nil {
			cerr = werr
		}
		if cerr != nil {
			return names, cerr
		}
		names = append(names, base)
	}
	return names, nil
}

// ---------- OpenFrp（基于 OpenFrp OPENAPI，接入条款要求显著署名） ----------

type openfrpProvider struct{}

// openfrpHTTPClient 只给 OpenFrp 的 frpc zip 下载使用。staticassets.naids.com 的腾讯 EdgeOne
// 会把 Go 默认 ClientHello 当机器人并返回 567；只宣告 TLS 1.3 会改变 ClientHello，实测可以过闸。
// 这是在绕过 CDN 机器人防护，不是普通的 TLS 兼容设置。EdgeOne 随时可能把「只报 TLS 1.3 的 Go 指纹」
// 也加进黑名单；失效时这里要重新判断，不能当成永久有效的固定配置。
// 不设 http.Client.Timeout：总时限仍由 EnsureFrpc 的 context 和 dl 的无进展看门狗控制。
var openfrpHTTPClient = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	TLSHandshakeTimeout:   15 * time.Second,
	ResponseHeaderTimeout: 60 * time.Second,
	MaxIdleConnsPerHost:   8,
	ForceAttemptHTTP2:     true, // 设置 TLSClientConfig 后 Go 不再自动升 h2，必须显式打开。
	TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS13},
}}

// openfrpSoftwareMeta 是 commonQuery?key=software 里和下载 frpc 有关的字段。
// latest_ver 是纯数字版本（0.68.0），只用于版本比较；下载目录是 latest_full
// （OF_0.68.0_…）。latest 是同一个目录名再包一层斜杠，不拿来拼地址。
type openfrpSoftwareMeta struct {
	Data struct {
		Latest     string `json:"latest"`
		LatestFull string `json:"latest_full"`
		LatestVer  string `json:"latest_ver"`
		Source     []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"source"`
	} `json:"data"`
}

// openfrpFrpcZip Windows amd64 定制版 frpc 的文件名（官方文档：windows 用 zip）。
const openfrpFrpcZip = "frpc_windows_amd64.zip"

// openfrpFrpcURLs 按官方文档拼下载地址：{source}/{latest_full}/frpc_windows_amd64.zip。
// latest_full 和下载源两端的斜杠去掉。不拼不带版本的地址，那个路径会 404。
func openfrpFrpcURLs(meta openfrpSoftwareMeta) ([]string, error) {
	full := strings.Trim(strings.TrimSpace(meta.Data.LatestFull), "/")
	if full == "" || len(meta.Data.Source) == 0 {
		return nil, fmt.Errorf("OpenFrp 客户端源信息为空")
	}
	urls := make([]string, 0, len(meta.Data.Source))
	for _, s := range meta.Data.Source {
		base := strings.TrimRight(strings.TrimSpace(s.Value), "/")
		if base == "" {
			continue
		}
		urls = append(urls, base+"/"+full+"/"+openfrpFrpcZip)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("OpenFrp 客户端源信息为空")
	}
	return urls, nil
}

func (openfrpProvider) ID() string { return "openfrp" }

func (openfrpProvider) EnsureFrpc(con *procutil.Console) (string, error) {
	dir := frpcDir("openfrp")
	// 已经放好的 frpc 直接用，不在这里比版本。latest_ver 和 latest_full 不是同一串，不能拿来互相比较。
	if exe := findExe(dir); exe != "" {
		return exe, nil
	}
	con.Append("[MineDeck] 下载 OpenFrp 定制版 frpc ...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var meta openfrpSoftwareMeta
	if err := dl.FetchJSON(ctx, []string{"https://api.openfrp.net/commonQuery/get?key=software"}, &meta); err != nil {
		return "", fmt.Errorf("获取 OpenFrp 客户端版本失败: %w", err)
	}
	urls, err := openfrpFrpcURLs(meta)
	if err != nil {
		return "", err
	}
	zipPath := filepath.Join(app.CacheDir, "frpc", "openfrp-frpc.zip")
	if err := dl.Fetch(ctx, dl.Request{URLs: urls, Dest: zipPath, MinSize: 1 << 20, Client: openfrpHTTPClient}, nil); err != nil {
		_ = os.MkdirAll(dir, 0o755) // 提示里叫人把 exe 放进这个文件夹，先建好（同樱花那条）
		return "", fmt.Errorf("下载 OpenFrp frpc 失败（可手动从 OpenFrp 下载 frpc_windows_amd64.zip，把 zip 里的 frpc_windows_amd64.exe 放入 %s 文件夹后重试）: %w", dir, err)
	}
	names, err := extractZipFlat(zipPath, dir)
	if err != nil {
		return "", err
	}
	return frpcAfterExtract(dir, names, fmt.Errorf("OpenFrp frpc 解压后未找到可执行文件"))
}

func (openfrpProvider) Launch(t *Tunnel) ([]string, []string, error) {
	// 简易启动：frpc -u <用户密钥> -p <隧道ID>（官方 ez_startup 机制）。
	// 已知限制：凭据在命令行参数里，本机进程列表看得到；服务商自己的 frpc 未必支持别的传法，暂不改。
	return []string{"-u", t.Credential, "-p", strings.TrimSpace(t.ProxyID)}, nil, nil
}

// ---------- 樱花 SakuraFrp ----------

type natfrpProvider struct{}

func (natfrpProvider) ID() string { return "natfrp" }

func (natfrpProvider) EnsureFrpc(con *procutil.Console) (string, error) {
	dir := frpcDir("natfrp")
	if exe := findExe(dir); exe != "" {
		return exe, nil
	}
	// 樱花未提供稳定的直链下载 API，引导用户手动放置（一次性操作）。
	// 放过又不见了的，多半是被杀毒软件删了（见 blocked.go）：程序分不出是没放过还是被删了，两种都说到。
	_ = os.MkdirAll(dir, 0o755)
	return "", fmt.Errorf(
		"首次使用需手动放置樱花 frpc：登录 SakuraFrp 管理面板 → 软件下载 → 下载「frpc」Windows amd64 版，"+
			"把 exe 放入 %s 文件夹后重试（仅需一次）。放过以后又不见了的，多半是被杀毒软件删了："+
			"到 Windows 安全中心的「保护历史记录」或杀毒软件的隔离区里恢复它", dir)
}

func (natfrpProvider) Launch(t *Tunnel) ([]string, []string, error) {
	// 官方启动语法：frpc -f <访问密钥>:<隧道ID>（同 OpenFrp，凭据在命令行参数里）
	return []string{"-f", t.Credential + ":" + strings.TrimSpace(t.ProxyID)}, nil, nil
}

// ---------- 自定义 frps（标准版 frp） ----------

type customProvider struct{}

func (customProvider) ID() string { return "custom" }

const frpVersion = "0.61.1"

// frpSHA256 为 frp_0.61.1_windows_amd64.zip 的官方 SHA-256，取自 fatedier/frp
// v0.61.1 Release 附带的 frp_sha256_checksums.txt。下载走国内加速镜像也必须
// 过这道校验，镜像被投毒/劫持时直接拒收（升级 frpVersion 时须同步更新）。
const frpSHA256 = "e0094cd0baf03d5ff9ce9739199406871ad8788cf51e766f00ad3a9e7a836f3a"

func (customProvider) EnsureFrpc(con *procutil.Console) (string, error) {
	dir := frpcDir("custom")
	if exe := findExe(dir); exe != "" {
		return exe, nil
	}
	con.Append("[MineDeck] 下载标准版 frp 客户端 ...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	file := fmt.Sprintf("frp_%s_windows_amd64.zip", frpVersion)
	ghPath := fmt.Sprintf("https://github.com/fatedier/frp/releases/download/v%s/%s", frpVersion, file)
	urls := dl.GHMirrors(ghPath) // 国内加速镜像优先，最后直连
	zipPath := filepath.Join(app.CacheDir, "frpc", file)
	if err := dl.Fetch(ctx, dl.Request{URLs: urls, Dest: zipPath, SHA256: frpSHA256, MinSize: 1 << 20}, nil); err != nil {
		return "", fmt.Errorf("下载 frp 客户端失败（可手动将 frpc.exe 放入 %s）: %w", dir, err)
	}
	names, err := extractZipFlat(zipPath, dir)
	if err != nil {
		return "", err
	}
	exe, err := frpcAfterExtract(dir, names, fmt.Errorf("frp 解压后未找到 frpc.exe"))
	if err != nil {
		return "", err
	}
	return exe, nil
}

// frpTokenEnv 自定义 frps 的 token 放在 frpc 进程的这个环境变量里。
const frpTokenEnv = "MINEDECK_FRP_TOKEN"

// tomlEscape 把每个字符都写成 TOML 字符串转义 \uXXXX / \UXXXXXXXX。frp 用 html/template 渲染配置文件，
// 环境变量原样代进去时 + & " ' < > 会被转成 HTML 实体，token 就对不上了；反斜杠和十六进制数字不会被转义，
// 渲染完再由 TOML 还原成原字符。frpc 0.61.1 实测可用（token 含这些符号、中文和 emoji 都能登录）。
func tomlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r > 0xFFFF {
			fmt.Fprintf(&b, `\U%08X`, r)
		} else {
			fmt.Fprintf(&b, `\u%04X`, r)
		}
	}
	return b.String()
}

func (customProvider) Launch(t *Tunnel) ([]string, []string, error) {
	// 生成标准 frp TOML 配置（loginFailExit=false：连不上持续重试而非退出）。
	// token 不落盘：配置里只写模板，frpc 读配置时从自己的环境变量取（frp 配置文件支持 {{ .Envs.变量名 }}）。
	// 模板放在第一行、早于任何用户填的内容：frp 按 HTML 模板渲染整份文件，前面的文字会影响转义的上下文。
	// 地址是配置里唯一原样写进去的用户文字：保存时已经只认域名和 IP，启动前再查一遍，兜住以前存下的（B-5）。
	// 以前存下的可能带着空格、方括号，按保存时的写法整理一遍，写进配置的就是查过的这个。
	addr := normServerAddr(t.ServerAddr)
	if err := checkServerAddr(addr); err != nil {
		return nil, nil, err
	}
	var cfg string
	var env []string
	if t.Credential != "" {
		cfg = "auth.token = \"{{ .Envs." + frpTokenEnv + " }}\"\n"
		env = []string{frpTokenEnv + "=" + tomlEscape(t.Credential)}
	}
	cfg += fmt.Sprintf(`serverAddr = %q
serverPort = %d
loginFailExit = false
`, addr, t.ServerPort)
	proto := "tcp"
	if t.Proto == "udp" { // 语音隧道：Simple Voice Chat 走 UDP
		proto = "udp"
	}
	cfg += fmt.Sprintf(`
[[proxies]]
name = "minedeck-%s"
type = %q
localIP = "127.0.0.1"
localPort = %d
remotePort = %d
`, t.ID, proto, t.LocalPort, t.RemotePort)
	dir := frpcDir("custom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	cfgPath := filepath.Join(dir, t.ID+".toml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		return nil, nil, err
	}
	return []string{"-c", cfgPath}, env, nil
}

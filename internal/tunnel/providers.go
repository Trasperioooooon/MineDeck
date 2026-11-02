package tunnel

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/procutil"
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
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".exe") &&
			strings.Contains(strings.ToLower(e.Name()), "frpc") {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// extractZipFlat 解压 zip 并抹平目录层级（frp 发行包带一层目录）。
func extractZipFlat(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(strings.ReplaceAll(f.Name, "\\", "/"))
		if base == "" || strings.Contains(base, "..") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.Create(filepath.Join(destDir, base))
		if err != nil {
			rc.Close()
			return err
		}
		_, cerr := io.Copy(w, rc)
		rc.Close()
		if werr := w.Close(); cerr == nil {
			cerr = werr
		}
		if cerr != nil {
			return cerr
		}
	}
	return nil
}

// ---------- OpenFrp（基于 OpenFrp OPENAPI，接入条款要求显著署名） ----------

type openfrpProvider struct{}

func (openfrpProvider) ID() string { return "openfrp" }

func (openfrpProvider) EnsureFrpc(con *procutil.Console) (string, error) {
	dir := frpcDir("openfrp")
	if exe := findExe(dir); exe != "" {
		return exe, nil
	}
	con.Append("[MineDeck] 下载 OpenFrp 定制版 frpc ...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var meta struct {
		Data struct {
			LatestVer string `json:"latest_ver"`
			Source    []struct {
				Label string `json:"label"`
				Value string `json:"value"`
			} `json:"source"`
		} `json:"data"`
	}
	if err := dl.FetchJSON(ctx, []string{"https://api.openfrp.net/commonQuery/get?key=software"}, &meta); err != nil {
		return "", fmt.Errorf("获取 OpenFrp 客户端版本失败: %w", err)
	}
	if meta.Data.LatestVer == "" || len(meta.Data.Source) == 0 {
		return "", fmt.Errorf("OpenFrp 客户端源信息为空")
	}
	var urls []string
	for _, s := range meta.Data.Source {
		base := strings.TrimSuffix(s.Value, "/")
		urls = append(urls,
			base+"/"+meta.Data.LatestVer+"/frpc_windows_amd64.zip",
			base+"/frpc_windows_amd64.zip")
	}
	zipPath := filepath.Join(app.CacheDir, "frpc", "openfrp-frpc.zip")
	if err := dl.Fetch(ctx, dl.Request{URLs: urls, Dest: zipPath, MinSize: 1 << 20}, nil); err != nil {
		return "", fmt.Errorf("下载 OpenFrp frpc 失败: %w", err)
	}
	if err := extractZipFlat(zipPath, dir); err != nil {
		return "", err
	}
	exe := findExe(dir)
	if exe == "" {
		return "", fmt.Errorf("OpenFrp frpc 解压后未找到可执行文件")
	}
	return exe, nil
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
	// 樱花未提供稳定的直链下载 API，引导用户手动放置（一次性操作）
	_ = os.MkdirAll(dir, 0o755)
	return "", fmt.Errorf(
		"首次使用需手动放置樱花 frpc：登录 SakuraFrp 管理面板 → 软件下载 → 下载「frpc」Windows amd64 版，"+
			"把 exe 放入 %s 目录后重试（仅需一次）", dir)
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
	if err := extractZipFlat(zipPath, dir); err != nil {
		return "", err
	}
	exe := findExe(dir)
	if exe == "" {
		return "", fmt.Errorf("frp 解压后未找到 frpc.exe")
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
	var cfg string
	var env []string
	if t.Credential != "" {
		cfg = "auth.token = \"{{ .Envs." + frpTokenEnv + " }}\"\n"
		env = []string{frpTokenEnv + "=" + tomlEscape(t.Credential)}
	}
	cfg += fmt.Sprintf(`serverAddr = %q
serverPort = %d
loginFailExit = false
`, t.ServerAddr, t.ServerPort)
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

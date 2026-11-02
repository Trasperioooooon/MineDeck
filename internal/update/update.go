// Package update 自动更新：检查 GitHub Releases，下载新版 exe 并通过
// update.bat 在退出后原子替换（国内经 gh 加速镜像兜底）。
package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/dl"
)

type Release struct {
	Tag         string `json:"tag"`
	Notes       string `json:"notes"`
	AssetURL    string `json:"-"`
	Size        int64  `json:"sizeBytes"`
	SHA256      string `json:"-"` // 官方发布的 exe SHA-256（自 checksums 资产直连 GitHub 解析）
	checksumURL string
}

// Check 查询最新 Release；repo 形如 "user/MineDeck"。
func Check(ctx context.Context, repo string) (*Release, bool, error) {
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	if repo == "" || !strings.Contains(repo, "/") {
		return nil, false, fmt.Errorf("未配置更新仓库（在全局设置填写，如 yourname/MineDeck）")
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	api := "https://api.github.com/repos/" + repo + "/releases/latest"
	if err := dl.FetchJSON(ctx, []string{api}, &rel); err != nil {
		return nil, false, fmt.Errorf("检查更新失败（GitHub API 不可达）: %w", err)
	}
	if rel.TagName == "" {
		return nil, false, fmt.Errorf("仓库还没有发布任何 Release")
	}
	r := &Release{Tag: rel.TagName, Notes: rel.Body}
	for _, a := range rel.Assets {
		low := strings.ToLower(a.Name)
		if strings.HasSuffix(low, ".exe") {
			r.AssetURL, r.Size = a.URL, a.Size
		}
		if low == "sha256sums" || strings.HasSuffix(low, ".sha256") {
			r.checksumURL = a.URL
		}
	}
	if r.AssetURL != "" && r.checksumURL != "" {
		r.SHA256 = fetchChecksum(ctx, r.checksumURL, "MineDeck.exe")
	}
	return r, newer(rel.TagName, app.Version), nil
}

// fetchChecksum 直连（绝不经镜像）拉取官方 checksums 资产并取出 filename 的 SHA-256。
// 校验和必须来自可信源：若走第三方镜像，镜像可同时伪造 exe 与其校验和，防护形同虚设。
func fetchChecksum(ctx context.Context, url, filename string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	resp, err := dl.Client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line) // 兼容 "<hash>  file" 与 "<hash> *file"
		if len(fields) >= 2 && strings.TrimPrefix(strings.TrimSpace(fields[len(fields)-1]), "*") == filename {
			return fields[0]
		}
	}
	return ""
}

// newer 比较 "v1.2.3" 风格版本号。数字相同时，正式版比带后缀的预发布版新
// （2.9.0 比 2.9.0-beta.1 新），装了测试版的人才能收到同号正式版的更新。
func newer(tag, current string) bool {
	pa := verParts(tag)
	pb := verParts(current)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return !isPre(tag) && isPre(current)
}

// isPre 报告版本号是否带预发布后缀（如 "2.9.0-beta.1"）。
func isPre(v string) bool { return strings.Contains(strings.TrimSpace(v), "-") }

func verParts(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		if i >= 3 {
			break
		}
		p, _, _ = strings.Cut(p, "-")
		n, _ := strconv.Atoi(p)
		out[i] = n
	}
	return out
}

// Apply 下载新版并写入换血脚本；调用方随后应触发程序退出。
func Apply(ctx context.Context, r *Release) error {
	if r.AssetURL == "" {
		return fmt.Errorf("该 Release 未附带 exe 文件")
	}
	if r.SHA256 == "" {
		// 无官方校验和则拒绝：避免经第三方镜像下载并执行未经校验的 exe（供应链/MITM RCE）
		return fmt.Errorf("无法获取该版本的官方校验和（SHA256SUMS 资产缺失或不可达），为安全起见已取消更新")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	newExe := filepath.Join(app.Base, "MineDeck.new.exe")
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	// 经镜像下载但以官方 SHA-256 强校验：镜像被投毒/中间人返回的伪 exe 会校验失败、绝不落地执行
	if err := dl.Fetch(cctx, dl.Request{
		URLs: dl.GHMirrors(r.AssetURL), Dest: newExe, MinSize: 4 << 20, SHA256: r.SHA256,
	}, nil); err != nil {
		return fmt.Errorf("下载新版本失败: %w", err)
	}
	// 轮询等待旧进程真正退出（而非固定延时），避免文件占用与双进程
	bat := fmt.Sprintf(`@echo off
cd /d "%s"
:wait
tasklist /FI "PID eq %d" 2>nul | find "%d" >nul && (ping 127.0.0.1 -n 2 >nul & goto wait)
move /y "MineDeck.new.exe" "%s" >nul
start "" "%s"
del "%%~f0"
`, app.Base, os.Getpid(), os.Getpid(), filepath.Base(exe), filepath.Base(exe))
	batPath := filepath.Join(app.Base, "update.bat")
	if err := os.WriteFile(batPath, []byte(bat), 0o755); err != nil {
		return err
	}
	return runDetached(batPath)
}

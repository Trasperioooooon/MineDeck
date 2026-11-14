// Package java 负责便携式 Java 运行时的自动下载与部署。
// 三级来源：清华 TUNA 镜像（zip 快）→ Adoptium 官方 API → Mojang 官方运行时（BMCLAPI 镜像，逐文件）。
// 运行时安装于程序目录 runtimes/java-{major}/，不修改系统环境变量。
package java

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"minedeck/internal/app"
	"minedeck/internal/diskguard"
	"minedeck/internal/dl"
	"minedeck/internal/mcsrc"
	"minedeck/internal/zipguard"
)

type Info struct {
	Major int    `json:"major"`
	Path  string `json:"path"` // java.exe 绝对路径
}

func Dir(major int) string {
	return filepath.Join(app.RuntimesDir, fmt.Sprintf("java-%d", major))
}

func Exe(major int) string { return filepath.Join(Dir(major), "bin", "java.exe") }

func IsInstalled(major int) bool {
	st, err := os.Stat(Exe(major))
	return err == nil && !st.IsDir()
}

// Installed 扫描已部署的运行时。
func Installed() []Info {
	ents, err := os.ReadDir(app.RuntimesDir)
	if err != nil {
		return nil
	}
	var out []Info
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "java-") {
			continue
		}
		major, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "java-"))
		if err != nil {
			continue
		}
		if IsInstalled(major) {
			out = append(out, Info{Major: major, Path: Exe(major)})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Major < out[b].Major })
	return out
}

var ensureMu sync.Mutex

// Ensure 确保指定 major 版本的便携 Java 就绪，返回 java.exe 绝对路径。
func Ensure(ctx context.Context, major int, logf func(string, ...any), prog dl.Progress) (string, error) {
	ensureMu.Lock()
	defer ensureMu.Unlock()
	if IsInstalled(major) {
		return Exe(major), nil
	}
	logf("需要 Java %d，开始下载便携式运行时（不影响系统环境）", major)
	var errs []error

	if err := fromTUNA(ctx, major, logf, prog); err == nil {
		return finalize(major)
	} else {
		errs = append(errs, fmt.Errorf("清华镜像: %w", err))
		logf("清华 TUNA 镜像不可用（%v），切换 Adoptium 官方源", err)
	}
	if err := fromAdoptium(ctx, major, logf, prog); err == nil {
		return finalize(major)
	} else {
		errs = append(errs, fmt.Errorf("Adoptium: %w", err))
		logf("Adoptium 官方源失败（%v），切换 Mojang 官方运行时（BMCLAPI 镜像）", err)
	}
	if err := fromMojang(ctx, major, logf, prog); err == nil {
		return finalize(major)
	} else {
		errs = append(errs, fmt.Errorf("Mojang 运行时: %w", err))
	}
	return "", fmt.Errorf("Java %d 全部下载源均失败: %w", major, errors.Join(errs...))
}

func finalize(major int) (string, error) {
	if !IsInstalled(major) {
		return "", fmt.Errorf("Java %d 部署后未找到 bin\\java.exe", major)
	}
	return Exe(major), nil
}

// ---------- 来源 1：清华 TUNA Adoptium 镜像 ----------

func fromTUNA(ctx context.Context, major int, logf func(string, ...any), prog dl.Progress) error {
	base := fmt.Sprintf("https://mirrors.tuna.tsinghua.edu.cn/Adoptium/%d/jre/x64/windows/", major)
	b, err := dl.FetchBytes(ctx, []string{base})
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`href="(OpenJDK[^"]*jre[^"]*\.zip)"`)
	ms := re.FindAllStringSubmatch(string(b), -1)
	if len(ms) == 0 {
		return fmt.Errorf("这个镜像里没有 Java %d 的运行时压缩包", major)
	}
	name := ms[len(ms)-1][1] // 目录按版本升序，取最新
	sha := ""
	if sb, err := dl.FetchBytes(ctx, []string{base + name + ".sha256.txt"}); err == nil {
		if fields := strings.Fields(string(sb)); len(fields) > 0 {
			sha = fields[0]
		}
	}
	logf("从清华镜像下载 %s", name)
	zipPath := filepath.Join(app.CacheDir, "java", name)
	if err := dl.Fetch(ctx, dl.Request{
		URLs: []string{base + name}, Dest: zipPath, SHA256: sha, MinSize: 10 << 20,
	}, prog); err != nil {
		return err
	}
	return extractZip(zipPath, Dir(major))
}

// ---------- 来源 2：Adoptium 官方 API ----------

func fromAdoptium(ctx context.Context, major int, logf func(string, ...any), prog dl.Progress) error {
	for _, imageType := range []string{"jre", "jdk"} {
		api := fmt.Sprintf(
			"https://api.adoptium.net/v3/assets/latest/%d/hotspot?os=windows&architecture=x64&image_type=%s&vendor=eclipse",
			major, imageType)
		var assets []struct {
			Binary struct {
				Package struct {
					Link     string `json:"link"`
					Checksum string `json:"checksum"`
					Name     string `json:"name"`
				} `json:"package"`
			} `json:"binary"`
		}
		if err := dl.FetchJSON(ctx, []string{api}, &assets); err != nil {
			continue
		}
		for _, a := range assets {
			p := a.Binary.Package
			if p.Link == "" || !strings.HasSuffix(p.Name, ".zip") {
				continue
			}
			logf("从 Adoptium 官方源下载 %s", p.Name)
			zipPath := filepath.Join(app.CacheDir, "java", p.Name)
			if err := dl.Fetch(ctx, dl.Request{
				URLs: []string{p.Link}, Dest: zipPath, SHA256: p.Checksum, MinSize: 10 << 20,
			}, prog); err != nil {
				return err
			}
			return extractZip(zipPath, Dir(major))
		}
	}
	return fmt.Errorf("Adoptium 没有 Java %d 的 Windows 64 位版本", major)
}

// ---------- 来源 3：Mojang 官方 Java 运行时（BMCLAPI 可镜像，逐文件下载） ----------

const mojangRuntimeAll = "https://launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

func fromMojang(ctx context.Context, major int, logf func(string, ...any), prog dl.Progress) error {
	var all map[string]map[string][]struct {
		Manifest struct {
			URL string `json:"url"`
		} `json:"manifest"`
		Version struct {
			Name string `json:"name"`
		} `json:"version"`
	}
	if err := dl.FetchJSON(ctx, mcsrc.URLPair(mojangRuntimeAll), &all); err != nil {
		return err
	}
	comps, ok := all["windows-x64"]
	if !ok {
		return fmt.Errorf("Mojang 的 Java 下载列表里没有 Windows 64 位（windows-x64）")
	}
	manifestURL, best := "", ""
	for _, entries := range comps {
		for _, e := range entries {
			if runtimeMajor(e.Version.Name) == major && e.Version.Name > best {
				best, manifestURL = e.Version.Name, e.Manifest.URL
			}
		}
	}
	if manifestURL == "" {
		return fmt.Errorf("Mojang 未提供 Java %d 运行时", major)
	}
	logf("使用 Mojang 官方运行时 %s（逐文件下载）", best)

	var mf struct {
		Files map[string]struct {
			Type      string `json:"type"`
			Downloads struct {
				Raw struct {
					URL  string `json:"url"`
					SHA1 string `json:"sha1"`
					Size int64  `json:"size"`
				} `json:"raw"`
			} `json:"downloads"`
		} `json:"files"`
	}
	if err := dl.FetchJSON(ctx, mcsrc.URLPair(manifestURL), &mf); err != nil {
		return err
	}
	dest := Dir(major)
	type job struct {
		rel, url, sha1 string
		size           int64
	}
	var jobs []job
	var total int64
	for rel, f := range mf.Files {
		switch f.Type {
		case "directory":
			if err := os.MkdirAll(filepath.Join(dest, filepath.FromSlash(rel)), 0o755); err != nil {
				return err
			}
		case "file":
			if f.Downloads.Raw.URL == "" {
				continue
			}
			jobs = append(jobs, job{rel, f.Downloads.Raw.URL, f.Downloads.Raw.SHA1, f.Downloads.Raw.Size})
			total += f.Downloads.Raw.Size
		}
	}
	var doneBytes atomic.Int64
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	// 首个错误用互斥保护而非 atomic.Value：dl.Fetch 可返回异构 error 具体类型
	// （*errorString / context.deadlineExceededError / *fmt.wrapError…），并发存入 atomic.Value 会 panic 整个进程
	var errMu sync.Mutex
	var firstErr error
	setErr := func(e error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = e
		}
		errMu.Unlock()
	}
	getErr := func() error { errMu.Lock(); defer errMu.Unlock(); return firstErr }
	for _, j := range jobs {
		if getErr() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			p := filepath.Join(dest, filepath.FromSlash(j.rel))
			err := dl.Fetch(ctx, dl.Request{URLs: mcsrc.URLPair(j.url), Dest: p, SHA1: j.sha1}, nil)
			if err != nil {
				setErr(err)
				return
			}
			if prog != nil {
				prog(doneBytes.Add(j.size), total)
			}
		}(j)
	}
	wg.Wait()
	if e := getErr(); e != nil {
		return e
	}
	return nil
}

// runtimeMajor 解析 "1.8.0_51" / "16.0.1.9.1" / "21.0.3" 形式的主版本号。
func runtimeMajor(name string) int {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == '+' })
	if len(parts) == 0 {
		return 0
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	if n == 1 && len(parts) > 1 { // 1.8.x → 8
		if m, err := strconv.Atoi(parts[1]); err == nil {
			return m
		}
	}
	return n
}

// extractZip 解压 JRE zip 至 destDir，自动剥离顶层目录，含 zip-slip 防护。
func extractZip(zipPath, destDir string) error {
	zr, err := zipguard.Open(zipPath, zipguard.Default)
	if err != nil {
		return fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer zr.Close()
	lease, err := diskguard.Acquire(context.Background(), destDir, diskguard.Options{AllowUnknown: true})
	if err != nil {
		return err
	}
	defer lease.Close()

	// 检测统一顶层目录
	top := ""
	uniform := true
	for _, f := range zr.File {
		name := strings.TrimPrefix(strings.ReplaceAll(f.Name, "\\", "/"), "/")
		if name == "" {
			continue
		}
		first, _, _ := strings.Cut(name, "/")
		if top == "" {
			top = first
		} else if first != top {
			uniform = false
			break
		}
	}
	if !uniform {
		top = ""
	}

	if err := os.RemoveAll(destDir); err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	cleanDest := filepath.Clean(destDir)
	for _, f := range zr.File {
		if err := lease.Check(0); err != nil {
			return err
		}
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if top != "" {
			name = strings.TrimPrefix(strings.TrimPrefix(name, top), "/")
		}
		if name == "" {
			continue
		}
		target := filepath.Join(cleanDest, filepath.FromSlash(name))
		if !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("zip 内含非法路径: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, cerr := io.Copy(lease.Writer(w), rc)
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

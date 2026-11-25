package java

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/errhint"
	"minedeck/internal/procutil"
)

// ScannedJava 本机已安装（非本工具便携部署）的 Java。
type ScannedJava struct {
	Path    string `json:"path"`
	Major   int    `json:"major"`
	Version string `json:"version"`
}

var (
	scanMu sync.Mutex
	cached []ScannedJava
	loaded bool
)

func cacheFile() string { return filepath.Join(app.CacheDir, "javas.json") }

// Cached 返回上次扫描结果（进程内存 + 磁盘缓存）。
func Cached() []ScannedJava {
	scanMu.Lock()
	defer scanMu.Unlock()
	if !loaded {
		loaded = true
		if b, err := os.ReadFile(cacheFile()); err == nil {
			_ = json.Unmarshal(b, &cached)
		}
	}
	out := make([]ScannedJava, len(cached))
	copy(out, cached)
	return out
}

func saveCache(list []ScannedJava) {
	scanMu.Lock()
	cached, loaded = list, true
	scanMu.Unlock()
	if b, err := json.MarshalIndent(list, "", "  "); err == nil {
		_ = os.WriteFile(cacheFile(), b, 0o644)
	}
}

// Scan 扫描本机 Java（注册表或系统的 Java 目录 / JAVA_HOME / PATH / 常见安装目录），验证版本后缓存。
func Scan() []ScannedJava {
	seen := map[string]string{} // lower(path) -> 原始路径
	add := func(p string) {
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		if skipJava(p) {
			return
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			p = homeExe(p)
		}
		if !strings.EqualFold(filepath.Base(p), exeName) {
			return
		}
		if _, err := os.Stat(p); err != nil {
			return
		}
		// 排除本工具自己的便携运行时
		if strings.HasPrefix(strings.ToLower(p), strings.ToLower(app.RuntimesDir)) {
			return
		}
		seen[seenKey(p)] = p
	}

	// 1. 系统登记的（Windows 是注册表；按平台，见 scan_windows.go / scan_darwin.go）
	registeredJavas(add)

	// 2. JAVA_HOME 与 PATH
	add(os.Getenv("JAVA_HOME"))
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		add(filepath.Join(dir, exeName))
	}

	// 3. 常见安装目录（按平台）
	commonJavaDirs(add)

	// 4. 并发验证版本
	type result struct {
		j  ScannedJava
		ok bool
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	results := make(chan result, len(seen))
	for _, p := range seen {
		wg.Add(1)
		sem <- struct{}{}
		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()
			if ver, major, ok := probeJava(path); ok {
				results <- result{ScannedJava{Path: path, Major: major, Version: ver}, true}
			}
		}(p)
	}
	wg.Wait()
	close(results)
	var list []ScannedJava
	for r := range results {
		if r.ok {
			list = append(list, r.j)
		}
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].Major != list[b].Major {
			return list[a].Major > list[b].Major
		}
		return list[a].Path < list[b].Path
	})
	saveCache(list)
	return list
}

var verRe = regexp.MustCompile(`version "([^"]+)"`)

// probeJava 运行 java -version 验证并解析版本。
func probeJava(path string) (version string, major int, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-version")
	procutil.HideWindow(cmd)
	out, err := cmd.CombinedOutput() // -version 输出在 stderr
	if err != nil {
		return "", 0, false
	}
	m := verRe.FindSubmatch(out)
	if m == nil {
		return "", 0, false
	}
	version = string(m[1])
	major = runtimeMajor(version)
	return version, major, major > 0
}

// AddManual 手动添加一个 Java 路径（验证后加入缓存）。
func AddManual(path string) (*ScannedJava, error) {
	p := filepath.Clean(path)
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		p = homeExe(p)
	}
	// 只允许指向 java 可执行文件本身，避免把任意 exe 交给 probeJava 执行
	if !IsExeName(filepath.Base(p)) {
		return nil, fmt.Errorf("请选择 %s（或 Java 的安装文件夹）", exeName)
	}
	ver, major, ok := probeJava(p)
	if !ok {
		return nil, errhint.Wrap(fmt.Errorf("无法识别该路径的 Java（java -version 执行失败）: %s", p), "这个路径下的 Java 运行不了，请确认选的 Java 完整可用")
	}
	j := ScannedJava{Path: p, Major: major, Version: ver}
	list := Cached()
	for _, e := range list {
		if strings.EqualFold(e.Path, p) {
			return &j, nil
		}
	}
	list = append(list, j)
	saveCache(list)
	return &j, nil
}

// Resolve 返回满足 major 的 java.exe：本机已装 > 便携已装 > 自动下载。
func Resolve(ctx context.Context, major int, logf func(string, ...any), prog func(done, total int64)) (string, error) {
	for _, j := range Cached() {
		if j.Major == major {
			if _, err := os.Stat(j.Path); err == nil {
				logf("使用本机已安装的 Java %d（%s）: %s", major, j.Version, j.Path)
				return j.Path, nil
			}
		}
	}
	return Ensure(ctx, major, logf, prog)
}

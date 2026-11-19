// Package java 负责便携式 Java 运行时的自动下载与部署。
// 三级来源：清华 TUNA 镜像（zip 快）→ Adoptium 官方 API → Mojang 官方运行时（BMCLAPI 镜像，逐文件）。
// 运行时安装于程序目录 runtimes/java-{major}/，不修改系统环境变量。
package java

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
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

// incompleteMark 「这套运行时还没装完」的记号：和 java-N 文件夹并排的一个空文件（runtimes\java-N.incomplete）。
// Ensure 动手装之前放上，整套装完才拿掉。放在文件夹外面，是因为解压前会把文件夹整个删掉重来。
func incompleteMark(major int) string { return Dir(major) + ".incomplete" }

// IsInstalled 这个大版本的便携 Java 装好了没有。
//
// 只看 bin\java.exe 在不在是不够的：解压到一半磁盘不够、逐文件下载断在中间、程序中途被结束，都会留下
// 「java.exe 在、别的文件不全」的半截。以前这种半截下次会被当成装好了直接拿去开服，报的是 JVM 自己的错，
// 也不会再重新下载，只能手动删文件夹。所以记号还在的不算装好，Ensure 会重装。
// 这道记号出现之前装好的运行时没有记号，照旧算装好。
func IsInstalled(major int) bool {
	st, err := os.Stat(Exe(major))
	if err != nil || st.IsDir() {
		return false
	}
	if _, err := os.Lstat(incompleteMark(major)); err == nil {
		return false
	}
	return true
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
	// 先放「还没装完」的记号，整套装完（finalize）才拿掉：中途失败或程序被结束，留下的半截不会被当成装好了
	if err := os.MkdirAll(app.RuntimesDir, 0o755); err != nil {
		return "", fmt.Errorf("准备安装 Java %d 失败：%w", major, err)
	}
	if err := os.WriteFile(incompleteMark(major), nil, 0o644); err != nil {
		return "", fmt.Errorf("准备安装 Java %d 失败：%w", major, err)
	}
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
	if st, err := os.Stat(Exe(major)); err != nil || st.IsDir() {
		return "", fmt.Errorf("Java %d 装好后没找到 bin\\java.exe", major)
	}
	if err := os.Remove(incompleteMark(major)); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("Java %d 装好了，但去不掉「还没装完」的记号：%w", major, err)
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
	return extractZipCtx(ctx, zipPath, Dir(major), logf)
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
			return extractZipCtx(ctx, zipPath, Dir(major), logf)
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

// dirSize 文件夹里普通文件的大小合计（不跟随链接，读不了的跳过）；不存在算 0。
func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// extractDiskFree 查磁盘余量的函数；nil 用系统的。测试用它让余量在解压中途变得不够。
var extractDiskFree func(string) (int64, error)

// extractZip 同 extractZipCtx，不能取消、排队时不出声。
func extractZip(zipPath, destDir string) error {
	return extractZipCtx(context.Background(), zipPath, destDir, nil)
}

// extractZipCtx 解压 JRE zip 至 destDir，自动剥离顶层目录，含 zip-slip 防护。
// 解到一半失败的，把已经解出来的删掉再返回：不留半截。
//
// 解压要排队拿这个盘的写入名额：同一个盘上正在做几分钟的备份、还原、地图暂存时，要等它做完。以前等的时候取消不了
// （用的是 context.Background()），任务日志里也看不出在等什么，新建服务器就停在「准备 Java」不动（2026-11-16 复审记下的）。
// 现在跟着 ctx 走：排队时、解压途中取消都会停下（解出来的照旧删掉）；要排队时用 logf 说一声。
func extractZipCtx(ctx context.Context, zipPath, destDir string, logf func(string, ...any)) (err error) {
	zr, err := zipguard.Open(zipPath, zipguard.Default)
	if err != nil {
		return fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer zr.Close()
	// 一开头就按解开以后的总大小要余量。原来只要求还剩保留的那 256 MiB：剩三四百兆的盘开头放行，
	// 旧文件夹删了、解到一半才报空间不够（解压过程中每个文件之前、每写 64 MiB 都会再查）
	// 下面会先把旧的文件夹整个删掉（重装装了一半的运行时就是这样），它占着的那部分算回来
	var total int64
	for _, f := range zr.File {
		if n := int64(f.UncompressedSize64); n > 0 && total <= math.MaxInt64-n {
			total += n
		}
	}
	need := max(total-dirSize(destDir), 0)
	opt := diskguard.Options{AllowUnknown: true, Need: need, Query: extractDiskFree}
	if logf != nil {
		opt.OnWait = func() {
			logf("这个盘上正有别的大批量写入在进行（备份、还原、导入地图这类），等它做完再解压 Java；不想等可以取消这个任务")
		}
	}
	lease, err := diskguard.Acquire(ctx, destDir, opt)
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
	// 从这里起失败就把解出来的删掉。bin\java.exe 在压缩包里排得很靠前，最大的 lib\modules 排在后面：
	// 留着半截既占着本来就不够的空间，看上去又像是装好了
	defer func() {
		if err != nil {
			_ = os.RemoveAll(destDir)
		}
	}()
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

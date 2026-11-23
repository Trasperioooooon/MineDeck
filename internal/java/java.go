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
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/diskguard"
	"minedeck/internal/dl"
	"minedeck/internal/mcsrc"
	"minedeck/internal/winname"
	"minedeck/internal/zipguard"
)

type Info struct {
	Major int    `json:"major"`
	Path  string `json:"path"` // java.exe 绝对路径
}

func Dir(major int) string {
	return filepath.Join(app.RuntimesDir, fmt.Sprintf("java-%d", major))
}

func Exe(major int) string { return homeExe(Dir(major)) }

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
	if err := autoInstallErr(major); err != nil { // Mac 版还没接上下载源（见 scan_darwin.go）
		return "", err
	}
	logf("需要 Java %d，开始下载便携式运行时（不影响系统环境）", major)
	// 先放「还没装完」的记号，整套装完（finalize）才拿掉：中途失败或程序被结束，留下的半截不会被当成装好了
	if err := os.MkdirAll(app.RuntimesDir, 0o755); err != nil {
		return "", fmt.Errorf("准备安装 Java %d 失败：%w", major, err)
	}
	if err := os.WriteFile(incompleteMark(major), nil, 0o644); err != nil {
		return "", fmt.Errorf("准备安装 Java %d 失败：%w", major, err)
	}
	errs, ok := fromAdoptiumBuilds(ctx, major, logf, prog)
	if ok {
		return finalize(major)
	}
	if err := fromMojang(ctx, major, logf, prog); err == nil {
		return finalize(major)
	} else {
		errs = append(errs, fmt.Errorf("Mojang 运行时: %w", err))
	}
	return "", fmt.Errorf("Java %d 全部下载源均失败: %w", major, errors.Join(errs...))
}

// fromAdoptiumBuilds 前两个来源：清华镜像（下载源选「官方」时不用），再是 Adoptium 官方源。两条路都要问 Adoptium 的接口
// （清华那条要官方校验值），前面已经确认接口连不上，后面那条就不再走、直接改用 Mojang 运行时，免得同一个主机再空等一轮（A35）。
// 装好了返回 ok；没装成返回每条路没走通的原因。
func fromAdoptiumBuilds(ctx context.Context, major int, logf func(string, ...any), prog dl.Progress) (errs []error, ok bool) {
	if !app.OfficialOnly() {
		err := fromTUNA(ctx, major, logf, prog)
		if err == nil {
			return nil, true
		}
		errs = append(errs, fmt.Errorf("清华镜像: %w", err))
		if errors.Is(err, errAdoptiumUnreachable) {
			logf("Adoptium 的接口连不上，清华镜像和 Adoptium 官方源都用不了，直接改用 Mojang 官方运行时（BMCLAPI 镜像）")
			return errs, false
		}
		logf("清华镜像这条路没走通（%v），切换 Adoptium 官方源", err)
	}
	err := fromAdoptium(ctx, major, logf, prog)
	if err == nil {
		return nil, true
	}
	errs = append(errs, fmt.Errorf("Adoptium: %w", err))
	if errors.Is(err, errAdoptiumUnreachable) {
		logf("Adoptium 的接口连不上，直接改用 Mojang 官方运行时（BMCLAPI 镜像）")
	} else {
		logf("Adoptium 官方源失败（%v），切换 Mojang 官方运行时（BMCLAPI 镜像）", err)
	}
	return errs, false
}

func finalize(major int) (string, error) {
	if st, err := os.Stat(Exe(major)); err != nil || st.IsDir() {
		return "", fmt.Errorf("Java %d 装好后没找到 %s", major, filepath.Join("bin", exeName))
	}
	if err := os.Remove(incompleteMark(major)); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("Java %d 装好了，但去不掉「还没装完」的记号：%w", major, err)
	}
	return Exe(major), nil
}

// ---------- 来源 1：清华 TUNA Adoptium 镜像 ----------

var (
	tunaAdoptiumBase = "https://mirrors.tuna.tsinghua.edu.cn/Adoptium"
	adoptiumAPIBase  = "https://api.adoptium.net/v3/assets"
	javaMinZipSize   = int64(10 << 20)
)

func safeJavaZipName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".zip") && !strings.ContainsAny(name, `/\`) && winname.OK(name)
}

// javaZipLess 按文件名里的版本号比新旧（如 17.0.9 < 17.0.14、8u92 < 8u442），避免字母序把 .9 排在 .14 后面。
// 版本段和构建号分开比：17.0.21.1_2 比 17.0.21_9 新，21.0.5_11 比 21_35 新。只拿数字段一路比会把这两对判反。
func javaZipLess(a, b string) bool {
	va, ba := javaZipVersion(a)
	vb, bb := javaZipVersion(b)
	for i := 0; i < len(va) || i < len(vb); i++ {
		x, y := 0, 0
		if i < len(va) {
			x = va[i]
		}
		if i < len(vb) {
			y = vb[i]
		}
		if x != y {
			return x < y
		}
	}
	if ba != bb {
		return ba < bb
	}
	return a < b
}

// javaZipVersion 从 Adoptium 的文件名里拆出版本段和构建号：
// …hotspot_17.0.21.1_2.zip → [17 0 21 1], 2；…hotspot_8u442b06.zip → [8 442], 6；…hotspot_21_35.zip → [21], 35。
func javaZipVersion(name string) ([]int, int) {
	s := strings.TrimSuffix(strings.ToLower(name), ".zip")
	if i := strings.LastIndex(s, "hotspot_"); i >= 0 {
		s = s[i+len("hotspot_"):]
	}
	ver, build := s, ""
	if i := strings.LastIndexAny(s, "_b"); i >= 0 && allDigits(s[i+1:]) {
		ver, build = s[:i], s[i+1:] // 切出来的不是纯数字（没有 hotspot_ 前缀的怪名字）就整段当版本
	}
	b := 0
	if nums := extractNums(build); len(nums) > 0 {
		b = nums[0]
	}
	return extractNums(ver), b
}

func extractNums(s string) []int {
	var out []int
	cur := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			if cur < 0 {
				cur = int(c - '0')
			} else if cur <= (math.MaxInt-9)/10 {
				cur = cur*10 + int(c-'0')
			}
		} else if cur >= 0 {
			out = append(out, cur)
			cur = -1
		}
	}
	if cur >= 0 {
		out = append(out, cur)
	}
	return out
}

func fromTUNA(ctx context.Context, major int, logf func(string, ...any), prog dl.Progress) error {
	base := fmt.Sprintf("%s/%d/jre/x64/windows/", strings.TrimRight(tunaAdoptiumBase, "/"), major)
	b, err := dl.FetchBytes(ctx, []string{base})
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`href="(OpenJDK[^"]*jre[^"]*\.zip)"`)
	ms := re.FindAllStringSubmatch(string(b), -1)
	var names []string
	seen := map[string]bool{}
	for _, m := range ms {
		cand := m[1]
		if safeJavaZipName(cand) && !seen[cand] {
			seen[cand] = true
			names = append(names, cand)
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("这个镜像里没有 Java %d 的运行时压缩包", major)
	}
	sort.Slice(names, func(i, j int) bool { return javaZipLess(names[i], names[j]) })
	// 校验值只认 Adoptium 官方 API 对得上这一个文件名的 SHA-256，不采用镜像站自证的 .sha256.txt。
	// 官方接口一共只问一次（两个地址、各有短时限），连不上就直接结束，不按文件名一个个重试。
	sums, err := adoptiumChecksums(ctx, major)
	if err != nil {
		logf("取不到 Adoptium 公布的官方 SHA-256（%v），不用清华镜像上的压缩包", err)
		return err
	}
	name, sha := "", ""
	for i := len(names) - 1; i >= 0; i-- {
		if sum, ok := sums[names[i]]; ok {
			name, sha = names[i], sum
			break
		}
	}
	if sha == "" {
		latest := names[len(names)-1]
		logf("清华镜像上的 %s 对不上 Adoptium 公布的官方 SHA-256，不在没有官方校验值的情况下使用这份压缩包", latest)
		return fmt.Errorf("清华镜像的 %s 没有匹配的官方校验值", latest)
	}
	logf("按 Adoptium 公布的 SHA-256 从清华镜像下载 %s", name)
	zipPath := filepath.Join(app.CacheDir, "java", name)
	if err := dl.Fetch(ctx, dl.Request{
		URLs: []string{base + name}, Dest: zipPath, SHA256: sha, MinSize: javaMinZipSize,
	}, prog); err != nil {
		return err
	}
	return extractZipCtx(ctx, zipPath, Dir(major), logf)
}

// ---------- 来源 2：Adoptium 官方 API ----------

func fromAdoptium(ctx context.Context, major int, logf func(string, ...any), prog dl.Progress) error {
	apiRoot := strings.TrimRight(adoptiumAPIBase, "/")
	tunaRoot := strings.TrimRight(tunaAdoptiumBase, "/")
	for _, imageType := range []string{"jre", "jdk"} {
		api := fmt.Sprintf(
			"%s/latest/%d/hotspot?os=windows&architecture=x64&image_type=%s&vendor=eclipse",
			apiRoot, major, imageType)
		var assets []struct {
			Binary struct {
				Package struct {
					Link     string `json:"link"`
					Checksum string `json:"checksum"`
					Name     string `json:"name"`
				} `json:"package"`
			} `json:"binary"`
		}
		if err := fetchAdoptiumJSON(ctx, api, &assets); err != nil {
			if adoptiumDown(err) { // 接口连不上：jdk 那一问是同一个主机，不再空等
				return fmt.Errorf("%w：%v", errAdoptiumUnreachable, err)
			}
			continue
		}
		for _, a := range assets {
			p := a.Binary.Package
			if p.Link == "" || !safeJavaZipName(p.Name) {
				continue
			}
			if !looksLikeSHA256(p.Checksum) {
				logf("Adoptium 没有给出 %s 的 SHA-256，跳过这一条", p.Name)
				continue
			}
			urls := []string{p.Link}
			if !app.OfficialOnly() {
				// 同一文件名先试清华。校验值仍是 Adoptium 公布的，对不上就换官方地址（多半在 GitHub）。
				// 没有把华为云、阿里云写进来：2026-11-20 抽查时，缺文件的地址也返回 200 的网页，不能当镜像。
				urls = []string{
					fmt.Sprintf("%s/%d/%s/x64/windows/%s", tunaRoot, major, imageType, p.Name),
					p.Link,
				}
				logf("按 Adoptium 公布的 SHA-256 下载 %s（先试清华镜像上的同名文件）", p.Name)
			} else {
				logf("从 Adoptium 官方源下载 %s", p.Name)
			}
			zipPath := filepath.Join(app.CacheDir, "java", p.Name)
			if err := dl.Fetch(ctx, dl.Request{
				URLs: urls, Dest: zipPath, SHA256: p.Checksum, MinSize: javaMinZipSize,
			}, prog); err != nil {
				return err
			}
			return extractZipCtx(ctx, zipPath, Dir(major), logf)
		}
	}
	return fmt.Errorf("Adoptium 没有 Java %d 的 Windows 64 位版本", major)
}

// looksLikeSHA256 是不是 64 位十六进制。镜像页面、HTML 404 不能拿来当校验值。
func looksLikeSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// errAdoptiumUnreachable Adoptium 的接口连不上（连接被拒、DNS 解析失败、超时，见 adoptiumDown）。
// 清华那条路因此不用；Ensure 看到它就不再走同一个接口的 Adoptium 官方源，免得再空等一轮。
var errAdoptiumUnreachable = errors.New("连不上 Adoptium 的接口")

// adoptiumIndexWait 向 Adoptium 的接口要清单（校验值、下载地址）时每一问最多等多久。
// 清单很小；国内连不上 api.adoptium.net 时不该让开服白等几分钟（普通下载是两轮、每轮 30 秒）。
var adoptiumIndexWait = 10 * time.Second

// fetchAdoptiumJSON 向 Adoptium 的接口问一个清单，最多等 adoptiumIndexWait。
func fetchAdoptiumJSON(ctx context.Context, u string, out any) error {
	cctx, cancel := context.WithTimeout(ctx, adoptiumIndexWait)
	defer cancel()
	return dl.FetchJSONHTTPS(cctx, []string{u}, out)
}

// adoptiumDown 这次没问到是不是因为「连不上」：连接被拒、DNS 解析失败、超时这类网络错误（含上面的时限到了）。
// 回了话的（404、5xx、回的不是 JSON）不算：接口还在，换个参数（jre 换 jdk）可能就有。
func adoptiumDown(err error) bool {
	var ne net.Error
	return errors.As(err, &ne)
}

// adoptiumChecksums 向 Adoptium API 要「文件名 → 官方 SHA-256」。
// 先看当前发布，再看最近几份正式版：清华镜像经常比「最新」晚一个小版本，
// 用的时候必须对上同一个文件名，不能拿另一份的校验值去套。
func adoptiumChecksums(ctx context.Context, major int) (map[string]string, error) {
	apiRoot := strings.TrimRight(adoptiumAPIBase, "/")
	sums := map[string]string{}
	add := func(name, sum string) {
		if name != "" && looksLikeSHA256(sum) {
			sums[name] = strings.ToLower(sum)
		}
	}

	latest := fmt.Sprintf(
		"%s/latest/%d/hotspot?os=windows&architecture=x64&image_type=jre&vendor=eclipse",
		apiRoot, major)
	var current []struct {
		Binary struct {
			Package struct {
				Checksum string `json:"checksum"`
				Name     string `json:"name"`
			} `json:"package"`
		} `json:"binary"`
	}
	errLatest := fetchAdoptiumJSON(ctx, latest, &current)
	if errLatest == nil {
		for _, a := range current {
			add(a.Binary.Package.Name, a.Binary.Package.Checksum)
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if adoptiumDown(errLatest) { // 第二问是同一个主机，不再空等
		return nil, fmt.Errorf("%w：%v", errAdoptiumUnreachable, errLatest)
	}

	page := fmt.Sprintf(
		"%s/feature_releases/%d/ga?os=windows&architecture=x64&image_type=jre&vendor=eclipse&page=0&page_size=20",
		apiRoot, major)
	var releases []struct {
		Binaries []struct {
			Package struct {
				Checksum string `json:"checksum"`
				Name     string `json:"name"`
			} `json:"package"`
		} `json:"binaries"`
	}
	errPage := fetchAdoptiumJSON(ctx, page, &releases)
	if errPage == nil {
		for _, rel := range releases {
			for _, b := range rel.Binaries {
				add(b.Package.Name, b.Package.Checksum)
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	switch {
	case errLatest == nil || errPage == nil:
		return sums, nil
	case adoptiumDown(errPage):
		return nil, fmt.Errorf("%w：%v", errAdoptiumUnreachable, errPage)
	}
	// 两问都回了话、却都没给出清单（404、回的不是 JSON）：接口还在，Adoptium 官方源那条路照常试
	return nil, fmt.Errorf("Adoptium 的接口没给出校验值清单：%v", errLatest)
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

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

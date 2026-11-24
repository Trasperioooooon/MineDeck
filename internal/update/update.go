// Package update 自动更新：检查 GitHub Releases，下载新版 exe 并通过
// update.bat 在退出后原子替换（国内经 gh 加速镜像兜底）。
package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/dl"
	"minedeck/internal/errhint"
)

type Release struct {
	Tag         string `json:"tag"`
	Notes       string `json:"notes"`
	AssetURL    string `json:"-"`
	Size        int64  `json:"sizeBytes"`
	Prerelease  bool   `json:"prerelease"` // 查到的是测试版（预发布）：标签带「-」后缀，或 GitHub 上标成了预发布
	Channel     string `json:"-"`          // 按哪个渠道查到的（app.UpdateStable / app.UpdateLatest）
	SHA256      string `json:"-"`          // 官方发布的 exe SHA-256（自 checksums 资产直连 GitHub 解析）
	checksumURL string
}

// apiBase GitHub API 的地址。只有测试把它换成 httptest 起的假接口，程序里不改。
var apiBase = "https://api.github.com"

// currentVersion 正在用的版本。只有测试换它（app.Version 是常量，测不了「用着测试版」和「用着正式版」两种情形）。
var currentVersion = app.Version

// ghRelease GitHub Releases 接口里用得到的字段。
type ghRelease struct {
	TagName    string `json:"tag_name"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// StartupNotice 启动时后台检查发现的新版本，存在内存里供界面安静提示。
type StartupNotice struct {
	Tag        string `json:"tag"`
	Notes      string `json:"notes,omitempty"`
	SizeBytes  int64  `json:"sizeBytes,omitempty"`
	Prerelease bool   `json:"prerelease,omitempty"`
	Channel    string `json:"channel"`
}

var (
	startupMu     sync.Mutex
	startupNotice *StartupNotice
)

// StartupUpdate 返回启动时后台检查记住的新版本通知；没有或已清除时返回 nil。
func StartupUpdate() *StartupNotice {
	startupMu.Lock()
	defer startupMu.Unlock()
	if startupNotice == nil {
		return nil
	}
	cp := *startupNotice
	return &cp
}

// ClearStartupUpdate 清掉内存里的启动更新通知（切换渠道、手动检查或开始更新时调用）。
func ClearStartupUpdate() {
	startupMu.Lock()
	startupNotice = nil
	startupMu.Unlock()
}

// ChannelLabel 返回更新渠道的中文名称，用于启动检查日志等。
func ChannelLabel(ch string) string {
	if ch == app.UpdateLatest {
		return "测试版渠道"
	}
	return "稳定版渠道"
}

func versionLabel(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
		return v
	}
	return "v" + v
}

// Check 按设置里的更新渠道查询；repo 形如 "user/MineDeck"。
// 渠道没选过时按正在用的版本定（见 app.UpdateChannelFor）。「检查更新」「立即更新」都走这里。
func Check(ctx context.Context, repo string) (*Release, bool, error) {
	rel, has, err := check(ctx, repo, app.UpdateChannelFor(app.GetConfig().UpdateChannel, currentVersion), currentVersion)
	if err == nil && !has {
		ClearStartupUpdate()
	}
	return rel, has, err
}

// RunStartupCheck 启动时按当前配置渠道检查一次更新：记录渠道与结果日志，
// 查到有新版本时存进内存，供设置入口和「更新与关于」页不打扰提示。
func RunStartupCheck(ctx context.Context, repo string, logf func(string, ...any)) {
	ch := app.UpdateChannelFor(app.GetConfig().UpdateChannel, currentVersion)
	label := ChannelLabel(ch)
	rel, has, err := check(ctx, repo, ch, currentVersion)
	if err != nil {
		if logf != nil {
			logf("%s：后台检查更新失败：%v", label, err)
		}
		return
	}
	cur := versionLabel(currentVersion)
	if has {
		startupMu.Lock()
		startupNotice = &StartupNotice{
			Tag:        rel.Tag,
			Notes:      rel.Notes,
			SizeBytes:  rel.Size,
			Prerelease: rel.Prerelease,
			Channel:    ch,
		}
		startupMu.Unlock()
		if logf != nil {
			logf("%s：有新版本 %s（当前 %s），可在设置中更新", label, rel.Tag, cur)
		}
		return
	}
	ClearStartupUpdate()
	if logf != nil {
		if rel.Tag == "" {
			logf("%s：已是最新 %s（当前没有找到正式发布版本）", label, cur)
		} else {
			logf("%s：已是最新 %s", label, cur)
		}
	}
}

func isStableRelease(r ghRelease) bool {
	return !r.Draft && !r.Prerelease && versionTag.MatchString(strings.TrimSpace(r.TagName)) && !isPre(r.TagName)
}

func wrapUpdateFallbackHint(err error, fallbackHint string) error {
	if err == nil || errhint.FromError(err) != "" {
		return err
	}
	return errhint.Wrap(err, fallbackHint)
}

func fetchReleasesPage(ctx context.Context, repo string) ([]ghRelease, error) {
	var list []ghRelease
	api := apiBase + "/repos/" + repo + "/releases?per_page=30"
	if err := dl.FetchJSON(ctx, []string{api}, &list); err != nil {
		return nil, wrapUpdateFallbackHint(fmt.Errorf("检查更新失败（连不上 GitHub）：%w", err), "请检查网络后重试，或直接去发布页查看")
	}
	return list, nil
}

// check 按 channel 查 repo 的新版本，返回查到的那个版本和它是不是比 current 新（严格更新；任何渠道都不会「更新」到更旧的版本）。
//   - 稳定版：优先查 releases/latest；如果返回的是预发布或带预发布后缀的标签（如测试版被手动标成 Latest），
//     改为在第一页发布列表里取最新的正式版，找不到就当没有更新。
//   - 最新版：取第一页发布列表，跳过草稿和标签不是版本号的，在正式版和预发布版里按版本号取最大的，不靠接口返回的顺序。
//
// 取到以后附件和校验和的处理两条渠道一样：exe 要有官方 SHA256SUMS（直连 GitHub 取）才装，预发布版不放宽（见 Apply）。
func check(ctx context.Context, repo, channel, current string) (*Release, bool, error) {
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	if repo == "" || !strings.Contains(repo, "/") {
		return nil, false, fmt.Errorf("未配置更新仓库（在全局设置填写，如 yourname/MineDeck）")
	}
	var rel ghRelease
	if channel == app.UpdateLatest {
		list, err := fetchReleasesPage(ctx, repo)
		if err != nil {
			return nil, false, err
		}
		found := false
		for _, r := range list {
			if r.Draft || !versionTag.MatchString(strings.TrimSpace(r.TagName)) {
				continue // 草稿没发出来；标签不是版本号的（别的东西的发布）不比
			}
			if !found || compare(r.TagName, rel.TagName) > 0 {
				rel, found = r, true
			}
		}
		if !found {
			return nil, false, fmt.Errorf("这个仓库还没有任何发布版本")
		}
	} else {
		channel = app.UpdateStable
		api := apiBase + "/repos/" + repo + "/releases/latest"
		if err := dl.FetchJSON(ctx, []string{api}, &rel); err != nil {
			return nil, false, wrapUpdateFallbackHint(fmt.Errorf("检查更新失败（连不上 GitHub）：%w", err), "请检查网络后重试，或直接去发布页查看")
		}
		if rel.TagName == "" {
			return nil, false, fmt.Errorf("这个仓库还没有任何发布版本")
		}
		if !isStableRelease(rel) {
			// A80：GitHub 上允许把带 -beta 的发布标成 Latest；稳定渠道只收正式版，退回第一页发布列表重选
			list, err := fetchReleasesPage(ctx, repo)
			if err != nil {
				return nil, false, err
			}
			rel = ghRelease{}
			found := false
			for _, r := range list {
				if !isStableRelease(r) {
					continue
				}
				if !found || compare(r.TagName, rel.TagName) > 0 {
					rel, found = r, true
				}
			}
			if !found {
				return &Release{Channel: app.UpdateStable}, false, nil
			}
		}
	}
	r := &Release{Tag: rel.TagName, Notes: rel.Body, Prerelease: rel.Prerelease || isPre(rel.TagName), Channel: channel}
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
	return r, newer(rel.TagName, current), nil
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

// versionTag 「最新版」渠道认作版本的标签：v 可有可无，三段数字，可带「-预发布后缀」和「+构建信息」。
// 同一个仓库里别的东西的发布（标签不是这个样子的）不参加比较。
var versionTag = regexp.MustCompile(`^[vV]?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// Newer 报告版本 a 是否比 b 新（规则见 compare）。
func Newer(a, b string) bool { return compare(a, b) > 0 }

// IsPre 报告版本号是否带预发布后缀（如 "3.2.0-beta.1"），即测试版。
func IsPre(v string) bool { return isPre(v) }

// newer 比较 "v1.2.3" 风格版本号：tag 比 current 新才为 true。
func newer(tag, current string) bool { return compare(tag, current) > 0 }

// isPre 报告版本号是否带预发布后缀（如 "2.9.0-beta.1"）。
func isPre(v string) bool { return parseVer(v).pre != nil }

// ver 拆开的版本号：三段数字，加上预发布后缀按「.」拆成的段（nil = 正式版）。
type ver struct {
	nums [3]string // 去掉前导零的数字串（空串当 0）；按字符串长度再按字典序比，数字再大也不会溢出
	pre  []string
}

// parseVer 拆版本号：去掉首尾空白和开头的 v，丢掉「+构建信息」（按语义化版本不参与比较），按第一个「-」分出预发布后缀。
// 数字段里不是数字的部分当 0（老行为：不认识的写法不报错，只是不会比谁新）。预发布后缀严格按 SemVer 原样拆分、区分大小写。
func parseVer(v string) ver {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	v, _, _ = strings.Cut(v, "+")
	core, pre, hasPre := strings.Cut(v, "-")
	var out ver
	for i, p := range strings.SplitN(core, ".", 3) {
		out.nums[i] = digits(p)
	}
	if hasPre {
		out.pre = strings.Split(pre, ".")
	}
	return out
}

// digits 取一段开头的数字并去掉前导零；没有数字时为空串（当 0）。
func digits(s string) string {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	return strings.TrimLeft(s[:end], "0")
}

// isNum 一段预发布后缀是不是纯数字。
func isNum(s string) bool {
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

// cmpNum 比两个去掉前导零的数字串：位数多的大，位数一样按字典序。
func cmpNum(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// compare 按语义化版本的规矩比 a 和 b：a 新返回 1，一样返回 0，a 旧返回 -1。
//   - 先比三段数字（2.10.0 比 2.9.0 新）。
//   - 数字相同时正式版比预发布版新（3.2.0 比 3.2.0-beta.3 新）：装着测试版的人收得到同号的正式版。
//   - 都是预发布时逐段比后缀：纯数字的段按数字比（beta.2 < beta.3 < beta.10），别的按字符串比（alpha < beta < rc），
//     纯数字的段比非数字的段旧；前面都一样时段数少的更旧（beta < beta.1）。
func compare(a, b string) int {
	va, vb := parseVer(a), parseVer(b)
	for i := 0; i < 3; i++ {
		if c := cmpNum(va.nums[i], vb.nums[i]); c != 0 {
			return c
		}
	}
	switch {
	case va.pre == nil && vb.pre == nil:
		return 0
	case va.pre == nil:
		return 1
	case vb.pre == nil:
		return -1
	}
	for i := 0; i < len(va.pre) && i < len(vb.pre); i++ {
		x, y := va.pre[i], vb.pre[i]
		nx, ny := isNum(x), isNum(y)
		var c int
		switch {
		case nx && ny:
			c = cmpNum(strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0"))
		case nx:
			c = -1
		case ny:
			c = 1
		default:
			c = strings.Compare(x, y)
		}
		if c != 0 {
			return c
		}
	}
	switch {
	case len(va.pre) < len(vb.pre):
		return -1
	case len(va.pre) > len(vb.pre):
		return 1
	}
	return 0
}

// errNoSelfUpdate 换程序的那一步（run_windows.go）只有 Windows 版有；Mac 版先拦下，不白下一个 Windows 的 exe。
var errNoSelfUpdate = errors.New("Mac 版还不能在程序里自动更新，请到发布页下载新版本")

// Apply 下载新版并写入换血脚本；调用方随后应触发程序退出。
func Apply(ctx context.Context, r *Release) error {
	if r.AssetURL == "" {
		return fmt.Errorf("这个版本没有附带 exe 文件")
	}
	if r.SHA256 == "" {
		// 无官方校验和则拒绝：避免经第三方镜像下载并执行未经校验的 exe（供应链/MITM RCE）
		return fmt.Errorf("拿不到这个版本的 SHA256SUMS（文件没有，或下不下来）。没有这份校验，已取消更新。")
	}
	if runtime.GOOS != "windows" {
		return errNoSelfUpdate
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
		return wrapUpdateFallbackHint(fmt.Errorf("下载新版本失败: %w", err), "请检查网络后重试，或去发布页手动下载")
	}
	// 换 exe 的脚本等本进程退出以后才动手，怎么写的、为什么这么写见 run_windows.go 的 swapScript
	return startSwap(app.Base, exe, newExe, os.Getpid())
}

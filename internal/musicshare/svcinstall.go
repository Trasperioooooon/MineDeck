package musicshare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"minedeck/internal/dl"
	"minedeck/internal/mcsrc"
)

// ---- 装 Simple Voice Chat（测试报告观察 13：检查清单只给「去资源页搜索」，资源页 20 条结果里容易装错）----
//
// 从 Modrinth 的 simple-voice-chat 项目装：按服务器的加载器（NeoForge → neoforge，Forge、Mohist → forge）和 MC 版本找版本，
// 取发布最晚的正式版（没有正式版才取测试版）。桥接模组对 SVC 不限版本（两份 mods.toml 里都是 versionRange="[0,)"，
// 2.6.0 以前的 SVC 自动改用兼容写法，见 bridge/voicechat/README.md），所以不再按 SVC 的版本挑。
// 下载先落到 mods 里一个不以 .jar 结尾的临时文件（加载器不认），核对 SHA-1，再读元数据确认是 SVC、加载器对得上，才改成正式文件名。

// ModrinthAPI Modrinth 的 API。
const ModrinthAPI = "https://api.modrinth.com/v2"

// svcProject SVC 在 Modrinth 上的项目（slug）。
const svcProject = "simple-voice-chat"

// svcLoader 服务器核心在 Modrinth 上对应的加载器（同 catalog 的 resourceSpec）。
func svcLoader(core mcsrc.Core) string {
	if core == mcsrc.CoreNeoForge {
		return "neoforge"
	}
	return "forge"
}

func (s *Service) modrinthAPI() string {
	if s.d.ModrinthAPI != "" {
		return strings.TrimRight(s.d.ModrinthAPI, "/")
	}
	return ModrinthAPI
}

// SVCPlan 装 SVC 之前算出来的（给操作层的预览用，不联网）。
type SVCPlan struct {
	Loader   string   // Modrinth 的加载器：neoforge / forge
	MC       string   // MC 版本
	Status   string   // 实例的状态
	Running  bool     // 服务器没停：装好后重启才生效
	Disabled []string // mods 里被停用的 SVC（不动它们）
}

// PreviewSVC 检查能不能装：核心和版本支持音乐共享、mods 里还没有 SVC。不联网，不改任何文件。
func (s *Service) PreviewSVC(name string) (*SVCPlan, error) {
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return nil, &Error{Code: 404, Err: err}
	}
	if ok, why := supportOf(info.Core, info.MC); !ok {
		return nil, errorf(400, "%s", why)
	}
	svc := s.detectSVC(info.Dir)
	if svc.Installed {
		return nil, errorf(400, "mods 里已经有 Simple Voice Chat %s（%s），不用再装", svc.Version, svc.File)
	}
	return &SVCPlan{Loader: svcLoader(info.Core), MC: info.MC, Status: info.Status, Running: info.Status != "stopped", Disabled: svc.Disabled}, nil
}

// SVCResult InstallSVC 的结果。
type SVCResult struct {
	File    string `json:"file"`           // 装进 mods 的文件名
	Version string `json:"version"`        // 模组版本（jar 里的元数据），如 1.21.1-2.6.27
	Name    string `json:"name"`           // Modrinth 上的版本号，如 neoforge-1.21.1-2.6.27
	Type    string `json:"type,omitempty"` // 不是正式版时：beta / alpha
	Restart bool   `json:"restart"`        // 服务器运行中装的：重启后生效
}

// mrVersion Modrinth 版本列表里用到的字段。
type mrVersion struct {
	VersionNumber string   `json:"version_number"`
	VersionType   string   `json:"version_type"`
	DatePublished string   `json:"date_published"`
	Loaders       []string `json:"loaders"`
	GameVersions  []string `json:"game_versions"`
	Files         []mrFile `json:"files"`
}

type mrFile struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Size     int64  `json:"size"`
	Hashes   struct {
		SHA1 string `json:"sha1"`
	} `json:"hashes"`
}

var jarNameRe = regexp.MustCompile(`^[A-Za-z0-9._+-]+\.jar$`)

// pickSVC 挑要装的版本：加载器、MC 版本都对得上，有文件；正式版优先，同一种里取发布最晚的。
func pickSVC(vs []mrVersion, loader, mc string) (mrVersion, bool) {
	rank := func(t string) int { return map[string]int{"release": 0, "beta": 1, "alpha": 2}[t] }
	var best *mrVersion
	var bestAt time.Time
	for i := range vs {
		v := &vs[i]
		if !slices.Contains(v.Loaders, loader) || !slices.Contains(v.GameVersions, mc) || len(v.Files) == 0 {
			continue
		}
		at, _ := time.Parse(time.RFC3339, v.DatePublished)
		if best == nil || rank(v.VersionType) < rank(best.VersionType) || (rank(v.VersionType) == rank(best.VersionType) && at.After(bestAt)) {
			best, bestAt = v, at
		}
	}
	if best == nil {
		return mrVersion{}, false
	}
	return *best, true
}

// InstallSVC 从 Modrinth 下载 SVC 装进 mods（见本节开头）。服务器运行中装的，记下「重启后生效」。
func (s *Service) InstallSVC(ctx context.Context, name string) (*SVCResult, error) {
	if !s.svcMu.TryLock() {
		return nil, errorf(409, "正在安装 Simple Voice Chat，请稍候")
	}
	defer s.svcMu.Unlock()
	p, err := s.PreviewSVC(name)
	if err != nil {
		return nil, err
	}
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return nil, &Error{Code: 404, Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	lj, _ := json.Marshal([]string{p.Loader})
	gj, _ := json.Marshal([]string{p.MC})
	u := fmt.Sprintf("%s/project/%s/version?loaders=%s&game_versions=%s", s.modrinthAPI(), svcProject, url.QueryEscape(string(lj)), url.QueryEscape(string(gj)))
	var vs []mrVersion
	if err := dl.FetchJSON(ctx, []string{u}, &vs); err != nil {
		return nil, errorf(502, "在 Modrinth 上查 Simple Voice Chat 的版本失败：%v（检查网络或代理后再试）", err)
	}
	v, ok := pickSVC(vs, p.Loader, p.MC)
	if !ok {
		return nil, errorf(404, "Modrinth 上没有 %s %s 版的 Simple Voice Chat：可以到 Modrinth 或 CurseForge 找相近的版本手动放进 mods", p.Loader, p.MC)
	}
	f := v.Files[0]
	for _, x := range v.Files {
		if x.Primary {
			f = x
			break
		}
	}
	fname := filepath.Base(f.Filename)
	fu, err := url.Parse(f.URL)
	if !jarNameRe.MatchString(fname) || err != nil || len(f.Hashes.SHA1) != 40 ||
		(s.d.ModrinthAPI == "" && (fu.Scheme != "https" || fu.Hostname() != "cdn.modrinth.com")) { // 用真的 Modrinth 时下载地址只认它的 CDN
		return nil, errorf(502, "Modrinth 给的 %s 的文件信息不对（文件名、地址或 SHA-1）", v.VersionNumber)
	}

	mods := modsDir(info.Dir)
	if err := os.MkdirAll(mods, 0o755); err != nil {
		return nil, errorf(500, "建 mods 目录失败：%v", err)
	}
	tmp := filepath.Join(mods, "."+fname+".download") // 不以 .jar 结尾：下载、检查途中服务器（重）启动也不会加载它
	defer os.Remove(tmp)
	if err := dl.Fetch(ctx, dl.Request{URLs: []string{f.URL}, Dest: tmp, SHA1: f.Hashes.SHA1, MinSize: 1024}, nil); err != nil {
		return nil, errorf(502, "下载 Simple Voice Chat %s 失败：%v（检查网络或代理后再试）", v.VersionNumber, err)
	}
	isSVC, ver, loader := scanJarForSVC(tmp)
	if !isSVC {
		return nil, errorf(502, "下载的 %s 里没有 Simple Voice Chat（模组 ID 不是 voicechat），没有安装", fname)
	}
	if loader != p.Loader {
		return nil, errorf(502, "下载的 %s 是给 %s 的，这台服务器要 %s 版，没有安装", fname, loader, p.Loader)
	}
	// 下载要几十秒到几分钟，期间可能已经从资源页装上了 SVC（那条路不经 svcMu）：mods 里有两份 SVC 服务器起不来，改名前再看一眼（Codex 复审发现）
	if svc := s.detectSVC(info.Dir); svc.Installed {
		return nil, errorf(409, "下载期间 mods 里已经装上了 Simple Voice Chat %s（%s），这次的就不装了", svc.Version, svc.File)
	}
	if err := os.Rename(tmp, filepath.Join(mods, fname)); err != nil {
		return nil, errorf(500, "安装 %s 失败（可能正被占用，停服后再试）：%v", fname, err)
	}
	// 服务器在不在跑要按装好这一刻算：下载那几分钟里它可能被开起来了（这次启动没加载新 jar，要记「重启后生效」）（独立评审发现）
	running := p.Running
	if cur, err := s.d.Instances.Info(name); err == nil {
		running = cur.Status != "stopped"
	}
	res := &SVCResult{File: fname, Version: ver, Name: v.VersionNumber, Restart: running}
	if v.VersionType != "release" {
		res.Type = v.VersionType
	}
	if running {
		s.markRestart(name, restartSVC)
	}
	s.log(name, "已从 Modrinth 安装 Simple Voice Chat %s（%s）%s", v.VersionNumber, fname, map[bool]string{true: "，重启服务器后生效", false: ""}[running])
	return res, nil
}

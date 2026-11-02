package companion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	companionjar "minedeck/internal/companion/jar"
	"minedeck/internal/dl"
	"minedeck/internal/modinfo"
)

// 安装（README「安装」「打进 MineDeck 的 jar」，清单的 tested-first 规则）：
//   - 伙伴模组：内置在 exe 里（companionjar），按加载器和 MC 版本取对应的 jar，写进 mods 前后都核对 sha256；
//   - PlayerEngine、Architectury API：不随 exe 分发，用户点了「安装」才从 Modrinth 下载。先下清单里实测过的那个版本
//     （tested），核对大小和 sha512；它下载不到（404 / 410，作者删了这个版本）时，才在 Modrinth 上按 loaders、gameVersions
//     找 version_type 为 release、version_number 匹配 versionPattern 的版本，取发布最晚的一个，核对 Modrinth 给的大小和 sha512，
//     再确认 jar 里 mods.toml 的版本在 versionRange 之内。已经装着、版本在范围内的不动：不自动追新。
//   - 换掉旧版本：我们自己的模组按文件名前缀和模组 ID 找，依赖按模组 ID 找；停用的（.jar.disabled）不动。
//     服务器运行中加载器占着旧 jar（Windows 上删不掉），这时只能装新的、不能替换，要先停服。

// ModrinthAPI Modrinth 的 API（只在用户点「安装」、要回退去找别的版本时才访问）。
const ModrinthAPI = "https://api.modrinth.com/v2"

// 模组在 mods 里的情况。
const (
	ModOK       = "ok"       // 装着要的那个版本（我们的模组：和内置的逐字节相同；依赖：实测过的版本）
	ModInRange  = "inrange"  // 依赖：装着的版本在范围内，但不是实测过的那个（不自动换）
	ModOutdated = "outdated" // 我们的模组：装着别的版本；依赖：装着的版本不在范围内，加载器会拒绝启动
	ModMissing  = "missing"  // 没装（只有停用的副本也算没装）
)

// ModFile mods 里的一个相关文件。
type ModFile struct {
	File     string `json:"file"`
	Version  string `json:"version,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Keep     bool   `json:"keep"` // 装的时候留着它；否则会被删掉、换成要的那个
}

// ModStatus 三个模组之一的安装情况。
type ModStatus struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"` // companion：MineDeck 伙伴模组（内置）；dependency：要从 Modrinth 下载的依赖
	State   string    `json:"state"`
	Version string    `json:"version,omitempty"` // 装着的版本（启用的那个）
	Want    string    `json:"want"`              // 要装的版本：伙伴模组是内置的版本，依赖是实测过的版本
	Range   string    `json:"range,omitempty"`   // 依赖：伙伴模组要求的版本范围
	File    string    `json:"file"`              // 要装的文件名
	Size    int64     `json:"size"`              // 依赖：要下载的大小
	Files   []ModFile `json:"files"`
	License string    `json:"license"`
	Page    string    `json:"page,omitempty"` // 依赖：Modrinth 上实测过的那个版本的网页
}

// InstallView 「伙伴」标签的安装状态。
type InstallView struct {
	Supported  bool        `json:"supported"`
	Reason     string      `json:"reason,omitempty"`     // 不支持的原因
	Loader     string      `json:"loader,omitempty"`     // neoforge / forge
	MC         string      `json:"mc,omitempty"`         //
	LoaderWarn string      `json:"loaderWarn,omitempty"` // 加载器的版本不在伙伴模组要求的范围里
	Java       int         `json:"java,omitempty"`       // 服务端要的 Java 大版本
	Mods       []ModStatus `json:"mods"`
	Ready      bool        `json:"ready"`    // 三个都装好了（依赖的版本在范围内也算）
	Needed     bool        `json:"needed"`   // 有要装、要换的
	Download   int64       `json:"download"` // 装的话要下载多少字节
}

// ClientMod 好友的客户端要装的一个模组（按清单显示：side 是 server 的依赖不用装）。
type ClientMod struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	File     string `json:"file,omitempty"`
	Required bool   `json:"required"`
	Page     string `json:"page,omitempty"` // Modrinth 的网页；伙伴模组没有（随 MineDeck 内置，从服务器的 mods 里拷）
	License  string `json:"license,omitempty"`
}

// loaderOf MineDeck 的核心 → 清单里的加载器。Mohist 这类混合端没测过，不算支持。
func loaderOf(core string) string {
	switch core {
	case companionjar.LoaderNeoForge, companionjar.LoaderForge:
		return core
	}
	return ""
}

// supported 清单里有没有这个加载器和 MC 版本的 jar；没有时给出原因（按清单列出支持的组合，不写死）。
func supported(m *companionjar.Manifest, core, mc string) (companionjar.Jar, string) {
	if j, ok := m.Find(loaderOf(core), mc); ok && loaderOf(core) != "" {
		return j, ""
	}
	var combos []string
	for _, j := range m.Jars {
		combos = append(combos, loaderName(j.Loader)+" "+j.Minecraft)
	}
	what := loaderName(core)
	if what == "" {
		what = core
	}
	return companionjar.Jar{}, fmt.Sprintf("AI 伙伴现在只支持 %s（这台是 %s %s）：伙伴模组用到了 Minecraft 的类，每个加载器、每个 MC 版本要各编一个 jar",
		strings.Join(combos, "、"), what, mc)
}

func loaderName(l string) string {
	switch l {
	case "neoforge":
		return "NeoForge"
	case "forge":
		return "Forge"
	case "fabric":
		return "Fabric"
	case "mohist":
		return "Mohist"
	case "paper":
		return "Paper"
	case "vanilla":
		return "原版"
	}
	return l
}

func sha256Hex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func sha512Hex(b []byte) string { s := sha512.Sum512(b); return hex.EncodeToString(s[:]) }

func modrinthPage(d companionjar.Dependency) string {
	return "https://modrinth.com/mod/" + d.Modrinth.Slug + "/version/" + d.Modrinth.Tested.VersionID
}

func isOurs(m modinfo.Mod, id string) bool {
	return strings.HasPrefix(strings.ToLower(m.File), companionjar.FilePrefix) || m.ID == id || contains(m.Also, id)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// inspect 看 mods 目录里三个模组的情况。目录不存在时都没装。
func inspect(m *companionjar.Manifest, j companionjar.Jar, modsDir string) ([]ModStatus, error) {
	mods, err := modinfo.ReadDir(modsDir)
	if err != nil {
		return nil, err
	}
	ours := ModStatus{ID: m.Mod.ID, Name: m.Mod.Name, Kind: "companion", State: ModMissing, Want: m.Mod.Version, File: j.InstallName,
		Size: j.Size, Files: []ModFile{}, License: m.Mod.License}
	for _, md := range mods {
		if !isOurs(md, m.Mod.ID) {
			continue
		}
		f := ModFile{File: md.File, Version: md.Version, Disabled: md.Disabled, Keep: md.Disabled}
		if !md.Disabled {
			if ours.Version == "" {
				ours.Version = md.Version
			}
			if md.File == j.InstallName {
				if b, err := os.ReadFile(filepath.Join(modsDir, md.File)); err == nil && sha256Hex(b) == j.SHA256 {
					f.Keep, ours.State, ours.Version = true, ModOK, md.Version
				}
			}
		}
		ours.Files = append(ours.Files, f)
	}
	if ours.State != ModOK && hasEnabled(ours.Files) {
		ours.State = ModOutdated
	}
	out := []ModStatus{ours}
	for _, d := range j.Dependencies {
		t := d.Modrinth.Tested
		st := ModStatus{ID: d.ModID, Name: d.Name, Kind: "dependency", State: ModMissing, Want: t.ModVersion, Range: d.VersionRange,
			File: t.File, Size: t.Size, Files: []ModFile{}, License: d.License, Page: modrinthPage(d)}
		best := -1 // 留哪一个：实测过的版本优先，其次范围内的（同一个模组启用了两份，加载器会拒绝启动，只能留一个）
		rank := func(v string) int {
			switch {
			case v == t.ModVersion:
				return 2
			case InRange(v, d.VersionRange):
				return 1
			}
			return 0
		}
		for _, md := range mods {
			if md.ID != d.ModID && !contains(md.Also, d.ModID) {
				continue
			}
			st.Files = append(st.Files, ModFile{File: md.File, Version: md.Version, Disabled: md.Disabled, Keep: md.Disabled})
			if md.Disabled {
				continue
			}
			if r := rank(md.Version); r > 0 && (best < 0 || r > rank(st.Files[best].Version)) {
				best = len(st.Files) - 1
			}
		}
		switch {
		case best >= 0:
			st.Files[best].Keep = true
			st.Version = st.Files[best].Version
			st.State = ModInRange
			if rank(st.Version) == 2 {
				st.State = ModOK
			}
		case hasEnabled(st.Files):
			st.State = ModOutdated
			for _, f := range st.Files {
				if !f.Disabled {
					st.Version = f.Version
					break
				}
			}
		}
		out = append(out, st)
	}
	return out, nil
}

func hasEnabled(fs []ModFile) bool {
	for _, f := range fs {
		if !f.Disabled {
			return true
		}
	}
	return false
}

// installView 汇总安装状态（只读本地文件，不联网）。
func (s *Service) installView(info InstInfo) InstallView {
	v := InstallView{Mods: []ModStatus{}}
	man, err := s.manifest()
	if err != nil {
		v.Reason = err.Error()
		return v
	}
	j, why := supported(man, info.Core, info.MC)
	if why != "" {
		v.Reason = why
		return v
	}
	v.Supported, v.Loader, v.MC, v.Java = true, j.Loader, j.Minecraft, j.Java
	if info.Build != "" && !InRange(info.Build, j.LoaderVersions) {
		v.LoaderWarn = fmt.Sprintf("伙伴模组要 %s %s，这台是 %s：加载器会拒绝启动，先换加载器的版本（实测过的是 %s）",
			loaderName(j.Loader), j.LoaderVersions, info.Build, j.TestedLoader)
	}
	mods, err := inspect(man, j, filepath.Join(info.Dir, "mods"))
	if err != nil {
		v.Supported, v.Reason = false, "读 mods 目录失败："+err.Error()
		return v
	}
	v.Mods, v.Ready = mods, true
	for _, m := range mods {
		if m.State != ModOK && m.State != ModInRange {
			v.Ready, v.Needed = false, true
			if m.Kind == "dependency" {
				v.Download += m.Size
			}
		}
		for _, f := range m.Files {
			if !f.Keep {
				v.Needed = true
			}
		}
	}
	return v
}

// clientMods 好友的客户端要装什么：按清单来——伙伴模组看 client，依赖看 side（both 才要装）。
func clientMods(m *companionjar.Manifest, j companionjar.Jar) []ClientMod {
	out := []ClientMod{{ID: m.Mod.ID, Name: m.Mod.Name, Version: m.Mod.Version, File: j.InstallName, Required: j.Client == "required", License: m.Mod.License}}
	for _, d := range j.Dependencies {
		t := d.Modrinth.Tested
		out = append(out, ClientMod{ID: d.ModID, Name: d.Name, Version: t.ModVersion, File: t.File, Required: d.Side == companionjar.SideBoth,
			Page: modrinthPage(d), License: d.License})
	}
	return out
}

// InstallPlan 装之前算出来的：要写哪些、要下载哪些、要删哪些。
type InstallPlan struct {
	Inst     string      `json:"inst"`
	Write    []string    `json:"write"`    // 内置的伙伴模组
	Download []string    `json:"download"` // 要从 Modrinth 下载的依赖（实测过的文件名）
	Remove   []string    `json:"remove"`   // 要删掉的旧版本
	Bytes    int64       `json:"bytes"`    // 要下载多少字节
	Running  bool        `json:"running"`  // 服务器没停：装好后重启才生效
	Noop     bool        `json:"noop"`     // 都装好了，什么都不用改
	Status   string      `json:"-"`        // 实例的状态（提案的前置条件）
	Files    string      `json:"-"`        // mods 里相关文件的指纹（提案的前置条件）
	View     InstallView `json:"-"`
}

// ErrRunningReplace 服务器运行中不能替换已经装着的（文件被服务器占用）。
var ErrRunningReplace = errors.New("服务器运行中不能替换已经装着的模组（文件被服务器占用）：先停服再安装")

// PreviewInstall 算出安装要做的事，不改任何文件、不联网。
func (s *Service) PreviewInstall(name string) (*InstallPlan, error) {
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return nil, &Error{Code: 404, Err: err}
	}
	v := s.installView(info)
	if !v.Supported {
		return nil, &Error{Code: 400, Err: errors.New(v.Reason)}
	}
	p := &InstallPlan{Inst: name, Write: []string{}, Download: []string{}, Remove: []string{}, Running: info.Status != "stopped",
		Status: info.Status, View: v}
	var all []string
	for _, m := range v.Mods {
		for _, f := range m.Files {
			all = append(all, f.File)
			if !f.Keep {
				p.Remove = append(p.Remove, f.File)
			}
		}
		if m.State == ModOK || m.State == ModInRange {
			continue
		}
		if m.Kind == "companion" {
			p.Write = append(p.Write, m.File)
		} else {
			p.Download = append(p.Download, m.File)
			p.Bytes += m.Size
		}
	}
	sort.Strings(all)
	p.Files = strings.Join(all, "、")
	if p.Files == "" {
		p.Files = "无"
	}
	p.Noop = len(p.Write) == 0 && len(p.Download) == 0 && len(p.Remove) == 0
	if p.Running && len(p.Remove) > 0 {
		return nil, &Error{Code: 409, Err: ErrRunningReplace}
	}
	return p, nil
}

// InstallResult Install 的结果。
type InstallResult struct {
	Installed []string    `json:"installed"`          // 装进 mods 的文件
	Removed   []string    `json:"removed"`            // 删掉的旧版本
	Fallback  []string    `json:"fallback,omitempty"` // 实测过的版本下载不到、换成了 Modrinth 上别的版本的依赖（说明）
	Restart   bool        `json:"restart"`            // 服务器运行中装的：重启后生效
	Install   InstallView `json:"install"`
}

// Install 装伙伴模组和它的两个依赖（见文件开头）。每装好一个，马上删掉它的旧版本，免得中途失败时留下两份同一个模组。
func (s *Service) Install(ctx context.Context, name string) (*InstallResult, error) {
	p, err := s.PreviewInstall(name)
	if err != nil {
		return nil, err
	}
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return nil, &Error{Code: 404, Err: err}
	}
	res := &InstallResult{Installed: []string{}, Removed: []string{}}
	if p.Noop {
		res.Install = p.View
		return res, nil
	}
	man, _ := s.manifest()
	j, _ := supported(man, info.Core, info.MC)
	mods := filepath.Join(info.Dir, "mods")
	if err := os.MkdirAll(mods, 0o755); err != nil {
		return nil, &Error{Code: 500, Err: err}
	}
	removeOld := func(m ModStatus) error {
		for _, f := range m.Files {
			if f.Keep {
				continue
			}
			if err := os.Remove(filepath.Join(mods, f.File)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("删不掉旧版本 %s（可能正被服务器占用，停服后再试）：%w", f.File, err)
			}
			res.Removed = append(res.Removed, f.File)
		}
		return nil
	}
	for _, m := range p.View.Mods {
		need := m.State != ModOK && m.State != ModInRange
		if m.Kind == "companion" {
			if need {
				data, err := j.Bytes() // 校验过大小和 sha256；placeJar 再读回来逐字节比一次
				if err != nil {
					return nil, &Error{Code: 500, Err: err}
				}
				if err := placeJar(mods, j.InstallName, data, nil); err != nil {
					return nil, &Error{Code: 500, Err: err}
				}
				res.Installed = append(res.Installed, j.InstallName)
			}
		} else if need {
			d := depByID(j, m.ID)
			file, note, err := s.installDep(ctx, mods, d)
			if err != nil {
				return nil, err
			}
			res.Installed = append(res.Installed, file)
			if note != "" {
				res.Fallback = append(res.Fallback, note)
			}
		}
		if err := removeOld(m); err != nil {
			return nil, &Error{Code: 500, Err: err}
		}
	}
	res.Restart = p.Running
	if p.Running {
		s.markRestart(name)
	}
	res.Install = s.installView(info)
	s.log(name, "已安装 %s%s", strings.Join(res.Installed, "、"), map[bool]string{true: "，重启服务器后生效", false: ""}[p.Running])
	return res, nil
}

func depByID(j companionjar.Jar, id string) companionjar.Dependency {
	for _, d := range j.Dependencies {
		if d.ModID == id {
			return d
		}
	}
	return companionjar.Dependency{}
}

// errGone 下载地址回了 404 / 410：这个版本被删了。
var errGone = errors.New("下载不到（这个版本可能已经被作者删掉了）")

// installDep 按 tested-first 装一个依赖，返回装好的文件名；回退到别的版本时 note 写明换成了哪个。
func (s *Service) installDep(ctx context.Context, mods string, d companionjar.Dependency) (file, note string, err error) {
	t := d.Modrinth.Tested
	data, err := s.download(ctx, t.URL, t.Size, t.SHA512)
	switch {
	case err == nil:
		err = placeJar(mods, t.File, data, checkModsToml(d, func(v string) bool { return v == t.ModVersion }, "实测过的 "+t.ModVersion))
		if err != nil {
			return "", "", &Error{Code: 502, Err: err}
		}
		return t.File, "", nil
	case !errors.Is(err, errGone):
		return "", "", &Error{Code: 502, Err: fmt.Errorf("下载 %s %s 失败：%w", d.Name, t.VersionNumber, err)}
	}
	// 实测过的版本下载不到：回退到 Modrinth 上符合规则的、发布最晚的 release
	v, f, err := s.pickFallback(ctx, d)
	if err != nil {
		return "", "", &Error{Code: 502, Err: fmt.Errorf("%s 实测过的版本 %s 下载不到，在 Modrinth 上也没找到能用的版本：%w", d.Name, t.VersionNumber, err)}
	}
	data, err = s.download(ctx, f.URL, f.Size, f.Hashes.SHA512)
	if err != nil {
		return "", "", &Error{Code: 502, Err: fmt.Errorf("下载 %s %s 失败：%w", d.Name, v.VersionNumber, err)}
	}
	name := filepath.Base(f.Filename)
	if err := placeJar(mods, name, data, checkModsToml(d, func(ver string) bool { return InRange(ver, d.VersionRange) }, "范围 "+d.VersionRange)); err != nil {
		return "", "", &Error{Code: 502, Err: err}
	}
	return name, fmt.Sprintf("%s 实测过的版本 %s 下载不到，换成了 Modrinth 上的 %s（版本在 %s 之内，但没有实测过）", d.Name, t.VersionNumber, v.VersionNumber, d.VersionRange), nil
}

// checkModsToml 下载下来的 jar 里 mods.toml 的模组 ID 要对、版本要满足 ok。
func checkModsToml(d companionjar.Dependency, ok func(string) bool, want string) func(path string) error {
	return func(path string) error {
		m := modinfo.Read(path)
		if m.ID != d.ModID && !contains(m.Also, d.ModID) {
			return fmt.Errorf("下载的 %s 不对：jar 里的模组 ID 是 %q", d.Name, m.ID)
		}
		if !ok(m.Version) {
			return fmt.Errorf("下载的 %s 版本是 %s，不是%s，没有装", d.Name, m.Version, want)
		}
		return nil
	}
}

var jarNameRe = regexp.MustCompile(`^[A-Za-z0-9._+-]+\.jar$`)

// placeJar 把 jar 装进 mods：先写一个不以 .jar 结尾的临时文件（加载器不会把它当模组），读回来交给 check 检查，再改名。
// check 收到的是临时文件的路径（检查 mods.toml 要读 zip）。
func placeJar(mods, name string, data []byte, check func(path string) error) error {
	if !jarNameRe.MatchString(name) {
		return fmt.Errorf("文件名不对：%q", name)
	}
	tmp := filepath.Join(mods, "."+name+".part")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if b, err := os.ReadFile(tmp); err != nil || !bytes.Equal(b, data) {
		os.Remove(tmp)
		return fmt.Errorf("写进 mods 的 %s 读回来不一样（磁盘可能有问题），没有安装", name)
	}
	if check != nil {
		if err := check(tmp); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	dest := filepath.Join(mods, name)
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("安装 %s 失败（可能正被服务器占用，停服后再试）：%w", name, err)
	}
	return nil
}

// ---------- Modrinth ----------

type mrVersion struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
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
		SHA512 string `json:"sha512"`
		SHA1   string `json:"sha1"`
	} `json:"hashes"`
}

// pickFallback 回退规则：项目 projectId、加载器在 loaders 里、游戏版本在 gameVersions 里、version_type 是 release、
// version_number 匹配 versionPattern，取发布时间最晚的一个；文件取 primary 的那个（没有就取第一个），下载地址只认 cdn.modrinth.com。
func (s *Service) pickFallback(ctx context.Context, d companionjar.Dependency) (mrVersion, mrFile, error) {
	r := d.Modrinth
	re, err := regexp.Compile(r.VersionPattern)
	if err != nil {
		return mrVersion{}, mrFile{}, err
	}
	lj, _ := json.Marshal(r.Loaders)
	gj, _ := json.Marshal(r.GameVersions)
	u := fmt.Sprintf("%s/project/%s/version?loaders=%s&game_versions=%s", strings.TrimRight(s.api(), "/"), url.PathEscape(r.ProjectID),
		url.QueryEscape(string(lj)), url.QueryEscape(string(gj)))
	body, err := s.get(ctx, u, 8<<20)
	if err != nil {
		return mrVersion{}, mrFile{}, err
	}
	var vs []mrVersion
	if err := json.Unmarshal(body, &vs); err != nil {
		return mrVersion{}, mrFile{}, fmt.Errorf("Modrinth 的回复读不懂：%w", err)
	}
	var best *mrVersion
	var bestAt time.Time
	for i := range vs {
		v := &vs[i]
		at, err := time.Parse(time.RFC3339, v.DatePublished)
		if err != nil || v.VersionType != "release" || !re.MatchString(v.VersionNumber) || (v.ProjectID != "" && v.ProjectID != r.ProjectID) ||
			!overlap(v.Loaders, r.Loaders) || !overlap(v.GameVersions, r.GameVersions) || len(v.Files) == 0 {
			continue
		}
		if best == nil || at.After(bestAt) {
			best, bestAt = v, at
		}
	}
	if best == nil {
		return mrVersion{}, mrFile{}, fmt.Errorf("没有符合 %s 的正式版", r.VersionPattern)
	}
	f := best.Files[0]
	for _, x := range best.Files {
		if x.Primary {
			f = x
			break
		}
	}
	fu, err := url.Parse(f.URL)
	if err != nil || fu.Scheme != "https" || fu.Hostname() != "cdn.modrinth.com" || !jarNameRe.MatchString(filepath.Base(f.Filename)) ||
		len(f.Hashes.SHA512) != 128 || f.Size <= 0 {
		return mrVersion{}, mrFile{}, fmt.Errorf("Modrinth 给的 %s 的文件信息不对（地址、文件名、大小或 sha512）", best.VersionNumber)
	}
	return *best, f, nil
}

func overlap(a, b []string) bool {
	for _, x := range a {
		if contains(b, x) {
			return true
		}
	}
	return false
}

func (s *Service) api() string {
	if s.d.API != "" {
		return s.d.API
	}
	return ModrinthAPI
}

func (s *Service) client() *http.Client {
	if s.d.HTTP != nil {
		return s.d.HTTP
	}
	return dl.Client
}

// get 取一个小文件（API 的回复），最多 limit 字节。404 / 410 报 errGone。
func (s *Service) get(ctx context.Context, u string, limit int64) ([]byte, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		b, err := s.getOnce(ctx, u, limit)
		if err == nil || errors.Is(err, errGone) || ctx.Err() != nil {
			return b, err
		}
		last = err
	}
	return nil, last
}

func (s *Service) getOnce(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", dl.UA)
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, errGone
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("文件太大（超过 %d 字节）", limit)
	}
	return b, nil
}

// download 下载一个 jar，核对大小和 sha512。
func (s *Service) download(ctx context.Context, u string, size int64, sum string) ([]byte, error) {
	if size <= 0 || size > 256<<20 {
		return nil, fmt.Errorf("文件大小不对：%d", size)
	}
	b, err := s.get(ctx, u, size)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) != size {
		return nil, fmt.Errorf("下载的文件大小是 %d 字节，应为 %d（可能没下完，或者被网络改过）", len(b), size)
	}
	if !strings.EqualFold(sha512Hex(b), sum) {
		return nil, errors.New("下载的文件 sha512 对不上（可能被网络改过），没有安装")
	}
	return b, nil
}

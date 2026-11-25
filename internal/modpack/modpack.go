// Package modpack 解析整合包（Modrinth .mrpack 与 CurseForge zip）并解析服务端文件下载清单。
package modpack

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"minedeck/internal/dl"
	"minedeck/internal/errhint"
	"minedeck/internal/mcsrc"
	"minedeck/internal/winname"
	"minedeck/internal/zipguard"
)

// File 一个待下载的整合包文件。
type File struct {
	Path string // 相对实例目录（如 mods/xxx.jar）
	SHA1 string
	URLs []string
	Size int64
}

// CFRef CurseForge 文件引用（需 API 解析成直链）。
type CFRef struct {
	ProjectID int64
	FileID    int64
	Required  bool

	// 下面几项清单里没有，是 ResolveCF 查到以后填的，给没法自动下载的那些用（查不到就空着）。
	FileName string // 文件名
	Name     string // 项目的名字
	URL      string // 这个文件在 CurseForge 网站上的页面，手动下载用
	Dir      string // 该放进实例的哪个文件夹：mods / resourcepacks / shaderpacks / datapacks
	Why      string // 为什么没法自动下载，见下面的 CFWhy*
}

// 没法自动下载的原因。
const (
	CFWhyNoKey  = "nokey"  // 没有填 API Key
	CFWhyAPI    = "api"    // 接口没查成（Key 不对、网络不通）
	CFWhyNoDist = "nodist" // 作者不允许第三方工具下载这个文件
	CFWhyGone   = "gone"   // CurseForge 上没有这个文件了
	CFWhyName   = "name"   // 文件名写不成这台电脑上的普通文件
)

// 压缩包是哪种。server 是作者给的现成服务端，没有整合包清单，解压后直接当实例。
const (
	FormatMrpack     = "mrpack"
	FormatCurseForge = "curseforge"
	FormatServer     = "server"
)

type Pack struct {
	Format       string // mrpack | curseforge | server
	Name         string
	MC           string
	Core         mcsrc.Core
	Loader       string // 加载器/核心构建版本
	Files        []File
	Pending      []PendingFile // 解析时就无法自动下载的文件，逐项交给用户处置（不再整包拒绝）
	CFRefs       []CFRef
	ZipPath      string
	OverrideDirs []string // 依序应用。清单外面若套了一层目录，这里已经带上那一层
	Root         string   // 清单或服务端文件外面多套的那一层；空表示就在压缩包最外层
	Launch       string   // 仅 server：认出来的启动方式（jar:… 或 args:…）。导入时还会用接管已有服务器的识别再对一遍
	MCFromLog    bool     // 仅 server：MC 是从压缩包里的 logs/latest.log 认出来的（A44），别的地方都没写
	ClientOnly   []string // 仅 mrpack：清单里写明服务端用不上（env.server 是 unsupported）、没有装的文件，导入结果里列出来（A46）
}

// Parse 识别并解析整合包 zip，或一份现成的服务端压缩包。
func Parse(zipPath string) (*Pack, error) {
	zr, err := zipguard.Open(zipPath, zipguard.Default)
	if err != nil {
		wrapErr := fmt.Errorf("无法打开整合包（需为 .mrpack 或 zip）: %w", err)
		if errors.Is(err, zip.ErrFormat) || errors.Is(err, zip.ErrChecksum) || errors.Is(err, zip.ErrAlgorithm) || errors.Is(err, zipguard.ErrTooLarge) {
			return nil, errhint.Wrap(wrapErr, "打不开这个整合包，请确认是完整的压缩包后再试")
		}
		return nil, wrapErr
	}
	defer zr.Close()
	return parseFiles(zipPath, zr.File)
}

// parseFiles 认压缩包里的条目（Parse 打开以后的那一步）。zipPath 只记进 Pack，后面解压时再打开。
// 最外层像服务端时只按最外层认（A40）；它的凭据只有 eula.txt、mods、plugins、libraries 这类旁证、又认不出核心的，
// 再往里看一层（parseInside），里面也认不出就照旧说最外层认不出核心。
func parseFiles(zipPath string, files []*zip.File) (*Pack, error) {
	layout := newZipLayout(files)
	outer := layout.looksLikeServer("")
	p, err := parseLayout(zipPath, files, layout, outer)
	if !outer || !layout.weakServer("") || !errors.Is(err, errServerUnknown) {
		return p, err
	}
	return parseInside(zipPath, files, layout, err)
}

// parseInside 最外层像服务端、凭据只有旁证又认不出核心（outerErr）时往里看一层（A40）。里面的整合包清单照收：服务端包的
// 最外层常带着 mods/，清单套在一层文件夹里。里面的服务端只在最外层的旁证只有 eula.txt、libraries/ 时收：带着 mods/、plugins/
// 的最外层多半就是那台服务端本身（加载器、核心没放进来），里面的是备份之类，拿它顶替的话外面的模组和配置都丢了。
func parseInside(zipPath string, files []*zip.File, layout zipLayout, outerErr error) (*Pack, error) {
	if layout.hasModsOrPlugins("") {
		loc, err := findIndex(files, layout, true)
		if err != nil {
			return nil, err
		}
		if loc == nil {
			return nil, outerErr
		}
		return parseIndex(zipPath, loc)
	}
	inner, err := parseLayout(zipPath, files, layout, false)
	if errors.Is(err, errUnrecognized) {
		return nil, outerErr
	}
	return inner, err
}

// parseLayout 先找整合包清单，没有再认现成服务端。outer 为 true 时只按最外层认：文件夹里的清单不看，服务端就是最外层。
func parseLayout(zipPath string, files []*zip.File, layout zipLayout, outer bool) (*Pack, error) {
	loc, err := findIndex(files, layout, !outer)
	if err != nil {
		return nil, err
	}
	if loc != nil {
		return parseIndex(zipPath, loc)
	}
	return recognizeServer(zipPath, files, layout, outer)
}

// parseIndex 按找到的那份清单解析整合包。
func parseIndex(zipPath string, loc *indexLoc) (*Pack, error) {
	if loc.kind == FormatMrpack {
		return parseMrpack(zipPath, loc.file, loc.root)
	}
	return parseCurseForge(zipPath, loc.file, loc.root)
}

func readZipFile(f *zip.File, out any) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 64<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// safeRelPath 校验整合包内文件路径，防目录穿越。
func safeRelPath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) ||
		strings.Contains(clean, ":") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("整合包内含非法路径: %s", p)
	}
	return clean, nil
}

// ---------- Modrinth .mrpack ----------

func parseMrpack(zipPath string, f *zip.File, root string) (*Pack, error) {
	var idx struct {
		FormatVersion int               `json:"formatVersion"`
		Name          string            `json:"name"`
		Dependencies  map[string]string `json:"dependencies"`
		Files         []struct {
			Path   string `json:"path"`
			Hashes struct {
				SHA1 string `json:"sha1"`
			} `json:"hashes"`
			Env *struct {
				Server string `json:"server"`
			} `json:"env"`
			Downloads []string `json:"downloads"`
			FileSize  int64    `json:"fileSize"`
		} `json:"files"`
	}
	if err := readZipFile(f, &idx); err != nil {
		return nil, errhint.Wrap(fmt.Errorf("解析 modrinth.index.json 失败: %w", err), "整合包里的清单损坏或格式不对，请重新下载整合包后再试")
	}
	p := &Pack{Format: FormatMrpack, Name: idx.Name, ZipPath: zipPath, Root: root,
		OverrideDirs: []string{joinRoot(root, "overrides"), joinRoot(root, "server-overrides")}}
	p.MC = idx.Dependencies["minecraft"]
	if p.MC == "" {
		return nil, fmt.Errorf("整合包未声明 Minecraft 版本")
	}
	switch {
	case idx.Dependencies["fabric-loader"] != "":
		p.Core, p.Loader = mcsrc.CoreFabric, idx.Dependencies["fabric-loader"]
	case idx.Dependencies["neoforge"] != "":
		p.Core, p.Loader = mcsrc.CoreNeoForge, idx.Dependencies["neoforge"]
	case idx.Dependencies["forge"] != "":
		p.Core, p.Loader = mcsrc.CoreForge, idx.Dependencies["forge"]
	case idx.Dependencies["quilt-loader"] != "":
		return nil, fmt.Errorf("暂不支持 Quilt 加载器的整合包")
	default:
		p.Core = mcsrc.CoreVanilla
	}
	for _, fl := range idx.Files {
		if fl.Env != nil && fl.Env.Server == "unsupported" {
			if rel, err := safeRelPath(fl.Path); err == nil { // 仅客户端文件（材质、光影等）：不装，只记下来给人看
				p.ClientOnly = append(p.ClientOnly, rel)
			}
			continue
		}
		rel, err := safeRelPath(fl.Path)
		if err != nil {
			return nil, err
		}
		if rel == "" {
			continue
		}
		// 逐文件分类：能自动下载的进 Files，不能的进 Pending（不再因为一个文件拒绝整个整合包）。
		// 分类规则（顺序重要）：缺 SHA-1 的文件任何处置都不能下载（下载下来没法核对，信任站点也一样）；
		// 有 SHA-1 且有白名单地址才能自动下载；只有自定义 https 地址的留待用户信任站点后下载。
		allowed, pendingURLs := classifyURLs(fl.Downloads, nil)
		switch {
		case len(allowed) > 0 && fl.Hashes.SHA1 == "":
			// 有可下载地址但没有校验值：下载下来没法核对，任何处置都不自动下载（保留地址没有意义）
			p.Pending = append(p.Pending, PendingFile{Path: rel, Size: fl.FileSize, Reason: ReasonNoHash})
		case len(allowed) > 0:
			p.Files = append(p.Files, File{Path: rel, SHA1: fl.Hashes.SHA1, URLs: allowed, Size: fl.FileSize})
		case len(pendingURLs) > 0 && fl.Hashes.SHA1 == "":
			// 自定义站点的直链、但缺校验值：trusted 下载也过不了校验，同上不给下载资格
			p.Pending = append(p.Pending, PendingFile{Path: rel, Size: fl.FileSize, Reason: ReasonNoHash})
		case len(pendingURLs) > 0:
			// https 的自定义主机：交给用户确认（可选「信任这个站点」）后再下载；SHA-1 带上，trust 下载必须过校验
			p.Pending = append(p.Pending, PendingFile{Path: rel, SHA1: fl.Hashes.SHA1, URLs: pendingURLs, Size: fl.FileSize, Reason: ReasonHost})
		default:
			// 一条能用的地址都没有（非 https 或没写地址）：只能保留本地文件 / 手动指定 / 跳过
			p.Pending = append(p.Pending, PendingFile{Path: rel, Size: fl.FileSize, Reason: ReasonBadURL})
		}
	}
	return p, nil
}

// ---------- CurseForge ----------

func parseCurseForge(zipPath string, f *zip.File, root string) (*Pack, error) {
	var man struct {
		Name      string `json:"name"`
		Overrides string `json:"overrides"`
		Minecraft struct {
			Version    string `json:"version"`
			ModLoaders []struct {
				ID      string `json:"id"`
				Primary bool   `json:"primary"`
			} `json:"modLoaders"`
		} `json:"minecraft"`
		Files []struct {
			ProjectID int64 `json:"projectID"`
			FileID    int64 `json:"fileID"`
			Required  bool  `json:"required"`
		} `json:"files"`
	}
	if err := readZipFile(f, &man); err != nil {
		return nil, errhint.Wrap(fmt.Errorf("解析 manifest.json 失败: %w", err), "整合包里的清单损坏或格式不对，请重新下载整合包后再试")
	}
	p := &Pack{Format: FormatCurseForge, Name: man.Name, ZipPath: zipPath, Root: root, MC: man.Minecraft.Version}
	ov := man.Overrides
	if ov == "" {
		ov = "overrides"
	}
	// 清单写的覆盖目录是相对清单所在的那一层，不是相对压缩包最外层
	p.OverrideDirs = []string{joinRoot(root, ov)}
	loaderID := ""
	for _, l := range man.Minecraft.ModLoaders {
		if l.Primary || loaderID == "" {
			loaderID = l.ID
		}
	}
	kind, ver, _ := strings.Cut(loaderID, "-")
	switch kind {
	case "forge":
		p.Core, p.Loader = mcsrc.CoreForge, ver
	case "neoforge":
		p.Core, p.Loader = mcsrc.CoreNeoForge, ver
	case "fabric":
		p.Core, p.Loader = mcsrc.CoreFabric, ver
	case "quilt":
		return nil, fmt.Errorf("暂不支持 Quilt 加载器的整合包")
	default:
		return nil, fmt.Errorf("无法识别整合包加载器: %s", loaderID)
	}
	for _, fl := range man.Files {
		p.CFRefs = append(p.CFRefs, CFRef{ProjectID: fl.ProjectID, FileID: fl.FileID, Required: fl.Required})
	}
	return p, nil
}

// cfAPIBase CurseForge 官方接口的地址。包变量是为了测试能换成本地的假服务。
var cfAPIBase = "https://api.curseforge.com"

// cfPost 调一次 CurseForge 的接口（POST，JSON 进 JSON 出）。
func cfPost(ctx context.Context, apiPath string, payload any, apiKey string, out any) error {
	body, _ := json.Marshal(payload)
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, cfAPIBase+apiPath, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("User-Agent", dl.UA)
	resp, err := dl.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CurseForge API HTTP %d（请检查 API Key）", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(out)
}

// cfProject 一个 CurseForge 项目里这边用得上的几样。
type cfProject struct {
	Name    string
	Site    string // 项目主页（核对过是 CurseForge 自己的 https 地址，不是就为空）
	ClassID int64
}

// cfProjects 查一批项目的名字、主页和类别。
func cfProjects(ctx context.Context, ids []int64, apiKey string) (map[int64]cfProject, error) {
	var out struct {
		Data []struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			ClassID int64  `json:"classId"`
			Links   struct {
				WebsiteURL string `json:"websiteUrl"`
			} `json:"links"`
		} `json:"data"`
	}
	if err := cfPost(ctx, "/v1/mods", map[string]any{"modIds": ids}, apiKey, &out); err != nil {
		return nil, err
	}
	m := make(map[int64]cfProject, len(out.Data))
	for _, d := range out.Data {
		m[d.ID] = cfProject{Name: cleanName(d.Name), Site: cfSite(d.Links.WebsiteURL), ClassID: d.ClassID}
	}
	return m, nil
}

// cfSite 接口给的项目主页只有是 CurseForge 自己的 https 地址才用（要拿给用户点，也要靠它认类别）。
func cfSite(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || (u.Host != "www.curseforge.com" && u.Host != "curseforge.com") || u.User != nil {
		return ""
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimRight(u.String(), "/")
}

// cleanName 项目名要写进日志和清单文件：去掉换行和控制字符，太长的截断。
func cleanName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > 80 {
		name = string(r[:80]) + "…"
	}
	return name
}

// cfDir 这个项目的文件该放进实例的哪个文件夹。整合包的清单不分模组、资源包、光影和数据包，
// 全放进 mods 的话，不是模组的那些在那里什么用都没有。
// 先看主页地址里的那一段（https://www.curseforge.com/minecraft/texture-packs/xxx），它自己写明了是哪一类；
// 地址认不出来再看类别编号；都认不出来的按模组算，和原来一样。
// 数据包放在根目录的 datapacks 里不会自己生效，这是有意的：带数据包的整合包通常自己带着加载它们的模组
// 和一份放好位置的拷贝（比如 config/paxi/datapacks），再塞进世界文件夹就加载两遍了。
func cfDir(p cfProject) string {
	if u, err := url.Parse(p.Site); err == nil && p.Site != "" {
		seg := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(seg) >= 2 && seg[0] == "minecraft" {
			switch seg[1] {
			case "mc-mods":
				return "mods"
			case "texture-packs":
				return "resourcepacks"
			case "shaders":
				return "shaderpacks"
			case "data-packs":
				return "datapacks"
			}
		}
	}
	switch p.ClassID {
	case 12:
		return "resourcepacks"
	case 6552:
		return "shaderpacks"
	}
	return "mods"
}

// cfFileNameOK 接口给的文件名能不能当落点里的那一段用。只认单独一段、在 Windows 上原样写得出来的名字（winname.OK）：
//   - 带分隔符的不要：mods/../server.properties 化简以后就落到实例根目录去了；
//   - 只有点的、末尾是点或空格的不要：Windows 会把它们去掉，落下来的名字就不是记下的那个；
//   - 带冒号（盘符、备用数据流）、Windows 不许用的符号、换行和控制字符的不要（名字还要写进日志和清单文件，DEL 也不要）；
//   - 保留设备名不要，带扩展名的（con.jar）也算。导入那边的落点检查（inst 的 overrideAllowed）用的是同一套，
//     这样名字不能用的在这一步就归到「名字不能用」，不会到后面才被挡下、原因却写成别的。
func cfFileNameOK(name string) bool {
	return winname.OK(name) && !strings.ContainsRune(name, 0x7f)
}

// ResolveCF 通过 CurseForge 官方 API（需用户 API key）把文件引用解析为直链。
// 返回可下载的文件和没法自动下载的引用（带着原因，查得到的话还有文件名、项目名和下载页）。
// err 不为 nil 时也可能有文件可下载：项目的类别没查到只影响往哪个文件夹放，不耽误下载。
func ResolveCF(ctx context.Context, refs []CFRef, apiKey string) (files []File, unresolved []CFRef, err error) {
	fail := func(why string, err error) ([]File, []CFRef, error) {
		out := make([]CFRef, len(refs))
		for i, r := range refs {
			r.Why, r.Dir = why, "mods"
			out[i] = r
		}
		return nil, out, err
	}
	if len(refs) == 0 {
		return nil, nil, nil
	}
	if apiKey == "" {
		return fail(CFWhyNoKey, fmt.Errorf("未配置 CurseForge API Key（可在全局设置填写，或改用 Modrinth 格式整合包）"))
	}
	fileIDs := make([]int64, 0, len(refs))
	var projIDs []int64
	seen := map[int64]bool{}
	for _, r := range refs {
		fileIDs = append(fileIDs, r.FileID)
		if !seen[r.ProjectID] {
			seen[r.ProjectID] = true
			projIDs = append(projIDs, r.ProjectID)
		}
	}
	var out struct {
		Data []struct {
			ID          int64  `json:"id"`
			FileName    string `json:"fileName"`
			DownloadURL string `json:"downloadUrl"`
			FileLength  int64  `json:"fileLength"`
			Hashes      []struct {
				Value string `json:"value"`
				Algo  int    `json:"algo"` // 1=SHA1 2=MD5
			} `json:"hashes"`
		} `json:"data"`
	}
	if err := cfPost(ctx, "/v1/mods/files", map[string]any{"fileIds": fileIDs}, apiKey, &out); err != nil {
		return fail(CFWhyAPI, err)
	}
	// 项目的名字、主页、类别另查一次。查不到不耽误下载：文件都按模组放，没法下载的只有编号，和原来一样。
	projects, projErr := cfProjects(ctx, projIDs, apiKey)
	byID := map[int64]int{}
	for i, d := range out.Data {
		byID[d.ID] = i
	}
	for _, r := range refs {
		proj := projects[r.ProjectID]
		r.Name, r.Dir = proj.Name, cfDir(proj)
		if proj.Site != "" {
			r.URL = fmt.Sprintf("%s/files/%d", proj.Site, r.FileID)
		}
		i, ok := byID[r.FileID]
		if !ok {
			r.Why = CFWhyGone
			unresolved = append(unresolved, r)
			continue
		}
		d := out.Data[i]
		rel, perr := "", fmt.Errorf("文件名不能用")
		if cfFileNameOK(d.FileName) {
			rel, perr = safeRelPath(r.Dir + "/" + d.FileName)
		}
		if perr != nil {
			r.Why = CFWhyName
			unresolved = append(unresolved, r)
			continue
		}
		r.FileName = d.FileName
		if d.DownloadURL == "" {
			r.Why = CFWhyNoDist
			unresolved = append(unresolved, r)
			continue
		}
		sha1 := ""
		for _, h := range d.Hashes {
			if h.Algo == 1 {
				sha1 = h.Value
			}
		}
		files = append(files, File{Path: rel, SHA1: sha1, URLs: []string{d.DownloadURL}, Size: d.FileLength})
	}
	if projErr != nil {
		err = fmt.Errorf("没查到这些文件各属于哪一类（%v），全部按模组放进了 mods", projErr)
	}
	return files, unresolved, err
}

// overrideEntry 覆盖目录里要写进实例的一个文件。
type overrideEntry struct {
	f    *zip.File
	rel  string // 相对实例目录
	size int64
}

// overrideEntries 覆盖目录里要写进实例的文件，按写的先后：OverrideDirs 依次来，每个目录里按 zip 里的先后。
// total 是按 zip 里写的大小合计。路径不安全的整包拒绝。
func overrideEntries(files []*zip.File, dirs []string) (ents []overrideEntry, total int64, err error) {
	for _, prefix := range dirs {
		for _, f := range files {
			name := zipNameOf(f)
			if !strings.HasPrefix(name, prefix+"/") || isZipDir(f, name) || !f.Mode().IsRegular() {
				continue
			}
			rel, err := safeRelPath(strings.TrimPrefix(name, prefix+"/"))
			if err != nil {
				return nil, 0, err
			}
			size := int64(f.UncompressedSize64)
			if size < 0 || total+size < total {
				return nil, 0, fmt.Errorf("整合包里的文件大小不对: %s", f.Name)
			}
			total += size
			ents = append(ents, overrideEntry{f: f, rel: rel, size: size})
		}
	}
	return ents, total, nil
}

// OverrideFiles 覆盖目录里要写进实例的文件（相对实例目录），按写的先后（同 ExtractOverrides）。
// 导入前用它找只差大小写的落点（A13、A47）；读不了的返回错误，真正解压时 ExtractOverrides 会报出来。
func OverrideFiles(p *Pack) ([]string, error) {
	zr, err := zipguard.Open(p.ZipPath, zipguard.Default)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	ents, _, err := overrideEntries(zr.File, p.OverrideDirs)
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		out = append(out, e.rel)
	}
	return out, err
}

// 整合包覆盖目录解开后的上限，和现成服务端压缩包一样（serverMaxFiles、serverMaxBytes）。
// 写成变量只是为了测试能调小。
var (
	overrideMaxFiles       = serverMaxFiles
	overrideMaxBytes int64 = serverMaxBytes
)

// ExtractOverrides 将整合包内的覆盖目录依序解压到实例目录。
// 先按 zip 里写的大小把要解的文件数一遍，个数或总大小超过上限就一个都不写。
// 实际大小由 zip 库核对：每读一次都查有没有超过写的大小，读到结尾再查够不够、校验和对不对，对不上就报错；
// 写的时候再套一层 LimitReader 兜底。以前没有上限，一个 8 MB 的整合包，overrides 里能解出 8 GB，
// 导入一个恶意整合包就能把磁盘写满（v3.1.0 正式版前检查 B-4）。后面的覆盖目录照旧后写，同名文件以它为准。
// 跳过哪些条目和解现成服务端时一样：目录（Windows 工具打的包里，目录可能写成反斜杠结尾）、符号链接这类不是普通文件的。
func ExtractOverrides(p *Pack, destDir string, writeFile func(rel string, r io.Reader) error) error {
	zr, err := zipguard.Open(p.ZipPath, zipguard.Default)
	if err != nil {
		return err
	}
	defer zr.Close()
	ents, total, err := overrideEntries(zr.File, p.OverrideDirs)
	if err != nil {
		return err
	}
	if len(ents) > overrideMaxFiles {
		return fmt.Errorf("整合包的覆盖文件有 %d 个，超过了一次能导入的上限（%d 个）", len(ents), overrideMaxFiles)
	}
	if total > overrideMaxBytes {
		return fmt.Errorf("整合包的覆盖文件解开后有 %.1f GB，超过了一次能导入的上限（%d GB）", float64(total)/(1<<30), overrideMaxBytes>>30)
	}
	for _, e := range ents {
		rc, err := e.f.Open()
		if err != nil {
			return err
		}
		werr := writeFile(e.rel, io.LimitReader(rc, e.size+1))
		rc.Close()
		if werr != nil {
			return werr
		}
	}
	return nil
}

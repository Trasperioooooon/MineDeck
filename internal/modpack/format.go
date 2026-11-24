package modpack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"minedeck/internal/mcsrc"
	"minedeck/internal/zipguard"
)

// 清单不在压缩包最外层时，常见的是作者把整个文件夹又压了一遍：整合包名/manifest.json。
// 套一层的照旧在各个顶层文件夹里找，整包里只能有一处；mods、config 这种内容目录里碰巧有个同名文件，不当成清单。
// 再往里只沿着「这一层只有一个文件夹、没有别的文件」往下剥，最多剥到第三层（A41，见 zipLayout.wrapped）。

// 解压现成服务端时的上限。正常的服务器（世界、模组、配置）到不了这么大；
// 用来挡住「声明很小、展开巨大」的压缩包。
const (
	serverMaxFiles = 200_000
	serverMaxBytes = 8 << 30
	maxJarInspect  = 96 << 20 // 比这还大的 jar 先看文件名，导入落到磁盘后再用接管那套完整打开
)

// indexSniffMax 找清单时要看 manifest.json 的内容（见 isIndexFile），一次最多解开这么多字节，和解析清单的上限一样。
// 超了的不看内容、只按文件名算（和以前一样），免得压缩包里塞上几千份大 manifest.json，导入时反复解压到卡住。
// 写成变量只是为了测试能调小。
var indexSniffMax int64 = 64 << 20

// indexSniffMin 每看一份至少按这么多扣：申报大小写成 0 的也要扣，一次能看的份数就有了上限（64 MB 里最多 1024 份）。
const indexSniffMin = 64 << 10

var (
	reLegacyForge = regexp.MustCompile(`(?i)^forge-(\d+(?:\.\d+)+)-(\d+(?:\.\d+)+)(?:-shim)?\.jar$`)
	reArgsFile    = regexp.MustCompile(`(?i)^libraries/net/(neoforged/neoforge|minecraftforge/forge)/([^/]+)/win_args\.txt$`)
	reMCInName    = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)
)

// indexLoc 找到的那一份清单。root 为空表示在压缩包最外层。
type indexLoc struct {
	file *zip.File
	kind string // mrpack | curseforge
	root string
}

type nestHit struct {
	mr *zip.File
	cf *zip.File
}

// findIndex 找 Modrinth / CurseForge 的清单。
// 最外层有就用最外层（和以前一样，zip 里谁排在前面用谁）。
// 否则只接受唯一的一层包装目录，再不然沿着单独一个文件夹往里剥（最多三层）。两处都有、或同一层里两种清单都在，不猜。
// nestedOK 为 false 时文件夹里的不看：最外层自己已经像一台服务端（A40：网页文件、插件数据里常有同名的 manifest.json）。
func findIndex(files []*zip.File, l zipLayout, nestedOK bool) (*indexLoc, error) {
	nests := map[string]*nestHit{}
	var order []string
	budget := indexSniffMax
	deep := ""
	if nestedOK {
		deep = l.wrapped()
		if slices.ContainsFunc(strings.Split(deep, "/"), func(d string) bool { return !isWrapperDir(d) }) {
			deep = "" // 剥的路上有 mods、config 这种内容目录：不是包装文件夹
		}
	}
	for _, f := range files {
		name := zipNameOf(f)
		if name == "" || isZipDir(f, name) {
			continue
		}
		if !strings.Contains(name, "/") {
			if kind, ok := indexKind(name); ok && isIndexFile(f, kind, &budget, false) {
				return &indexLoc{file: f, kind: kind}, nil // 最外层的就用它，后面的不用再看
			}
			continue
		}
		if !nestedOK {
			continue
		}
		dir, base, ok := wrapperLevel(name, deep)
		if !ok {
			continue
		}
		kind, ok := indexKind(base)
		if !ok {
			continue
		}
		n := nests[dir]
		if n != nil && (kind == FormatMrpack && n.mr != nil || kind == FormatCurseForge && n.cf != nil) {
			continue // 这一层已经有一份同类的了（zip 里可以有同名的条目）：用前面那份，后面的不再解开看
		}
		if !isIndexFile(f, kind, &budget, true) {
			continue
		}
		if n == nil {
			n = &nestHit{}
			nests[dir] = n
			order = append(order, dir)
		}
		if kind == FormatMrpack {
			n.mr = f
		} else {
			n.cf = f
		}
	}
	switch len(nests) {
	case 0:
		return nil, nil
	case 1:
		dir := order[0]
		n := nests[dir]
		if n.mr != nil && n.cf != nil {
			return nil, hinted("同一个文件夹里两种整合包清单都有，没法判断是哪种", "「%s」里同时有 modrinth.index.json 和 manifest.json，没法判断是哪种整合包。请只留一份再导入。", dir)
		}
		if n.mr != nil {
			return &indexLoc{file: n.mr, kind: FormatMrpack, root: dir}, nil
		}
		return &indexLoc{file: n.cf, kind: FormatCurseForge, root: dir}, nil
	default:
		sort.Strings(order)
		shown := order
		if len(shown) > 4 {
			shown = append(append([]string{}, order[:4]...), "…")
		}
		return nil, hinted("压缩包里有不止一份整合包清单，没法判断用哪份", "压缩包里有不止一处整合包清单（%s），没法判断该用哪一份。请只留一份，或把 modrinth.index.json / manifest.json 放到最外层再导入。", strings.Join(shown, "、"))
	}
}

func indexKind(base string) (string, bool) {
	switch base {
	case "modrinth.index.json":
		return FormatMrpack, true
	case "manifest.json":
		return FormatCurseForge, true
	default:
		return "", false
	}
}

// isIndexFile 文件名对上以后，再看一眼内容是不是真的整合包清单。
// manifest.json 这个名字不止 CurseForge 在用。最常见的是 ServerPackCreator 打的服务端包：最外层放一份它自己的
// manifest.json（files 是一串路径，带 serverPackCreatorVersion）。以前它被当成 CurseForge 清单去解析，
// 报一句 JSON 类型错误，走不到服务端的识别。CurseForge 的清单写着 manifestType: minecraftModpack；
// 老一些或别的工具导出的可能没写这一项，但一定有 minecraft 那一段（版本和加载器在里面）。两样都没有的不算。
// 最外层的读不出来、不是 JSON 的仍然当成清单，让解析那一步把原因报出来。
// budget 是这一次找清单还能解开的字节数（从 indexSniffMax 起扣，每份至少扣 indexSniffMin），不够看这一份的，最外层的也按清单算。
// nested（在文件夹里的）从严：要读得出来，而且明写着 manifestType: minecraftModpack、或者 minecraft 那一段写着版本才算（A40）。
// 文件夹里的 manifest.json 多半是别的东西（网页文件、插件的数据），读不出来就当成清单的话，整包会被当成
// CurseForge 整合包、报一句看不懂的错。
func isIndexFile(f *zip.File, kind string, budget *int64, nested bool) bool {
	if kind != FormatCurseForge {
		return true
	}
	size := int64(f.UncompressedSize64)
	cost := max(size, indexSniffMin)
	if size < 0 || cost > *budget {
		return !nested
	}
	*budget -= cost
	b, err := readZipEntry(f, size) // zip 库读的时候会核对实际大小，写小了、写大了都读不出来
	if err != nil {
		return !nested
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")) // 记事本这类存出来的开头带 BOM，json 不认
	var m struct {
		ManifestType string          `json:"manifestType"`
		Minecraft    json.RawMessage `json:"minecraft"`
	}
	if json.Unmarshal(b, &m) != nil {
		return !nested
	}
	if m.ManifestType != "" && m.ManifestType != "minecraftModpack" {
		return false
	}
	if nested && m.ManifestType == "" {
		// 明写了 manifestType: minecraftModpack 的就是 CurseForge 清单，版本空着也算，由解析那一步报出具体缺什么
		var mc struct {
			Version string `json:"version"`
		}
		return json.Unmarshal(m.Minecraft, &mc) == nil && strings.TrimSpace(mc.Version) != ""
	}
	return m.ManifestType != "" || len(m.Minecraft) > 0 && string(m.Minecraft) != "null"
}

// wrapperLevel name 是不是直接放在某个包装文件夹里的文件：套一层的「文件夹/文件」（文件夹名不能是 mods 这种内容目录），
// 或者正好在 deep（沿着单独一个文件夹剥进去的那一层，见 zipLayout.wrapped）里。
func wrapperLevel(name, deep string) (dir, base string, ok bool) {
	if dir, base, ok := oneLevel(name); ok {
		return dir, base, isWrapperDir(dir)
	}
	if deep == "" {
		return "", "", false
	}
	base, ok = strings.CutPrefix(name, deep+"/")
	if !ok || base == "" || strings.Contains(base, "/") {
		return "", "", false
	}
	return deep, base, true
}

// oneLevel 路径是不是「刚好一层目录 + 文件名」。../、绝对路径、再深一层都不算。
func oneLevel(name string) (dir, base string, ok bool) {
	dir, base, ok = strings.Cut(name, "/")
	if !ok || base == "" || strings.Contains(base, "/") {
		return "", "", false
	}
	clean, err := safeRelPath(dir)
	if err != nil || clean != dir {
		return "", "", false
	}
	return dir, base, true
}

// isWrapperDir 这一层像不像「把整个整合包文件夹又压了一遍」的目录名。
// mods、config、overrides 是内容目录，里面的 manifest.json 多半是某个模组自己的，不能当成整合包清单。
func isWrapperDir(dir string) bool {
	switch strings.ToLower(dir) {
	case "mods", "plugins", "config", "libraries", "overrides", "server-overrides",
		"resourcepacks", "shaderpacks", "datapacks", "kubejs", "defaultconfigs",
		"scripts", "logs", "crash-reports", "versions", ".fabric", "world", "worlds",
		"cache", "meta-inf":
		return false
	default:
		return true
	}
}

// joinRoot 把清单所在的那一层加到覆盖目录前面。root 为空时目录原样返回。
func joinRoot(root, dir string) string {
	dir = strings.Trim(strings.ReplaceAll(dir, "\\", "/"), "/")
	if root == "" {
		return dir
	}
	if dir == "" {
		return root
	}
	return root + "/" + dir
}

func zipNameOf(f *zip.File) string {
	return strings.ReplaceAll(f.Name, "\\", "/")
}

func isZipDir(f *zip.File, name string) bool {
	return strings.HasSuffix(name, "/") || f.FileInfo().IsDir()
}

// ---------- 现成服务端压缩包 ----------

// hintError 报错原文之外另给一句给人看的话（Hint）。原文里有长长的下一步说明，界面的提示正文只放得下一句，
// 正文接这一句，原文进「详情」。web 的 writeErr 会把它放进响应的 hint（同 internal/ai 的 APIError.Hint）。
type hintError struct {
	hint, msg string
	kind      error // 哪一类（errServerUnknown、errUnrecognized），给 errors.Is 认；别的是 nil
}

func (e *hintError) Error() string { return e.msg }
func (e *hintError) Hint() string  { return e.hint }
func (e *hintError) Unwrap() error { return e.kind }

func hinted(hint, format string, a ...any) error {
	return &hintError{hint: hint, msg: fmt.Sprintf(format, a...)}
}

var (
	errServerUnknown = errors.New("像服务端，没认出核心")
	errUnrecognized  = errors.New("没认出这个压缩包")
)

// ServerUnknownError 能看出是服务端、但核心或版本认不出来时给用户的说明。
func ServerUnknownError() error {
	return &hintError{kind: errServerUnknown, hint: "看着像现成服务端，但没认出核心和版本", msg: "这个压缩包像是一台现成的服务端（有 mods、plugins、server.properties 或服务端 jar），但没认出核心和 Minecraft 版本。请确认 Forge / NeoForge 的 libraries，或 Fabric / Paper / 原版的服务端 jar 就在里面（可以套在文件夹里：套一层的随便套，只有一个文件夹一层层套着的最多认三层）。只有启动脚本或加载器安装器的 Server Pack，需要先解压，按包作者的说明运行包里的启动脚本或安装器，把加载器装好，再用「新建服务器 → 接管已有服务器」选中那个文件夹。加载器已装好但仍认不出时，也可以解压后用接管，类型和版本可以自己填。"}
}

func unrecognizedPackError() error {
	return &hintError{kind: errUnrecognized, hint: "没认出这个压缩包，既没有整合包清单，也看不出是服务端", msg: "无法识别这个压缩包。Modrinth 整合包要有 modrinth.index.json，CurseForge 整合包要有 manifest.json，放在最外层，或套在文件夹里（例如「整合包名/manifest.json」；只有一个文件夹一层层套着的，最多认三层）。如果这是作者给的现成服务端，压缩包里应能看到 mods、plugins、libraries、server.properties、eula.txt 或服务端 jar。也可以先解压，再用「新建服务器 → 接管已有服务器」选中那个文件夹。"}
}

func ambiguousServerError(dirs []string) error {
	sort.Strings(dirs)
	shown := dirs
	if len(shown) > 4 {
		shown = append(append([]string{}, dirs[:4]...), "…")
	}
	return hinted("压缩包里有好几台服务端，一次只能导入一台", "压缩包里有好几个看起来像服务端的文件夹（%s），没法判断该导入哪一个。请只打包其中一台，或用「新建服务器 → 接管已有服务器」选中具体的文件夹。", strings.Join(shown, "、"))
}

// recognizeServer 没有整合包清单时，看是不是一台现成的服务端（可能也套在文件夹里）。outer 见 serverRoot。
// 哪一层都不像服务端时，退一步看最外层（或剥到最里面的那层包装）里的 jar，按内容认得出核心就是服务端（A40）：
// 只有核心加启动脚本、没开过服的包，核心常改了名（leaf-1.21.4.jar、core.jar），名字不在 knownServerJar 里。
func recognizeServer(zipPath string, files []*zip.File, l zipLayout, outer bool) (*Pack, error) {
	root, roots, ok := serverRoot(l, outer)
	byContent := false
	if !ok {
		if len(roots) > 1 {
			return nil, ambiguousServerError(roots)
		}
		root, byContent = l.jarLevel(), true
		if lv := l[root]; lv == nil || lv.jars == 0 {
			return nil, unrecognizedPackError()
		}
	}
	if err := auditServerPaths(files); err != nil {
		return nil, err
	}
	cands, unread := serverCands(files, root)
	if byContent {
		// 退路只认打开看过、认出了核心的 jar：名字和内容都认不出的不算，不然随便一个工具 jar 都成了服务端
		cands = slices.DeleteFunc(cands, func(c srvCand) bool { return c.core == "" && !c.quilt })
		if len(cands) == 0 {
			return nil, unrecognizedPackError()
		}
		unread = false
	}
	if len(cands) == 0 {
		if unread {
			// jar 太大，这里没打开。先收下，解压后用接管已有服务器的识别再看。
			return serverPack(zipPath, root, srvCand{}), nil
		}
		return nil, ServerUnknownError()
	}
	sortCands(cands)
	best := cands[0]
	if best.quilt {
		return nil, hinted("暂不支持 Quilt，可以换用 Fabric 版", "暂不支持 Quilt 加载器。可以换用 Fabric 版，或者解压后用「新建服务器 → 接管已有服务器」手动选一个接近的类型。")
	}
	if best.core == "" && !unread {
		return nil, ServerUnknownError()
	}
	if best.mc == "" {
		if id := versionJSONAt(files, root); id != "" && best.core != mcsrc.CoreFabric {
			best.mc = id
		}
	}
	fromLog := false
	if best.mc == "" && best.core != "" && mcsrc.KindOf(best.core) != mcsrc.KindProxy {
		best.mc = logVersionAt(files, root)
		fromLog = best.mc != ""
	}
	p := serverPack(zipPath, root, best)
	p.MCFromLog = fromLog
	return p, nil
}

// reLogVersion 服务端开服时写进 logs/latest.log 的那一行（同接管识别 inst 的 reLogMC）。版本号只收像样的写法。
var reLogVersion = regexp.MustCompile(`Starting minecraft server version ([0-9][0-9A-Za-z._-]{0,31})`)

// logVersionAt 从压缩包里 root 那一层的 logs/latest.log 认 Minecraft 版本：jar、version.json 都认不出时的最后一招（A44）。
// 日志不解压（控制目录，见 inst 的 IsControlFile），解压以后接管那套识别看不到它，所以在这里认。
// 只看开头 512 KB：这一行在开服的头几秒就写了，日志可能很大。是上一次开服时的版本，导入时会说明。
func logVersionAt(files []*zip.File, root string) string {
	for _, f := range files {
		rel, ok := relUnder(zipNameOf(f), root)
		if !ok || !strings.EqualFold(rel, "logs/latest.log") || !f.Mode().IsRegular() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		b, _ := io.ReadAll(io.LimitReader(rc, 512<<10))
		rc.Close()
		if m := reLogVersion.FindSubmatch(b); m != nil {
			return string(m[1])
		}
		return ""
	}
	return ""
}

// ServerRootFiles 读现成服务端压缩包里服务端那一层（Root）的几个小文件，比如启动脚本：解压时它们不落盘（控制文件），
// 要看里面写了什么只能从压缩包里读。names 不分大小写，返回的键是小写的名字；比 max 大的、读不出来的不给。
// 同一个名字（不分大小写）出现几次时用前面那个，和解压时只差大小写的留前一个一致。
func ServerRootFiles(p *Pack, names []string, max int64) map[string][]byte {
	out := map[string][]byte{}
	if p == nil || p.Format != FormatServer {
		return out
	}
	zr, err := zipguard.Open(p.ZipPath, zipguard.Default)
	if err != nil {
		return out
	}
	defer zr.Close()
	want := map[string]bool{}
	for _, n := range names {
		want[strings.ToLower(n)] = true
	}
	seen := map[string]bool{}
	for _, f := range zr.File {
		rel, ok := relUnder(zipNameOf(f), p.Root)
		low := strings.ToLower(rel)
		if !ok || !want[low] || seen[low] || !f.Mode().IsRegular() {
			continue
		}
		seen[low] = true
		if b, err := readZipEntry(f, max); err == nil {
			out[low] = b
		}
	}
	return out
}

func serverPack(zipPath, root string, c srvCand) *Pack {
	name := path.Base(root) // 套了几层的取最里面那个文件夹的名字
	if root == "" {
		name = strings.TrimSuffix(filepath.Base(zipPath), filepath.Ext(zipPath))
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		name = "服务器"
	}
	return &Pack{
		Format: FormatServer, Name: name, ZipPath: zipPath, Root: root,
		Core: c.core, MC: c.mc, Loader: c.build, Launch: c.launch,
	}
}

// DisplayNameFromUpload 把上传时的原文件名整理成显示用的名字。只用于展示，不能拿去拼任何路径。
func DisplayNameFromUpload(fileName string) string {
	s := strings.ReplaceAll(fileName, "\\", "/")
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	if ext := path.Ext(s); ext != "" {
		s = strings.TrimSuffix(s, ext)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	s = strings.TrimFunc(b.String(), func(r rune) bool {
		return r == '.' || unicode.IsSpace(r)
	})
	if utf8.RuneCountInString(s) > 80 {
		s = string([]rune(s)[:80])
	}
	return s
}

// maxWrapDepth 包装文件夹最多剥几层（A41）。
const maxWrapDepth = 3

// zipLayout 压缩包最外几层（到 maxWrapDepth 为止）每一层直接放着什么：键是这一层的路径（最外层是空串）。
// 扫一遍条目就建好，找清单和找服务端在哪一层都查它。以前对每个顶层文件夹都把全部条目扫一遍，
// 八千个顶层文件夹要一秒多（A40）。
type zipLayout map[string]*zipLevel

type zipLevel struct {
	files  int             // 直接放在这一层的文件个数
	dirs   map[string]bool // 直接的子文件夹
	jars   int             // 直接放在这一层的 jar（不算安装器，见 isRootJar）
	server bool            // 这一层像服务端（见 serverEntry）
	strong bool            // 像服务端的凭据里有 server.properties 或名字认得出的服务端 jar（见 strongServerEntry）
}

func newZipLayout(files []*zip.File) zipLayout {
	l := zipLayout{}
	level := func(p string) *zipLevel {
		lv := l[p]
		if lv == nil {
			lv = &zipLevel{dirs: map[string]bool{}}
			l[p] = lv
		}
		return lv
	}
	for _, f := range files {
		name := zipNameOf(f)
		isDir := isZipDir(f, name)
		name = strings.TrimSuffix(name, "/")
		if name == "" || macJunk(name, isDir) {
			continue
		}
		if clean, err := safeRelPath(name); err != nil || clean != name {
			continue // ../、盘符这类：服务端压缩包整包拒绝（auditServerPaths），这里不算它在哪一层
		}
		segs := strings.Split(name, "/")
		for d := 0; d < len(segs) && d <= maxWrapDepth; d++ {
			lv := level(strings.Join(segs[:d], "/"))
			if d < len(segs)-1 || isDir {
				lv.dirs[segs[d]] = true
				lv.server = lv.server || serverEntry(segs[d], true)
				continue
			}
			lv.files++
			lv.server = lv.server || serverEntry(segs[d], false)
			lv.strong = lv.strong || strongServerEntry(segs[d])
			if isRootJar(segs[d]) {
				lv.jars++
			}
		}
	}
	return l
}

// macJunk Mac 上打 zip 时夹带的东西：最外层的 __MACOSX/（资源分支），各层的 .DS_Store 和 ._ 开头的 AppleDouble 文件。
// 数一层里有什么时不算它们，不然最外层多出一个文件夹、中间一层多出一个文件，包装文件夹就剥不进去了（A41）。
func macJunk(name string, isDir bool) bool {
	first, _, _ := strings.Cut(name, "/")
	if strings.EqualFold(first, "__MACOSX") {
		return true
	}
	base := path.Base(name)
	return !isDir && (base == ".DS_Store" || strings.HasPrefix(base, "._"))
}

// looksLikeServer 压缩包里 p 这一层看起来是不是一台服务端。
func (l zipLayout) looksLikeServer(p string) bool {
	lv := l[p]
	return lv != nil && lv.server
}

// weakServer p 这一层像服务端，凭据却只有 eula.txt、mods、plugins、libraries 这类旁证（没有 server.properties、
// 也没有名字认得出的服务端 jar）：认不出核心时，真正的服务端或整合包清单可能在里面一层（A40 的退路，见 parseFiles）。
func (l zipLayout) weakServer(p string) bool {
	lv := l[p]
	return lv != nil && lv.server && !lv.strong
}

// innermost 从最外层起，这一层只有一个文件夹、没有别的文件就往里剥一层，最多剥 maxWrapDepth 层（A41）：返回剥到的那一层，
// 一层都剥不了是空串。
func (l zipLayout) innermost() string {
	cur := ""
	for depth := 0; depth < maxWrapDepth; depth++ {
		lv := l[cur]
		if lv == nil || lv.files != 0 || len(lv.dirs) != 1 {
			break
		}
		for d := range lv.dirs {
			cur = path.Join(cur, d)
		}
	}
	return cur
}

// wrapped 同 innermost，只剥了一层时也返回空串：一层的照旧按「各个顶层文件夹里找」认，不靠这里。
func (l zipLayout) wrapped() string {
	if cur := l.innermost(); strings.Contains(cur, "/") {
		return cur
	}
	return ""
}

// jarLevel 哪一层都不像服务端时，退一步按内容认 jar 的那一层（A40 的退路，见 recognizeServer）：最外层有文件的就是最外层，
// 最外层只是包装文件夹的，剥到最里面那层（路上不能有 mods、config 这种内容目录）。
func (l zipLayout) jarLevel() string {
	cur := l.innermost()
	if slices.ContainsFunc(strings.Split(cur, "/"), func(d string) bool { return cur != "" && !isWrapperDir(d) }) {
		return ""
	}
	return cur
}

// serverEntry 直接放在某一层的这一项算不算服务端的特征：server.properties、eula.txt、mods、plugins、libraries 文件夹，
// 或者名字认得出的服务端 jar（knownServerJar）。名字认不出的 jar 单独一个不算（A40）：套了一层的服务端旁边多放一个
// 别的 jar（工具、备份的旧核心），以前最外层就算「像服务端」，里面那台反而认不出了。
func serverEntry(name string, isDir bool) bool {
	low := strings.ToLower(name)
	if isDir {
		return low == "mods" || low == "plugins" || low == "libraries"
	}
	return low == "eula.txt" || strongServerEntry(name)
}

// strongServerEntry 一个文件单独就能说明这一层是服务端本身：server.properties、名字认得出的服务端 jar。
func strongServerEntry(name string) bool {
	return strings.EqualFold(name, "server.properties") || knownServerJar(name)
}

// knownServerJar 文件名看得出是服务端或加载器的启动 jar。和 serverCands 认核心时看文件名的那一套一致（candFromName），
// 再加上原版、Spigot / CraftBukkit、BungeeCord 和 Taiyitist 的常见名字：这几种是按 jar 里的主类认的，名字不在那一套里。
// 安装器、BuildTools 不算。
func knownServerJar(name string) bool {
	if !isRootJar(name) {
		return false
	}
	if _, ok := candFromName(name); ok {
		return true
	}
	stem := strings.TrimSuffix(strings.ToLower(name), ".jar")
	if stem == "server" {
		return true
	}
	for _, p := range []string{"minecraft_server", "spigot", "craftbukkit", "bungeecord", "taiyitist"} {
		if strings.HasPrefix(stem, p) {
			return true
		}
	}
	return false
}

// serverRoot 服务端文件在压缩包的哪一层。outer 为 true 时最外层自己像服务端就用最外层（旁边再套一个文件夹时不猜里面那个）；
// 不然看各个顶层文件夹，正好一个像服务端就用它；再不然沿着单独一个文件夹往里剥（最多三层，A41）。
// nested 是像服务端的顶层文件夹，不止一个时调用方报「好几台」。
func serverRoot(l zipLayout, outer bool) (root string, nested []string, ok bool) {
	if outer && l.looksLikeServer("") {
		return "", nil, true
	}
	if top := l[""]; top != nil {
		for dir := range top.dirs {
			// mods、plugins、libraries 本身是服务端的旁证，不会是套着一台服务端的那层文件夹：里面的 fabric-api-*.jar 这类
			// 名字像服务端 jar，不跳过的话，最外层只多一个 mods/ 时往里看，会和真正那台一起被当成「好几台」
			if serverEntry(dir, true) {
				continue
			}
			if l.looksLikeServer(dir) {
				nested = append(nested, dir)
			}
		}
	}
	switch {
	case len(nested) == 1:
		return nested[0], nested, true
	case len(nested) > 1:
		return "", nested, false
	}
	if deep := l.wrapped(); deep != "" && l.looksLikeServer(deep) {
		return deep, nil, true
	}
	return "", nil, false
}

func isRootJar(rel string) bool {
	low := strings.ToLower(rel)
	return !strings.Contains(rel, "/") && strings.HasSuffix(low, ".jar") &&
		!strings.Contains(low, "installer") && !strings.Contains(low, "buildtools")
}

// relUnder root 为空时整条路径都算；否则必须落在 root/ 下面。
func relUnder(name, root string) (string, bool) {
	if name == "" || isDirName(name) {
		return "", false
	}
	if root == "" {
		return name, true
	}
	rest, ok := strings.CutPrefix(name, root+"/")
	return rest, ok && rest != ""
}

func isDirName(name string) bool { return strings.HasSuffix(name, "/") }

// auditServerPaths 服务端压缩包几乎每个文件都会落盘，所以每条路径都要过 safeRelPath。
// 在最外层的 ../、盘符、绝对路径，哪怕不在要解压的那一层里，也整包拒绝。
func auditServerPaths(files []*zip.File) error {
	for _, f := range files {
		name := strings.TrimSuffix(zipNameOf(f), "/")
		if name == "" {
			continue
		}
		if _, err := safeRelPath(name); err != nil {
			return err
		}
	}
	return nil
}

type srvCand struct {
	core   mcsrc.Core
	mc     string
	build  string
	launch string
	rank   int
	quilt  bool
}

const (
	rankArgs    = 50
	rankLoader  = 40
	rankServer  = 30
	rankUnknown = 20
	rankVanilla = 10
)

// serverCands 从参数文件和最外层的 jar 里认核心。规则和 internal/inst 接管已有服务器时看 jar 的办法对齐，
// 导入落到磁盘之后还会用那套再认一遍，以那边为准（这边认错了、或者 jar 太大没打开，都还能补上）。
func serverCands(files []*zip.File, root string) (out []srvCand, unread bool) {
	jars := 0
	for _, f := range files {
		name := zipNameOf(f)
		rel, ok := relUnder(name, root)
		if !ok || isZipDir(f, name) {
			continue
		}
		low := strings.ToLower(rel)
		if m := reArgsFile.FindStringSubmatch(low); m != nil {
			c := srvCand{rank: rankArgs, launch: "args:" + rel}
			if m[1] == "neoforged/neoforge" {
				c.core = mcsrc.CoreNeoForge
				c.build = m[2]
			} else {
				c.core = mcsrc.CoreForge
				if mc, build, ok := strings.Cut(m[2], "-"); ok {
					c.mc, c.build = mc, build
				}
			}
			fillFromArgs(f, &c)
			out = append(out, c)
			continue
		}
		if !isRootJar(rel) {
			continue
		}
		if jars++; jars > 40 {
			continue
		}
		c, opened := candFromJar(f, rel)
		if !opened {
			unread = true
		}
		if c.launch != "" {
			out = append(out, c)
		}
	}
	return out, unread
}

func fillFromArgs(f *zip.File, c *srvCand) {
	b, err := readZipEntry(f, 1<<20)
	if err != nil {
		return
	}
	toks := strings.Fields(string(b))
	for i := 0; i+1 < len(toks); i++ {
		switch toks[i] {
		case "--fml.mcVersion":
			c.mc = toks[i+1]
		case "--fml.forgeVersion", "--fml.neoForgeVersion":
			c.build = toks[i+1]
		}
	}
}

func candFromJar(f *zip.File, rel string) (srvCand, bool) {
	name := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		name = rel[i+1:]
	}
	if c, ok := candFromName(name); ok && c.quilt {
		return c, true
	}
	b, err := readZipEntry(f, maxJarInspect)
	if err != nil {
		if c, ok := candFromName(name); ok {
			return c, false
		}
		return srvCand{launch: "jar:" + name, rank: rankUnknown}, false
	}
	zr, err := zipguard.NewReader(bytes.NewReader(b), int64(len(b)), zipguard.Default)
	if err != nil {
		return srvCand{}, true // 读出来了却不是 jar：不是能启动的服务端，也不按文件名猜
	}
	files := map[string][]byte{}
	for _, inner := range zr.File {
		switch inner.Name {
		case "META-INF/MANIFEST.MF", "META-INF/versions.list", "version.json", "install.properties", "patch.properties":
			if raw, err := readZipEntry(inner, 1<<20); err == nil {
				files[inner.Name] = raw
			}
		}
		if len(files) == 5 {
			break
		}
	}
	return candFromJarBytes(name, files), true
}

func candFromName(name string) (srvCand, bool) {
	low := strings.ToLower(name)
	stem := strings.TrimSuffix(low, ".jar")
	c := srvCand{launch: "jar:" + name}
	if strings.Contains(stem, "quilt") {
		c.quilt, c.rank = true, rankLoader
		return c, true
	}
	if m := reLegacyForge.FindStringSubmatch(name); m != nil {
		c.core, c.mc, c.build, c.rank = mcsrc.CoreForge, m[1], m[2], rankLoader
		return c, true
	}
	mc := ""
	if !strings.Contains(stem, "fabric") { // Fabric 文件名里常是加载器版本，不能当成 Minecraft 版本
		mc = reMCInName.FindString(stem)
	}
	switch {
	case strings.HasPrefix(stem, "purpur"):
		c.core, c.mc, c.rank = mcsrc.CorePurpur, mc, rankServer
	case strings.HasPrefix(stem, "folia"):
		c.core, c.mc, c.rank = mcsrc.CoreFolia, mc, rankServer
	case strings.HasPrefix(stem, "leaves"):
		c.core, c.mc, c.rank = mcsrc.CoreLeaves, mc, rankServer
	case strings.HasPrefix(stem, "paper"), strings.HasPrefix(stem, "paperclip"):
		c.core, c.mc, c.rank = mcsrc.CorePaper, mc, rankServer
	case strings.HasPrefix(stem, "fabric"):
		c.core, c.rank = mcsrc.CoreFabric, rankLoader
	case strings.HasPrefix(stem, "mohist"):
		c.core, c.mc, c.rank = mcsrc.CoreMohist, mc, rankLoader
	case strings.HasPrefix(stem, "banner"):
		c.core, c.mc, c.rank = mcsrc.CoreBanner, mc, rankLoader
	case strings.HasPrefix(stem, "velocity"):
		c.core, c.mc, c.rank = mcsrc.CoreVelocity, mc, rankServer
	case strings.HasPrefix(stem, "waterfall"):
		c.core, c.mc, c.rank = mcsrc.CoreWaterfall, mc, rankServer
	default:
		return srvCand{}, false
	}
	return c, c.core != "" || c.quilt
}

func candFromJarBytes(name string, files map[string][]byte) srvCand {
	lowName := strings.ToLower(name)
	if strings.Contains(strings.TrimSuffix(lowName, ".jar"), "quilt") {
		return srvCand{quilt: true, rank: rankLoader, launch: "jar:" + name}
	}
	mf := parseManifest(files["META-INF/MANIFEST.MF"])
	main := mf["Main-Class"]
	lmain := strings.ToLower(main)
	var vj struct {
		ID   string `json:"id"`
		Java int    `json:"java_version"`
	}
	if b := files["version.json"]; b != nil {
		_ = json.Unmarshal(b, &vj)
	}
	brand, listMC := bundlerVersion(files["META-INF/versions.list"])
	inner := firstNonEmpty(listMC, vj.ID)
	c := srvCand{launch: "jar:" + name, mc: inner}
	switch {
	case strings.Contains(lmain, "quilt") || strings.Contains(lowName, "quilt"):
		c.quilt, c.rank = true, rankLoader
	case strings.Contains(lmain, "leavesclip"):
		c.core, c.rank = mcsrc.CoreLeaves, rankServer
		c.mc = firstNonEmpty(inner, propOf(files["patch.properties"], "version"))
	case strings.Contains(lmain, "paperclip"):
		c.rank = rankServer
		c.mc = firstNonEmpty(inner, propOf(files["patch.properties"], "version"))
		if brand == "" || brand == "server" {
			brand, _, _ = strings.Cut(strings.TrimSuffix(lowName, ".jar"), "-")
		}
		switch brand {
		case "purpur":
			c.core = mcsrc.CorePurpur
		case "folia":
			c.core = mcsrc.CoreFolia
		case "leaves":
			c.core = mcsrc.CoreLeaves
		default:
			c.core = mcsrc.CorePaper
		}
	case main == "net.minecraft.bundler.Main" || strings.HasPrefix(main, "net.minecraft.server."):
		c.core, c.rank = mcsrc.CoreVanilla, rankVanilla
	case strings.HasPrefix(main, "net.fabricmc."):
		c.core, c.rank = mcsrc.CoreFabric, rankLoader
		ip := files["install.properties"]
		c.mc, c.build = propOf(ip, "game-version"), propOf(ip, "fabric-loader-version")
	case strings.HasPrefix(main, "com.velocitypowered."):
		c.core, c.rank = mcsrc.CoreVelocity, rankServer
		c.mc, _, _ = strings.Cut(mf["Implementation-Version"], " ")
	case strings.HasPrefix(main, "net.md_5.bungee.") || strings.Contains(lmain, "waterfall"):
		c.core, c.rank = mcsrc.CoreWaterfall, rankServer
		c.mc = reMCInName.FindString(mf["Implementation-Version"])
	case strings.Contains(lmain, "banner"):
		c.core, c.rank = mcsrc.CoreBanner, rankLoader
	case strings.Contains(lmain, "mohist"):
		c.core, c.rank = mcsrc.CoreMohist, rankLoader
	case strings.HasPrefix(main, "net.minecraftforge.") || reLegacyForge.MatchString(name):
		c.core, c.rank = mcsrc.CoreForge, rankLoader
		if m := reLegacyForge.FindStringSubmatch(name); m != nil {
			c.mc, c.build = m[1], m[2]
		}
	case strings.HasPrefix(main, "org.bukkit.craftbukkit."):
		c.core, c.rank = mcsrc.CorePaper, rankServer
	default:
		// jar 打开了、Main-Class 却认不出：不按文件名猜。PaperLib-1.20.1.jar 这类库的名字里也有 paper 和版本号，
		// 猜了会登记出一台起不来的服务器。和接管的识别一样，认不出就是认不出；文件名只在 jar 太大没打开时用（candFromJar）。
		c.rank = rankUnknown
	}
	if c.mc == "" && c.core != mcsrc.CoreFabric && !c.quilt {
		c.mc = reMCInName.FindString(name)
	}
	return c
}

func versionJSONAt(files []*zip.File, root string) string {
	for _, f := range files {
		name := zipNameOf(f)
		rel, ok := relUnder(name, root)
		if !ok || !strings.EqualFold(rel, "version.json") {
			continue
		}
		b, err := readZipEntry(f, 1<<20)
		if err != nil {
			return ""
		}
		var vj struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(b, &vj) != nil {
			return ""
		}
		return strings.TrimSpace(vj.ID)
	}
	return ""
}

func sortCands(cands []srvCand) {
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].rank != cands[j].rank {
			return cands[i].rank > cands[j].rank
		}
		if cands[i].build != cands[j].build {
			return versionLess(cands[j].build, cands[i].build)
		}
		return versionLess(cands[j].mc, cands[i].mc)
	})
}

func versionLess(a, b string) bool {
	an, as := splitVer(a)
	bn, bs := splitVer(b)
	for i := 0; i < len(an) || i < len(bn); i++ {
		x, y := 0, 0
		if i < len(an) {
			x = an[i]
		}
		if i < len(bn) {
			y = bn[i]
		}
		if x != y {
			return x < y
		}
	}
	if (as == "") != (bs == "") {
		return as != ""
	}
	return as < bs
}

func splitVer(v string) (nums []int, rest string) {
	i := 0
	for i < len(v) {
		j := i
		for j < len(v) && v[j] >= '0' && v[j] <= '9' {
			j++
		}
		if j == i || j-i > 9 {
			break
		}
		n := 0
		for _, c := range v[i:j] {
			n = n*10 + int(c-'0')
		}
		nums = append(nums, n)
		i = j
		if i < len(v) && v[i] == '.' && i+1 < len(v) && v[i+1] >= '0' && v[i+1] <= '9' {
			i++
			continue
		}
		break
	}
	return nums, v[i:]
}

func readZipEntry(f *zip.File, max int64) ([]byte, error) {
	if int64(f.UncompressedSize64) > max {
		return nil, fmt.Errorf("太大")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("太大")
	}
	return b, nil
}

func parseManifest(b []byte) map[string]string {
	out := map[string]string{}
	last := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if line == "" {
			if len(out) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, " ") && last != "" {
			out[last] += line[1:]
			continue
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			last = strings.TrimSpace(k)
			out[last] = strings.TrimSpace(v)
		}
	}
	return out
}

func bundlerVersion(b []byte) (brand, mc string) {
	line, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	f := strings.Split(strings.TrimSpace(line), "\t")
	if len(f) < 3 {
		return "", ""
	}
	base := strings.TrimSuffix(strings.ToLower(strings.ReplaceAll(f[2], "\\", "/")), ".jar")
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	brand, _, _ = strings.Cut(base, "-")
	return brand, strings.TrimSpace(f[1])
}

func propOf(b []byte, key string) string {
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

// ServerFile 现成服务端压缩包里要交给 writeFile 的一个文件：剥掉 Root 之后的相对路径、zip 里写的大小。
type ServerFile struct {
	Rel  string
	Size int64
}

// ExtractServer 把现成服务端压缩包解进实例目录。Root 那一层会剥掉。
// 每条路径都过 safeRelPath，过不了就整包拒绝、一个文件都不写。
// 符号链接不展开。writeFile 拿到的 rel 已经是剥掉 Root 之后的相对路径。
// begin（可以是 nil）在数完要解的文件、写第一个之前调一次，拿到全部文件：调用方在这里按真要写的那些查磁盘余量
// （A40；writeFile 不落盘的控制文件不算），返回错误就一个文件都不写。
func ExtractServer(p *Pack, begin func(files []ServerFile) error, writeFile func(rel string, r io.Reader) error) (int, error) {
	if p == nil || p.Format != FormatServer {
		return 0, fmt.Errorf("这不是服务端压缩包")
	}
	zr, err := zipguard.Open(p.ZipPath, zipguard.Default)
	if err != nil {
		return 0, fmt.Errorf("无法打开整合包（需为 .mrpack 或 zip）: %w", err)
	}
	defer zr.Close()
	if err := auditServerPaths(zr.File); err != nil {
		return 0, err
	}
	type entry struct {
		f    *zip.File
		rel  string
		size int64
	}
	var ents []entry
	var total int64
	for _, f := range zr.File {
		name := zipNameOf(f)
		if name == "" || isZipDir(f, name) || !f.Mode().IsRegular() {
			continue
		}
		rel, ok := relUnder(name, p.Root)
		if !ok {
			continue
		}
		rel, err = safeRelPath(rel)
		if err != nil {
			return 0, err
		}
		size := int64(f.UncompressedSize64)
		if size < 0 || total+size < total {
			return 0, fmt.Errorf("压缩包里的文件大小不对: %s", f.Name)
		}
		total += size
		ents = append(ents, entry{f: f, rel: rel, size: size})
	}
	if len(ents) > serverMaxFiles {
		return 0, fmt.Errorf("压缩包里有 %d 个文件，超过了一次能导入的上限（%d 个）", len(ents), serverMaxFiles)
	}
	if total > serverMaxBytes {
		return 0, fmt.Errorf("压缩包解开后有 %.1f GB，超过了一次能导入的上限（%d GB）", float64(total)/(1<<30), serverMaxBytes>>30)
	}
	if begin != nil {
		list := make([]ServerFile, 0, len(ents))
		for _, e := range ents {
			list = append(list, ServerFile{Rel: e.rel, Size: e.size})
		}
		if err := begin(list); err != nil {
			return 0, err
		}
	}
	n := 0
	for _, e := range ents {
		rc, err := e.f.Open()
		if err != nil {
			return n, err
		}
		werr := writeFile(e.rel, io.LimitReader(rc, e.size+1))
		rc.Close()
		if werr != nil {
			return n, werr
		}
		n++
	}
	return n, nil
}

package modpack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"minedeck/internal/mcsrc"
)

// 清单不在压缩包最外层时，常见的是作者把整个文件夹又压了一遍：整合包名/manifest.json。
// 只认这一层，而且整包里只能有一处。mods、config 这种内容目录里碰巧有个同名文件，不当成清单。

// 解压现成服务端时的上限。正常的服务器（世界、模组、配置）到不了这么大；
// 用来挡住「声明很小、展开巨大」的压缩包。
const (
	serverMaxFiles = 200_000
	serverMaxBytes = 8 << 30
	maxJarInspect  = 96 << 20 // 比这还大的 jar 先看文件名，导入落到磁盘后再用接管那套完整打开
)

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
// 否则只接受唯一的一层包装目录。两处都有、或同一层里两种清单都在，不猜。
func findIndex(files []*zip.File) (*indexLoc, error) {
	var root *indexLoc
	nests := map[string]*nestHit{}
	var order []string
	for _, f := range files {
		name := zipNameOf(f)
		if name == "" || isZipDir(f, name) {
			continue
		}
		if !strings.Contains(name, "/") {
			kind, ok := indexKind(name)
			if !ok || root != nil {
				continue
			}
			root = &indexLoc{file: f, kind: kind}
			continue
		}
		dir, base, ok := oneLevel(name)
		if !ok || !isWrapperDir(dir) {
			continue
		}
		kind, ok := indexKind(base)
		if !ok {
			continue
		}
		n := nests[dir]
		if n == nil {
			n = &nestHit{}
			nests[dir] = n
			order = append(order, dir)
		}
		if kind == FormatMrpack && n.mr == nil {
			n.mr = f
		}
		if kind == FormatCurseForge && n.cf == nil {
			n.cf = f
		}
	}
	if root != nil {
		return root, nil
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
type hintError struct{ hint, msg string }

func (e *hintError) Error() string { return e.msg }
func (e *hintError) Hint() string  { return e.hint }

func hinted(hint, format string, a ...any) error {
	return &hintError{hint: hint, msg: fmt.Sprintf(format, a...)}
}

// ServerUnknownError 能看出是服务端、但核心或版本认不出来时给用户的说明。
func ServerUnknownError() error {
	return hinted("看着像现成服务端，但没认出核心和版本", "这个压缩包像是一台现成的服务端（有 mods、plugins、server.properties 或服务端 jar），但没认出核心和 Minecraft 版本。请确认 Forge / NeoForge 的 libraries，或 Fabric / Paper / 原版的服务端 jar 就在里面（可以套一层文件夹）。只有启动脚本或加载器安装器的 Server Pack，需要先解压，按包作者的说明运行包里的启动脚本或安装器，把加载器装好，再用「新建服务器 → 接管已有服务器」选中那个文件夹。加载器已装好但仍认不出时，也可以解压后用接管，类型和版本可以自己填。")
}

func unrecognizedPackError() error {
	return hinted("没认出这个压缩包，既没有整合包清单，也看不出是服务端", "无法识别这个压缩包。Modrinth 整合包要有 modrinth.index.json，CurseForge 整合包要有 manifest.json，放在最外层，或只套一层文件夹（例如「整合包名/manifest.json」）。如果这是作者给的现成服务端，压缩包里应能看到 mods、plugins、server.properties 或服务端 jar。也可以先解压，再用「新建服务器 → 接管已有服务器」选中那个文件夹。")
}

func ambiguousServerError(dirs []string) error {
	sort.Strings(dirs)
	shown := dirs
	if len(shown) > 4 {
		shown = append(append([]string{}, dirs[:4]...), "…")
	}
	return hinted("压缩包里有好几台服务端，一次只能导入一台", "压缩包里有好几个看起来像服务端的文件夹（%s），没法判断该导入哪一个。请只打包其中一台，或用「新建服务器 → 接管已有服务器」选中具体的文件夹。", strings.Join(shown, "、"))
}

// recognizeServer 没有整合包清单时，看是不是一台现成的服务端（可能也套了一层文件夹）。
func recognizeServer(zipPath string, files []*zip.File) (*Pack, error) {
	root, roots, ok := serverRoot(files)
	if !ok {
		if len(roots) > 1 {
			return nil, ambiguousServerError(roots)
		}
		return nil, unrecognizedPackError()
	}
	if err := auditServerPaths(files); err != nil {
		return nil, err
	}
	cands, unread := serverCands(files, root)
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
	return serverPack(zipPath, root, best), nil
}

func serverPack(zipPath, root string, c srvCand) *Pack {
	name := root
	if name == "" {
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

// serverRoot 服务端文件在最外层，还是只套了一层目录。
// 最外层自己像服务端就用最外层（旁边再套一个文件夹时不猜里面那个）。
func serverRoot(files []*zip.File) (root string, nested []string, ok bool) {
	if hasServerTrait(files, "") {
		return "", nil, true
	}
	seen := map[string]bool{}
	for _, f := range files {
		name := zipNameOf(f)
		if name == "" || isZipDir(f, name) {
			continue
		}
		dir, _, found := strings.Cut(name, "/")
		if !found || seen[dir] {
			continue
		}
		clean, err := safeRelPath(dir)
		if err != nil || clean != dir {
			continue
		}
		seen[dir] = true
		if hasServerTrait(files, dir) {
			nested = append(nested, dir)
		}
	}
	if len(nested) == 1 {
		return nested[0], nested, true
	}
	return "", nested, false
}

func hasServerTrait(files []*zip.File, root string) bool {
	for _, f := range files {
		name := zipNameOf(f)
		rel, ok := relUnder(name, root)
		if !ok {
			continue
		}
		if isServerTrait(rel) {
			return true
		}
	}
	return false
}

func isServerTrait(rel string) bool {
	low := strings.ToLower(rel)
	switch {
	case low == "server.properties":
		return true
	case low == "mods" || strings.HasPrefix(low, "mods/"):
		return true
	case low == "plugins" || strings.HasPrefix(low, "plugins/"):
		return true
	case strings.HasPrefix(low, "libraries/net/minecraftforge/forge/"),
		strings.HasPrefix(low, "libraries/net/neoforged/neoforge/"):
		return true
	case isRootJar(rel):
		return true
	default:
		return false
	}
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
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
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

// ExtractServer 把现成服务端压缩包解进实例目录。Root 那一层会剥掉。
// 每条路径都过 safeRelPath，过不了就整包拒绝、一个文件都不写。
// 符号链接不展开。writeFile 拿到的 rel 已经是剥掉 Root 之后的相对路径。
func ExtractServer(p *Pack, writeFile func(rel string, r io.Reader) error) (int, error) {
	if p == nil || p.Format != FormatServer {
		return 0, fmt.Errorf("这不是服务端压缩包")
	}
	zr, err := zip.OpenReader(p.ZipPath)
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

package worldimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"minedeck/internal/winname"
	"minedeck/internal/worldimport/nbt"
)

// 识别：把一份来源分成「能导入的 Java 世界（一个或几个候选）」和「别的东西（说清是什么、该去哪）」。
// 认世界看的是能解析的 level.dat 和真实目录结构，不看扩展名、不看文件夹叫不叫 world，也不要求根目录有 region。

const maxWorldDepth = 6 // level.dat 所在目录最深在来源根下第几层（套壳目录、「地图名/存档/世界」这类）

// 一次检测的工作量上限（2026-11-14 Codex 复审第 1 条）。单个文件早就有上限，合起来以前没有：一个做过手脚的包里放上
// 成千上万个世界，每个都带一份塞满的 level.dat，检测能把程序拖住很久。做成变量是为了测试能调小。
var (
	// maxWorlds 一份来源里最多认多少个世界。选 .minecraft/saves 这样放着很多存档的文件夹是正常用法，所以留得宽；
	// 超了整份拦下、说清楚，不悄悄截掉一部分。
	maxWorlds = 256
	// maxScanNBTBytes、maxScanNBTNodes 一次检测里所有 NBT（各个世界的 level.dat 和共用数据）展开以后合起来最多多少字节、
	// 多少个值。正常的 level.dat 展开后几十 KB，带大量模组的几 MB；二百多个存档合起来也远到不了这里。
	maxScanNBTBytes int64 = 1 << 30
	maxScanNBTNodes int64 = 16_000_000
)

// the five cross-dimension data files that 26.1 Vanilla keeps in data/minecraft/ and Paper keeps in the overworld folder
var sharedDataFiles = []string{"game_rules.dat", "scheduled_events.dat", "wandering_trader.dat", "weather.dat", "world_gen_settings.dat"}

// scanResult 识别的结果，带上暂存时要用的东西（不发给前端）。
type scanResult struct {
	Scan
	src   *source
	cands map[string]*candInfo
	// pins 有几个导入、建服任务正在用它（Service.mu 保护）。用着的时候过期、放弃都不删上传的文件，
	// 只记下 dropped，等最后一个任务用完再删。
	pins    int
	dropped bool

	// 下面几项只在检测的过程中用
	tooManyWorlds int                    // 世界的个数超过了 maxWorlds 时记下有多少个
	budget        *nbt.Budget            // 这次检测还能展开多少 NBT
	kids          map[string]*dirListing // children 的结果按目录记着：每个世界都要看上级目录和来源根，不重复数
}

type dirListing struct{ files, subdirs []string }

// children 同 source.children，按目录记着结果。
func (r *scanResult) children(dir string) (files, subdirs []string) {
	k := winname.Fold(dir)
	if l := r.kids[k]; l != nil {
		return l.files, l.subdirs
	}
	l := &dirListing{}
	l.files, l.subdirs = r.src.children(dir)
	if r.kids == nil {
		r.kids = map[string]*dirListing{}
	}
	r.kids[k] = l
	return l.files, l.subdirs
}

// candInfo 一个候选在来源里占了哪些文件。
type candInfo struct {
	c     *Candidate
	roots []string // 来源里属于这张地图的目录（主目录在前，旧 Bukkit 布局再加 _nether、_the_end）
	files []entry
	fp    string
}

// scanPath 识别 p（文件夹或文件）。返回的 scanResult 里 src 还开着，调用方负责 Close。
func scanPath(p, display string, uploaded bool) (*scanResult, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	r := &scanResult{cands: map[string]*candInfo{}}
	r.Source = SourceRef{Path: p, Name: display, Uploaded: uploaded}
	if st.IsDir() {
		r.Source.Kind = "folder"
		src, err := openFolder(p)
		if err != nil {
			return r.failed(err)
		}
		r.src = src
	} else {
		r.Source.Kind = "file"
		kind, err := sniff(p)
		if err != nil {
			return nil, err
		}
		ext := strings.ToLower(filepath.Ext(display))
		switch kind {
		case "zip":
			r.Source.Kind = "zip"
			src, err := openZip(p)
			if err != nil {
				return r.failed(err)
			}
			r.src = src
		case "zip-split":
			return r.only(KindArchive, SevBlock, "split", "这是分卷压缩包里的一卷。先用解压软件把全部分卷一起解开，再选解出来的地图文件夹", "")
		case "rar", "7z":
			return r.only(KindArchive, SevBlock, kind, "这是 "+strings.ToUpper(kind)+" 压缩包，MineDeck 只能直接读 ZIP。先用解压软件解开，再选解出来的地图文件夹", "")
		case "gzip", "nbt":
			return r.singleNBT(p, display, ext)
		default:
			if ext == ".mcworld" {
				return r.only(KindBedrock, SevBlock, "bedrock", bedrockMsg, "")
			}
			return r.only(KindUnknown, SevBlock, "unknown-file", "认不出这个文件。地图一般是一个文件夹，或者一个 .zip 压缩包", "")
		}
	}
	r.Files = len(r.src.files)
	for _, e := range r.src.files {
		r.Bytes += e.size
	}
	r.Fingerprint = fingerprint(r.src.files)
	r.classify(strings.ToLower(filepath.Ext(display)))
	return r, nil
}

const bedrockMsg = "这是基岩版（手机、主机、Win10/11 版）的世界。现在的导入只支持 Java 版，改扩展名转不过来；要在 Java 版服务器上用，需要先用专门的转换工具转成 Java 版世界"

func (r *scanResult) failed(err error) (*scanResult, error) {
	var se *sourceErr
	if errors.As(err, &se) {
		r.Kind = se.Kind
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: se.Code, Msg: se.Msg, Detail: se.Detail})
		return r, nil
	}
	return nil, err
}

func (r *scanResult) only(kind InputKind, sev Severity, code, msg, detail string) (*scanResult, error) {
	r.Kind = kind
	r.Issues = append(r.Issues, Issue{Sev: sev, Code: code, Msg: msg, Detail: detail})
	return r, nil
}

// singleNBT 用户选的是一个 NBT 文件：建筑蓝图（.schem / .schematic / .litematic / 结构 .nbt），或者单独一个 level.dat。
func (r *scanResult) singleNBT(p, display, ext string) (*scanResult, error) {
	if strings.EqualFold(filepath.Base(display), "level.dat") || strings.EqualFold(filepath.Base(display), "level.dat_old") {
		return r.only(KindDamagedWorld, SevBlock, "pick-folder", "这是世界里的 level.dat 一个文件。请选它所在的整个世界文件夹（里面还有 region 等文件夹）", "")
	}
	_, root, _, err := nbt.ReadFile(p, nbt.Limits{MaxFile: 64 << 20, MaxBytes: 64 << 20, MaxDepth: 64, MaxNodes: 2_000_000})
	if err == nil && isStructure(root, ext) || err != nil && structureExt(ext) {
		return r.only(KindStructure, SevBlock, "structure", structureMsg, "")
	}
	return r.only(KindUnknown, SevBlock, "unknown-nbt", "这是一个 Minecraft 数据文件，但不是完整的世界。地图一般是一个文件夹，或者一个 .zip 压缩包", "")
}

const structureMsg = "这是建筑蓝图（.schem / .schematic / .litematic / 结构文件），不是完整的世界。蓝图要在游戏里用 WorldEdit、Litematica 或结构方块粘贴进现有世界"

func structureExt(ext string) bool {
	switch ext {
	case ".schem", ".schematic", ".litematic", ".nbt":
		return true
	}
	return false
}

func isStructure(root nbt.Compound, ext string) bool {
	if structureExt(ext) {
		return true
	}
	_, a := root.Get("Regions")   // litematic
	_, b := root.Get("Materials") // MCEdit .schematic
	_, c := root.Get("palette")   // 结构方块的 .nbt
	_, d := root.Get("Palette")   // Sponge .schem v1/v2
	_, e := root.Get("Schematic") // Sponge v3
	return a || b || c || d || e
}

// classify 在文件清单上找世界；找不到就认别的东西。
func (r *scanResult) classify(ext string) {
	s := r.src
	if len(s.links) > 0 {
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "links",
			Msg:    "来源文件夹里有快捷链接（符号链接或目录联接）。为了安全不会跟进去读，请把真实文件复制到一个普通文件夹再导入",
			Detail: strings.Join(firstN(s.links, 5), "、")})
	}
	if len(s.bad) > 0 {
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "bad-names",
			Msg:    "来源里有文件名在 Windows 上不能原样使用（设备名、结尾是点或空格等），请先改名",
			Detail: strings.Join(firstN(s.bad, 5), "、")})
	}
	if ext == ".mcworld" || r.looksBedrock() {
		r.Kind = KindBedrock
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "bedrock", Msg: bedrockMsg})
		return
	}
	if r.looksMdpack() {
		r.Kind = KindMdpack
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "mdpack",
			Msg: "这是 MineDeck 的交接包（整台服务器）。请到「新建服务器」里选「导入交接包」，它会连地图带配置一起导入"})
		return
	}
	r.budget = &nbt.Budget{Bytes: maxScanNBTBytes, Nodes: maxScanNBTNodes}
	r.findWorlds()
	if r.tooManyWorlds > 0 {
		r.Kind = KindUnknown
		r.Candidates, r.cands = nil, map[string]*candInfo{}
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "too-many-worlds",
			Msg: fmt.Sprintf("这里面的世界太多了（%d 个，最多 %d 个），一次认不过来。请直接选其中一个世界的文件夹（里面有 level.dat 的那一层），或者只把要用的那张地图打成压缩包",
				r.tooManyWorlds, maxWorlds)})
		return
	}
	if r.budget.Spent() {
		r.Kind = KindUnknown
		r.Candidates, r.cands = nil, map[string]*candInfo{}
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "too-much-data",
			Msg: "这里面各个世界的 level.dat 合起来大得不正常，没有认完。请直接选其中一个世界的文件夹（里面有 level.dat 的那一层）"})
		return
	}
	if len(r.Candidates) > 0 {
		r.Kind = KindJavaWorld
		good := 0
		for _, c := range r.Candidates {
			if !hasBlock(c.Issues) {
				good++
			}
		}
		if good == 0 {
			r.Kind = KindDamagedWorld
		}
		if len(r.Candidates) > 1 {
			r.Issues = append(r.Issues, Issue{Sev: SevInfo, Code: "multi",
				Msg: fmt.Sprintf("这个包里找到 %d 张地图，请选要使用的一张", len(r.Candidates))})
		}
		if r.hasServerFiles() {
			r.Issues = append(r.Issues, Issue{Sev: SevInfo, Code: "server-folder",
				Msg: "这看起来是一整台服务器。这里只会导入你选的那张地图；要连核心、模组、插件和配置一起搬过来，请用「新建服务器」里的「接管已有服务器」或「导入现成服务端」"})
		}
		return
	}
	// 没有世界：认别的东西
	switch {
	case r.damagedOnly():
		r.Kind = KindDamagedWorld
	case r.looksModpack():
		r.Kind = KindModpack
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "modpack",
			Msg: "这是整合包（模组清单），里面没有世界。请到「新建服务器」里选「导入整合包」"})
	case r.looksPack("assets"):
		r.Kind = KindResourcePack
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "resource-pack",
			Msg: "这是资源包（材质、声音），不是世界。资源包要在客户端安装，或者在服务器设置里填一个大家都能下载的资源包地址"})
	case r.looksPack("data"):
		r.Kind = KindDatapack
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "datapack",
			Msg: "这是数据包，不是世界。数据包要放进世界文件夹的 datapacks 里，再在游戏里 /reload"})
	case r.looksStructures():
		r.Kind = KindStructure
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "structure", Msg: structureMsg})
	case r.hasServerFiles():
		r.Kind = KindServer
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "server-no-world",
			Msg: "这是服务端文件，里面没有找到世界。整台服务器请用「新建服务器」里的「接管已有服务器」或「导入现成服务端」"})
	default:
		r.Kind = KindUnknown
		r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "no-world",
			Msg: "没有在里面找到 Java 版世界（世界文件夹里应该有 level.dat）。如果你只有种子，请在新建服务器时填写种子；种子不包含已有的建筑和玩家进度"})
	}
}

// findWorlds 找 level.dat 所在的目录，按旧 Bukkit 布局分组，读元数据，定布局。
func (r *scanResult) findWorlds() {
	s := r.src
	var dirs []string
	have := map[string]bool{}
	oldOnly := map[string]string{} // 只有 level.dat_old 的目录：fold 后的名字 → 原来的写法
	for _, e := range s.files {
		base := path.Base(e.fold)
		if base != "LEVEL.DAT" && base != "LEVEL.DAT_OLD" {
			continue
		}
		d := path.Dir(e.rel)
		if d == "." {
			d = ""
		}
		if d != "" && strings.Count(d, "/")+1 > maxWorldDepth {
			continue
		}
		// MineDeck 自己的控制目录（换地图的暂存区、恢复点）：选一个 MineDeck 服务器文件夹当来源时，不把它们当成地图
		if strings.HasPrefix(e.fold, ".MINEDECK/") || strings.Contains(e.fold, "/.MINEDECK/") {
			continue
		}
		k := winname.Fold(d)
		if base == "LEVEL.DAT_OLD" {
			if !have[k] {
				oldOnly[k] = d
			}
			continue
		}
		delete(oldOnly, k)
		if !have[k] {
			have[k] = true
			dirs = append(dirs, d)
		}
	}
	for _, e := range s.files { // level.dat_old 在 level.dat 之前排序的情况
		if path.Base(e.fold) == "LEVEL.DAT" {
			delete(oldOnly, winname.Fold(dirOf(e.rel)))
		}
	}
	// 世界太多就到此为止，不往下认：每个世界都要读 level.dat、数文件、找说明书，一个做过手脚的包里放上十万个
	// 「目录/level.dat」，检测能跑上很久（2026-11-14 Codex 复审第 1 条）。不悄悄截掉一部分：整份拦下，让人选小一点的范围
	if n := len(dirs) + len(oldOnly); n > maxWorlds {
		r.tooManyWorlds = n
		return
	}
	// 嵌在别的世界里面的 level.dat 算那个世界的一部分（比如老的维度目录里带的）。
	// 短的在前，逐个看它的上级目录里有没有已经算作世界的：层数有上限（maxWorldDepth），不用两两比
	sort.Slice(dirs, func(a, b int) bool { return len(dirs[a]) < len(dirs[b]) })
	var tops []string
	isTop := map[string]bool{}
	for _, d := range dirs {
		k := winname.Fold(d)
		inside := isTop[""] // 来源的根本身就是世界：别的都在它里面
		for up := k; !inside && up != ""; {
			up = dirOf(up)
			inside = up != "" && isTop[up]
		}
		if !inside {
			isTop[k] = true
			tops = append(tops, d)
		}
	}
	// 旧 Bukkit：<名>_nether、<名>_the_end 并进 <名>
	member := map[string]string{} // fold(成员目录) → 主目录
	for _, d := range tops {
		for _, suf := range []string{"_nether", "_the_end"} {
			sib := d + suf
			if d == "" {
				continue
			}
			if s.isDir(sib) {
				member[winname.Fold(sib)] = d
			}
		}
	}
	sort.Slice(tops, func(a, b int) bool { return tops[a] < tops[b] })
	n := 0
	for _, d := range tops {
		if _, isMember := member[winname.Fold(d)]; isMember {
			continue
		}
		n++
		r.addCandidate(fmt.Sprintf("c%d", n), d)
	}
	oldKeys := make([]string, 0, len(oldOnly))
	for k := range oldOnly {
		oldKeys = append(oldKeys, k)
	}
	sort.Strings(oldKeys) // 编号每次检测都一样
	for _, k := range oldKeys {
		d := oldOnly[k]
		n++
		c := &Candidate{ID: fmt.Sprintf("c%d", n), Root: d, FolderName: folderName(d, r.Source.Name), Layout: LayoutLegacy}
		c.Issues = append(c.Issues, Issue{Sev: SevBlock, Code: "only-level-dat-old",
			Msg: "这个世界只有备份用的 level.dat_old，没有 level.dat。可以先把 level.dat_old 复制一份改名成 level.dat，确认能在单人游戏里打开，再来导入"})
		r.Candidates = append(r.Candidates, *c)
		r.cands[c.ID] = &candInfo{c: &r.Candidates[len(r.Candidates)-1], roots: []string{d}}
	}
	// 每个 candInfo.c 指向切片里的元素：切片扩容会搬走，最后统一指一遍
	for k := range r.Candidates {
		r.cands[r.Candidates[k].ID].c = &r.Candidates[k]
	}
}

func dirOf(rel string) string {
	d := path.Dir(rel)
	if d == "." {
		return ""
	}
	return d
}

func folderName(rel, fallback string) string {
	if rel == "" {
		n := strings.TrimSuffix(fallback, filepath.Ext(fallback))
		if n == "" {
			n = "world"
		}
		return n
	}
	return path.Base(rel)
}

// addCandidate 读一个世界目录。
func (r *scanResult) addCandidate(id, d string) {
	s := r.src
	c := Candidate{ID: id, Root: d, FolderName: folderName(d, r.Source.Name)}
	info := &candInfo{roots: []string{d}}
	if d != "" {
		for _, suf := range []string{"_nether", "_the_end"} {
			if s.isDir(d + suf) {
				c.Group = append(c.Group, d+suf)
				info.roots = append(info.roots, d+suf)
			}
		}
	}
	if d == "" {
		info.files = s.files
	} else {
		for _, root := range info.roots {
			info.files = append(info.files, s.under(root)...)
		}
	}
	for _, e := range info.files {
		c.Files++
		c.Bytes += e.size
	}
	info.fp = fingerprint(info.files)

	// level.dat
	lvl := s.find(join(d, "level.dat"))
	meta, perr := r.readLevel(lvl)
	if perr != nil {
		c.Issues = append(c.Issues, Issue{Sev: SevBlock, Code: "level-dat-broken",
			Msg:    "这个世界的 level.dat 读不出来，可能已经损坏。如果旁边有 level.dat_old，可以先复制一份改名成 level.dat，在单人游戏里确认能打开再导入",
			Detail: perr.Error()})
	}
	c.Meta = meta
	c.Layout = r.layoutOf(&c)
	if perr == nil && (c.Layout == LayoutModern || c.Layout == LayoutModernPaper) {
		dataDir := join(d, "data/minecraft")
		if c.Layout == LayoutModernPaper {
			dataDir = join(d, "dimensions/minecraft/overworld/data/minecraft")
		}
		c.Meta.applySharedData(r.readNBT(dataDir+"/game_rules.dat"), r.readNBT(dataDir+"/world_gen_settings.dat"))
	}
	c.Dimensions = r.dimensionsOf(&c)
	r.playersOf(&c)
	r.extrasOf(&c)
	r.checkCandidate(&c)
	r.Candidates = append(r.Candidates, c)
	r.cands[id] = info
}

// nbtLimits 读一个世界的 NBT 用的上限：单个文件的上限照旧，另外从这次检测共用的额度里扣。
func (r *scanResult) nbtLimits() nbt.Limits {
	lim := nbt.LevelLimits
	lim.Budget = r.budget
	return lim
}

func (r *scanResult) readLevel(e *entry) (LevelMeta, error) {
	if e == nil {
		return LevelMeta{}, fmt.Errorf("没有 level.dat")
	}
	if e.size > nbt.LevelLimits.MaxFile {
		return LevelMeta{}, fmt.Errorf("level.dat 有 %d 字节，大得不正常", e.size)
	}
	rc, err := r.src.open(e)
	if err != nil {
		return LevelMeta{}, err
	}
	defer rc.Close()
	_, root, _, err := nbt.Read(rc, r.nbtLimits())
	if err != nil {
		if errors.Is(err, nbt.ErrNotNBT) && r.bedrockHeader(e) {
			return LevelMeta{}, fmt.Errorf("这是基岩版的 level.dat")
		}
		return LevelMeta{}, err
	}
	return parseLevel(root)
}

// readNBT 读来源里的一个小 NBT 文件（读不了返回 nil）。
func (r *scanResult) readNBT(rel string) nbt.Compound {
	e := r.src.find(rel)
	if e == nil || e.size > nbt.LevelLimits.MaxFile {
		return nil
	}
	rc, err := r.src.open(e)
	if err != nil {
		return nil
	}
	defer rc.Close()
	_, root, _, err := nbt.Read(rc, r.nbtLimits())
	if err != nil {
		return nil
	}
	return root
}

func join(d, name string) string {
	if d == "" {
		return name
	}
	return d + "/" + name
}

// layoutOf 按真实目录结构定布局。
func (r *scanResult) layoutOf(c *Candidate) Layout {
	s, d := r.src, c.Root
	if s.isDir(join(d, "dimensions/minecraft/overworld")) || s.isDir(join(d, "players")) && !s.isDir(join(d, "playerdata")) && !s.isDir(join(d, "region")) {
		// Paper 26.x：data/paper 和 paper-world.yml 在每个维度的目录里（实服 26.3 核对过），没有 uid.dat
		if s.isDir(join(d, "data/paper")) || s.isDir(join(d, "dimensions/minecraft/overworld/data/paper")) ||
			r.anyFile(join(d, "dimensions/minecraft/overworld/data/minecraft"), sharedDataFiles) ||
			s.find(join(d, "dimensions/minecraft/overworld/paper-world.yml")) != nil {
			return LayoutModernPaper
		}
		return LayoutModern
	}
	if len(c.Group) > 0 {
		return LayoutLegacyBukkit
	}
	return LayoutLegacy
}

func (r *scanResult) anyFile(dir string, names []string) bool {
	for _, n := range names {
		if r.src.find(dir+"/"+n) != nil {
			return true
		}
	}
	return false
}

// regionCount dir/region 下的 .mca 个数（不往下走）。
func (r *scanResult) regionCount(dir string) int {
	n := 0
	rd := join(dir, "region")
	for _, e := range r.src.under(rd) {
		rest := relRest(e.fold, rd)
		if !strings.Contains(rest, "/") && strings.HasSuffix(rest, ".MCA") {
			n++
		}
	}
	return n
}

// dimensionsOf 列出维度。
func (r *scanResult) dimensionsOf(c *Candidate) []Dimension {
	s, d := r.src, c.Root
	var out []Dimension
	add := func(id, rel string) {
		out = append(out, Dimension{ID: id, Rel: rel, Regions: r.regionCount(rel)})
	}
	custom := func(base string, skipMinecraft bool) {
		// dimensions/<命名空间>/<路径…>：含 region、entities、poi 或 data 的目录算一个维度
		seen := map[string]bool{}
		for _, e := range s.under(base) {
			rest := relRest(e.rel, base)
			segs := strings.Split(rest, "/")
			for k := 2; k < len(segs); k++ {
				switch strings.ToLower(segs[k]) {
				case "region", "entities", "poi", "data":
					ns, p := segs[0], strings.Join(segs[1:k], "/")
					id := strings.ToLower(ns + ":" + p)
					if skipMinecraft && strings.EqualFold(ns, "minecraft") && (strings.EqualFold(p, "overworld") || strings.EqualFold(p, "the_nether") || strings.EqualFold(p, "the_end")) {
						k = len(segs)
						continue
					}
					if !seen[id] && len(seen) < 64 {
						seen[id] = true
						add(id, base+"/"+ns+"/"+p)
					}
					k = len(segs)
				}
			}
		}
	}
	switch c.Layout {
	case LayoutModern, LayoutModernPaper:
		for _, v := range []string{"overworld", "the_nether", "the_end"} {
			rel := join(d, "dimensions/minecraft/"+v)
			if s.isDir(rel) {
				add("minecraft:"+v, rel)
			}
		}
		custom(join(d, "dimensions"), true)
	case LayoutLegacyBukkit:
		add("minecraft:overworld", d)
		for _, g := range c.Group {
			if strings.HasSuffix(strings.ToLower(g), "_nether") && s.isDir(g+"/DIM-1") {
				add("minecraft:the_nether", g+"/DIM-1")
			}
			if strings.HasSuffix(strings.ToLower(g), "_the_end") && s.isDir(g+"/DIM1") {
				add("minecraft:the_end", g+"/DIM1")
			}
		}
		custom(join(d, "dimensions"), false)
	default:
		add("minecraft:overworld", d)
		if s.isDir(join(d, "DIM-1")) {
			add("minecraft:the_nether", join(d, "DIM-1"))
		}
		if s.isDir(join(d, "DIM1")) {
			add("minecraft:the_end", join(d, "DIM1"))
		}
		custom(join(d, "dimensions"), false)
	}
	return out
}

// playersOf 玩家文件数。
func (r *scanResult) playersOf(c *Candidate) {
	dir := join(c.Root, "playerdata")
	if c.Layout == LayoutModern || c.Layout == LayoutModernPaper {
		dir = join(c.Root, "players/data")
	}
	for _, e := range r.src.under(dir) {
		rest := relRest(e.fold, dir)
		if !strings.Contains(rest, "/") && strings.HasSuffix(rest, ".DAT") {
			c.PlayerFiles++
			if len(c.players) < 10000 {
				c.players = append(c.players, strings.ToLower(strings.TrimSuffix(rest, ".DAT")))
			}
		}
	}
}

// extrasOf 数据包、资源包、图标、说明书，以及和地图并列、不会导入的东西。
func (r *scanResult) extrasOf(c *Candidate) {
	s, d := r.src, c.Root
	_, packDirs := s.children(join(d, "datapacks"))
	packFiles, _ := s.children(join(d, "datapacks"))
	for _, n := range append(packFiles, packDirs...) {
		if len(c.Datapacks) < 64 {
			c.Datapacks = append(c.Datapacks, cleanText(n, 80))
		}
	}
	sort.Strings(c.Datapacks)
	c.ResourcePak = s.find(join(d, "resources.zip")) != nil
	c.HasIcon = s.find(join(d, "icon.png")) != nil
	// 说明书：世界所在目录和来源根上的文本文件
	look := []string{dirOf(d)}
	if dirOf(d) != "" {
		look = append(look, "")
	}
	seen := map[string]bool{}
	for _, dir := range look {
		files, subdirs := r.children(dir) // 上级目录和来源根：每个世界都要看，结果记着
		for _, f := range files {
			low := strings.ToLower(f)
			isText := strings.HasSuffix(low, ".txt") || strings.HasSuffix(low, ".md")
			if isText && (strings.Contains(low, "readme") || strings.Contains(low, "read me") || strings.Contains(f, "说明") ||
				strings.Contains(f, "必读") || strings.Contains(low, "info") || strings.Contains(low, "instruction") || len(files) <= 6) {
				rel := join(dir, f)
				if e := s.find(rel); e != nil && e.size <= 256<<10 && !seen[e.fold] && len(c.Readme) < 5 {
					seen[e.fold] = true
					c.Readme = append(c.Readme, rel)
				}
			}
			if strings.HasSuffix(low, ".jar") || low == "start.bat" || low == "run.bat" || low == "start.sh" || low == "run.sh" ||
				low == "server.properties" || low == "eula.txt" || strings.HasSuffix(low, ".zip") && low != "resources.zip" {
				c.SourceHints = appendUniq(c.SourceHints, join(dir, f))
			}
		}
		for _, sd := range subdirs {
			switch strings.ToLower(sd) {
			case "mods", "plugins", "config", "resourcepacks", "shaderpacks", "libraries", "defaultconfigs", "kubejs", "scripts":
				c.SourceHints = appendUniq(c.SourceHints, join(dir, sd)+"/")
			}
		}
	}
	if len(c.SourceHints) > 12 {
		c.SourceHints = c.SourceHints[:12]
	}
}

func appendUniq(s []string, v string) []string {
	for _, x := range s {
		if strings.EqualFold(x, v) {
			return s
		}
	}
	return append(s, v)
}

// checkCandidate 只看地图自己就能下的结论（和目标服务器无关的）。
func (r *scanResult) checkCandidate(c *Candidate) {
	s, d := r.src, c.Root
	m := c.Meta
	if c.Layout == LayoutModern || c.Layout == LayoutModernPaper {
		if r.regionCount(d) > 0 {
			c.Issues = append(c.Issues, Issue{Sev: SevBlock, Code: "layout-conflict",
				Msg: "这个世界的目录结构自相矛盾：既有 26.1 以前的主世界区块，又有 26.1 起的 dimensions 文件夹。可能是两个版本的文件混在了一起，请在单人游戏里确认能打开以后重新打包"})
		}
		if c.Layout == LayoutModernPaper {
			for _, f := range sharedDataFiles {
				if s.find(join(d, "data/minecraft/"+f)) != nil && s.find(join(d, "dimensions/minecraft/overworld/data/minecraft/"+f)) != nil {
					c.Issues = append(c.Issues, Issue{Sev: SevBlock, Code: "shared-data-twice",
						Msg:    "这个世界里同一份共用数据（天气、游戏规则等）出现在两个位置，不知道该用哪一份，没法自动整理",
						Detail: f})
					break
				}
			}
		}
	}
	if c.Layout == LayoutLegacyBukkit && (s.isDir(join(d, "DIM-1")) || s.isDir(join(d, "DIM1"))) {
		c.Issues = append(c.Issues, Issue{Sev: SevBlock, Code: "bukkit-and-vanilla-dims",
			Msg:    "这个世界的下界或末地同时出现在两个位置（原版的 DIM-1 / DIM1 和 Bukkit 的 _nether / _the_end），不知道哪一份是最新的。请先确认要哪一份，把另一份移走再导入",
			Detail: strings.Join(c.Group, "、")})
	}
	if strings.HasSuffix(strings.ToLower(c.FolderName), "_nether") || strings.HasSuffix(strings.ToLower(c.FolderName), "_the_end") {
		c.Issues = append(c.Issues, Issue{Sev: SevWarn, Code: "partial-bukkit",
			Msg: "这看起来只是一张地图的下界或末地部分（旧 Bukkit / Paper 服务器把它们分开放）。主世界的文件夹不在这里"})
	}
	if m.HasVersion && m.Series != "" && m.Series != "main" {
		c.Issues = append(c.Issues, Issue{Sev: SevConfirm, Code: "special-series",
			Msg: "这张地图是用特殊分支的游戏版本（" + m.Series + "）存的，不是正式版本线，兼容性无法确认"})
	}
	if m.Snapshot {
		c.Issues = append(c.Issues, Issue{Sev: SevConfirm, Code: "snapshot",
			Msg: "这张地图最后是用快照（测试版）" + m.VersionName + " 存的，快照的存档格式随时会变，兼容性无法确认"})
	}
	if s.kind == "folder" {
		for _, root := range r.cands0(c) {
			if inUse, err := sessionLockHeld(filepath.Join(s.root, filepath.FromSlash(root))); err == nil && inUse {
				c.Issues = append(c.Issues, Issue{Sev: SevBlock, Code: "source-in-use",
					Msg: "这张地图正在被 Minecraft 或另一台服务器使用。先退出那个世界（或停掉那台服务器），再来导入"})
				break
			}
		}
	}
}

func (r *scanResult) cands0(c *Candidate) []string { return append([]string{c.Root}, c.Group...) }

// ---------- 别的东西 ----------

func (r *scanResult) looksBedrock() bool {
	for _, e := range r.src.files {
		if path.Base(e.fold) == "LEVELNAME.TXT" && strings.Count(e.fold, "/") <= 3 {
			if r.src.isDir(join(dirOf(e.rel), "db")) {
				return true
			}
		}
	}
	return false
}

// bedrockHeader level.dat 开头是不是基岩版的 8 字节小端头（存储版本 + 长度），后面紧跟复合标签。
func (r *scanResult) bedrockHeader(e *entry) bool {
	// 只读开头 9 个字节，文件一共多大看清单里记的：不必为了数长度把整个文件读进来
	if e.size < 9 || e.size > 16<<20 {
		return false
	}
	rc, err := r.src.open(e)
	if err != nil {
		return false
	}
	defer rc.Close()
	var b [9]byte
	if _, err := io.ReadFull(rc, b[:]); err != nil {
		return false
	}
	n := int64(b[4]) | int64(b[5])<<8 | int64(b[6])<<16 | int64(b[7])<<24
	return n == e.size-8 && b[8] == 0x0a
}

func (r *scanResult) looksMdpack() bool {
	e := r.src.find("manifest.json")
	if e == nil || !r.src.isDir("instance") {
		return false
	}
	b, err := r.src.readSmall(e, 4<<20)
	if err != nil {
		return false
	}
	var m struct {
		FormatVersion int             `json:"formatVersion"`
		Instance      json.RawMessage `json:"instance"`
	}
	return json.Unmarshal(b, &m) == nil && m.FormatVersion > 0 && len(m.Instance) > 0
}

func (r *scanResult) looksModpack() bool {
	for _, e := range r.src.files {
		if strings.Count(e.fold, "/") > 1 {
			continue
		}
		switch path.Base(e.fold) {
		case "MODRINTH.INDEX.JSON", "MMC-PACK.JSON", "INSTANCE.CFG":
			return true
		case "MANIFEST.JSON":
			if b, err := r.src.readSmall(&e, 8<<20); err == nil && bytes.Contains(b, []byte(`"minecraft"`)) && bytes.Contains(b, []byte(`"manifestType"`)) {
				return true
			}
		}
	}
	return false
}

// looksPack pack.mcmeta 旁边有 assets（资源包）或 data（数据包）。可以套一层目录。
func (r *scanResult) looksPack(sub string) bool {
	for _, e := range r.src.files {
		if path.Base(e.fold) == "PACK.MCMETA" && strings.Count(e.fold, "/") <= 1 && r.src.isDir(join(dirOf(e.rel), sub)) {
			return true
		}
	}
	return false
}

func (r *scanResult) looksStructures() bool {
	n := 0
	for _, e := range r.src.files {
		if structureExt(strings.ToLower(path.Ext(e.rel))) {
			n++
		}
	}
	return n > 0 && n*2 >= len(r.src.files)
}

func (r *scanResult) hasServerFiles() bool {
	for _, e := range r.src.files {
		if strings.Count(e.fold, "/") > 1 {
			continue
		}
		switch b := path.Base(e.fold); {
		case b == "SERVER.PROPERTIES", b == "EULA.TXT", b == "BUKKIT.YML", b == "SPIGOT.YML":
			return true
		}
	}
	return false
}

// damagedOnly 没有 level.dat，但有区块文件：像世界、缺了最关键的元数据。
func (r *scanResult) damagedOnly() bool {
	for _, e := range r.src.files {
		if strings.HasSuffix(e.fold, ".MCA") && strings.Contains(e.fold, "REGION/") {
			r.Issues = append(r.Issues, Issue{Sev: SevBlock, Code: "only-regions",
				Msg: "这里只有区块文件（.mca），没有 level.dat。只有区块是开不了服的：请找到完整的世界文件夹（里面有 level.dat）再导入"})
			return true
		}
	}
	return false
}

func hasBlock(is []Issue) bool {
	for _, i := range is {
		if i.Sev == SevBlock {
			return true
		}
	}
	return false
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

package inst

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"minedeck/internal/mcsrc"
)

// 接管已有服务器的第一步：只读地看一遍用户选的文件夹，认出核心、版本、启动文件、原来分的内存。
// 认出来的东西只是给界面预填，用户都能改；这里不联网、不写任何文件。
//
// 各种服务端在文件里留下的记号（Forge / NeoForge、原版、日志三样拿真实的服务器核对过；
// Paper 系和 Fabric 按它们启动器的源码写的，见 docs/交接说明.md「接管已有服务器」）：
//   - Forge / NeoForge（1.17 起）：libraries/net/{minecraftforge/forge,neoforged/neoforge}/<版本>/win_args.txt，
//     里面有 --fml.mcVersion、--fml.forgeVersion / --fml.neoForgeVersion
//   - 原版（1.18 起）：jar 的 Main-Class 是 net.minecraft.bundler.Main，根上有 version.json（id、java_version）
//   - Paper 系：Main-Class 带 paperclip（Leaves 是 leavesclip），META-INF/versions.list 一行「哈希 版本 路径」
//   - Fabric：启动器 jar 的 Main-Class 在 net.fabricmc 下，install.properties 里有 game-version、fabric-loader-version
//   - 老 Forge（1.16.5 及以前）：根目录的 forge-<mc>-<forge>.jar
//   - 都认不出时看 logs/latest.log 里的「Starting minecraft server version X」

// AdoptTarget 一个能启动的东西：根目录的某个 jar，或 Forge / NeoForge 的参数文件。
type AdoptTarget struct {
	Target    string `json:"target"`    // "jar:<文件>" 或 "args:<win_args.txt 相对路径>"，同 Settings.LaunchTarget
	Label     string `json:"label"`     // 给人看的说明
	Core      string `json:"core"`      // 认出的核心 id；认不出为空
	MC        string `json:"mc"`        // 认出的 Minecraft 版本（代理端是它自己的版本）
	Build     string `json:"build"`     // 加载器 / 构建版本
	JavaMajor int    `json:"javaMajor"` // 服务端自己写明要的 Java；没写为 0
}

// AdoptScan 识别结果。
type AdoptScan struct {
	Dir          string        `json:"dir"`
	Name         string        `json:"name"` // 建议的名称
	Core         string        `json:"core"`
	MC           string        `json:"mc"`
	Build        string        `json:"build"`
	LaunchTarget string        `json:"launchTarget"` // 选中的启动方式（Targets 里的第一个）
	Targets      []AdoptTarget `json:"targets"`      // 全部候选，最可能的在前
	JavaMajor    int           `json:"javaMajor"`
	XmxMB        int           `json:"xmxMb"`    // 原来的启动脚本里分的最大内存；没找到为 0
	ExtraJVM     []string      `json:"extraJvm"` // 会带过来的 JVM 参数
	Port         int           `json:"port"`
	EULA         bool          `json:"eula"`     // eula.txt 里已经同意过
	HasWorld     bool          `json:"hasWorld"` // 有存档
	Managed      bool          `json:"managed"`  // 目录里有 MineDeck 的 instance.json（或移出列表时留的 .bak）
	Notes        []string      `json:"notes"`    // 要让用户知道的事

	prev *Settings // Managed 时原来的设置
	key  string    // validateAdopt 登记的目录占位键（见 dirKey），任务结束时释放
}

// adoptCand 候选启动方式加上排序用的分数。
type adoptCand struct {
	AdoptTarget
	rank int
	note string
	shim bool // 新版 Forge 放在根目录的 forge-…-shim.jar：同目录有 win_args.txt 时不单列，算作同一种启动方式
}

const (
	rankArgs    = 50 // Forge / NeoForge 的参数文件：有它就不是直接跑 jar
	rankHybrid  = 45 // Mohist / Banner
	rankLoader  = 40 // Fabric 启动器、老 Forge 的 jar（旁边通常还放着原版 server.jar）
	rankServer  = 30 // Paper 系、代理端
	rankUnknown = 20 // 认不出的 jar：旁边还有原版 jar 时，多半它才是真正的服务端（Arclight 这类混合服），原版 jar 是它的依赖
	rankVanilla = 10
	rankScript  = 100 // 原来的启动脚本里点名用的
	rankPrev    = 200 // instance.json 里记着的

	dirLimit = 5000 // 一个文件夹最多看这么多项：选错成下载目录这种有几十万个文件的地方时不至于卡住
)

var (
	// -jar 后面的文件名：带引号的（名字里能有空格）或不带的，前面可以有 %~dp0、.\；只认根目录里的文件
	reScriptJar = regexp.MustCompile(`(?i)-jar\s+(?:"(?:%~dp0\\?)?(?:\.[\\/])?([^"\\/%]+\.jar)"|(?:%~dp0\\?)?(?:\.[\\/])?([^"\s\\/%]+\.jar))`)
	// @libraries/…/win_args.txt（.sh 里是 unix_args.txt）
	reScriptArgs  = regexp.MustCompile(`(?i)@"?(?:%~dp0\\?)?(?:\.[\\/])?(libraries[\\/][^"\s]+?)[\\/](?:win|unix)_args\.txt`)
	reXmx         = regexp.MustCompile(`(?i)-Xmx(\d{1,9})([kmg])`)
	reOtherFlags  = regexp.MustCompile(`(?:^|[\s"])-(?:XX:|D[A-Za-z]|javaagent:|-add-)`)
	reLogMC       = regexp.MustCompile(`Starting minecraft server version (\S+)`)
	reLogFmlMC    = regexp.MustCompile(`--fml\.mcVersion,?\s+([^\s,\]]+)`)
	reLegacyForge = regexp.MustCompile(`(?i)^forge-(\d+(?:\.\d+)+)-(\d+(?:\.\d+)+)`)
	reNameVersion = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)
	reEULA        = regexp.MustCompile(`(?im)^\s*eula\s*=\s*true\s*$`)
)

// scanServerDir 识别 dir（已经是清理过的绝对路径）。里面找不到能启动的东西时报错。
func scanServerDir(dir string) (*AdoptScan, error) {
	sc := &AdoptScan{Dir: dir, Targets: []AdoptTarget{}, ExtraJVM: []string{}, Notes: []string{}}

	if s, err := readSettings(dir); err == nil {
		sc.prev, sc.Managed = &s, true
	} else if s, err := readSettingsFile(filepath.Join(dir, settingsBackup)); err == nil {
		sc.prev, sc.Managed = &s, true
	}

	cands := scanArgsTargets(dir)
	jars := scanRootJars(dir)
	hints := scanScripts(dir)

	// 新版 Forge 的 shim.jar 和 win_args.txt 是同一个服务端的两种启动写法：有参数文件就只留参数文件
	// （MineDeck 新建的 Forge 也是这么启动的），脚本点名 shim.jar 算点名了参数文件
	forgeArgs := ""
	for _, c := range cands {
		if c.Core == string(mcsrc.CoreForge) && forgeArgs == "" {
			forgeArgs = strings.ToLower(c.Target)
		}
	}
	vanillaMC, vanillaJava := "", 0
	for _, c := range jars {
		if c.Core == string(mcsrc.CoreVanilla) && c.MC != "" && vanillaMC == "" {
			vanillaMC, vanillaJava = c.MC, c.JavaMajor
		}
		if c.shim && forgeArgs != "" {
			if key := strings.ToLower(c.Target); hints.targets[key] {
				hints.targets[forgeArgs] = true
				if x := hints.xmxByTarget[key]; x > 0 && hints.xmxByTarget[forgeArgs] == 0 {
					hints.xmxByTarget[forgeArgs] = x
				}
			}
			continue
		}
		cands = append(cands, c)
	}
	if len(cands) == 0 {
		return nil, noServerErr(dir)
	}

	// 版本的兜底：最近一次运行的日志（最能说明这台服务器现在是什么版本），同目录的原版 jar，再看目录结构
	fallbackMC := firstNonEmpty(mcFromLogs(dir), vanillaMC, mcFromLayout(dir))
	for i := range cands {
		c := &cands[i]
		if mcsrc.KindOf(mcsrc.Core(c.Core)) != mcsrc.KindProxy {
			if c.MC == "" {
				c.MC = fallbackMC
			}
			if c.JavaMajor == 0 && c.MC != "" && c.MC == vanillaMC {
				c.JavaMajor = vanillaJava
			}
		}
		if hints.targets[strings.ToLower(c.Target)] {
			c.rank += rankScript
		}
		if sc.prev != nil && strings.EqualFold(c.Target, sc.prev.LaunchTarget) {
			c.rank += rankPrev
		}
	}
	// 分数高的在前；分数一样（比如升级后新旧两个 jar 都留着）时版本新的在前
	sort.SliceStable(cands, func(a, b int) bool {
		if cands[a].rank != cands[b].rank {
			return cands[a].rank > cands[b].rank
		}
		if x, y := cands[a].MC, cands[b].MC; x != "" && y != "" && x != y {
			return versionLess(y, x)
		}
		return false
	})
	top := cands[0]
	// MineDeck 管过的目录、启动文件也还是原来那个：核心和版本信 instance.json 里记的
	trustPrev := false
	if p := sc.prev; p != nil && strings.EqualFold(top.Target, p.LaunchTarget) && mcsrc.ValidCore(p.Core) && p.MC != "" {
		trustPrev = true
		cands[0].Core, cands[0].MC, cands[0].Build = string(p.Core), p.MC, p.Build
		top = cands[0]
	}
	for i := range cands {
		cands[i].Label = targetLabel(&cands[i])
		sc.Targets = append(sc.Targets, cands[i].AdoptTarget)
	}
	sc.LaunchTarget, sc.Core, sc.MC, sc.Build, sc.JavaMajor = top.Target, top.Core, top.MC, top.Build, top.JavaMajor

	// 内存和 JVM 参数
	isArgs := strings.HasPrefix(sc.LaunchTarget, "args:")
	managedMem := sc.prev != nil && sc.prev.XmxMB >= 512
	if managedMem {
		sc.XmxMB = sc.prev.XmxMB
	} else {
		sc.XmxMB = hints.xmxFor(sc.LaunchTarget)
	}
	if sc.prev != nil {
		sc.ExtraJVM = append(sc.ExtraJVM, sc.prev.ExtraJVM...)
	}
	fromArgsFile, skippedArgs := 0, 0 // user_jvm_args.txt 里会带过来的、带不过来的参数个数
	if isArgs {                       // 这个文件只有 Forge / NeoForge 的启动脚本会读
		xmx, extra, skipped := readUserJVMArgs(filepath.Join(dir, "user_jvm_args.txt"))
		if xmx > 0 && !managedMem {
			sc.XmxMB = xmx
		}
		had := len(sc.ExtraJVM)
		sc.ExtraJVM = mergeArgs(sc.ExtraJVM, extra)
		fromArgsFile, skippedArgs = len(sc.ExtraJVM)-had, skipped
	}

	if mcsrc.KindOf(mcsrc.Core(sc.Core)) != mcsrc.KindProxy {
		level := "world" // 存档目录：server.properties 里没写（或者还没有这个文件）就是默认的 world
		if p, err := LoadProps(filepath.Join(dir, "server.properties")); err == nil {
			if v, ok := p.Get("server-port"); ok {
				sc.Port, _ = strconv.Atoi(strings.TrimSpace(v))
			}
			if v, ok := p.Get("level-name"); ok && strings.TrimSpace(v) != "" {
				level = strings.TrimSpace(v)
			}
		}
		// level-name 可以带子目录（worlds/main）；指到文件夹外面的不看
		if wp := filepath.Join(dir, filepath.FromSlash(level)); wp != dir && within(wp, dir) {
			if st, err := os.Stat(wp); err == nil && st.IsDir() {
				sc.HasWorld = true
			}
		}
		if b, err := readCapped(filepath.Join(dir, "eula.txt"), 64<<10); err == nil {
			sc.EULA = reEULA.Match(b)
		}
	}

	sc.Name = suggestName(sc, dir)

	// 提醒
	if top.note != "" && !trustPrev {
		sc.Notes = append(sc.Notes, top.note)
	}
	if sc.Core == "" {
		sc.Notes = append(sc.Notes, "没认出这是哪种服务端，请选一个最接近的：插件服选 Paper，模组服按加载器选 Forge、NeoForge 或 Fabric。")
	}
	if sc.MC == "" {
		sc.Notes = append(sc.Notes, "没认出版本，请手动填写。")
	}
	if len(sc.Targets) > 1 {
		sc.Notes = append(sc.Notes, fmt.Sprintf("这个文件夹里有 %d 个能启动的文件，已经选了最可能的那个，不对可以改。", len(sc.Targets)))
	}
	if fromArgsFile > 0 {
		sc.Notes = append(sc.Notes, fmt.Sprintf("user_jvm_args.txt 里的 %d 个 JVM 参数会带过来。", fromArgsFile))
	}
	if skippedArgs > 0 {
		sc.Notes = append(sc.Notes, fmt.Sprintf("user_jvm_args.txt 里有 %d 个带空格的 JVM 参数带不过来。需要的话，接管后对照 %s 手动补上。", skippedArgs, jvmArgsBackup))
	}
	if hints.otherFlags && sc.prev == nil {
		sc.Notes = append(sc.Notes, "原来的启动脚本里还有别的 JVM 参数，不会自动带过来。需要的话，接管后在「内存 / 启动」里补上。")
	}
	return sc, nil
}

// scanArgsTargets Forge / NeoForge（1.17 起）装好后留下的参数文件。装过多个版本时都列出来，新的在前。
func scanArgsTargets(dir string) []adoptCand {
	var out []adoptCand
	for _, g := range []struct {
		pattern string
		core    mcsrc.Core
	}{
		{"libraries/net/neoforged/neoforge/*/win_args.txt", mcsrc.CoreNeoForge},
		{"libraries/net/minecraftforge/forge/*/win_args.txt", mcsrc.CoreForge},
	} {
		ms, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(g.pattern)))
		var group []adoptCand
		for _, m := range ms {
			rel, err := filepath.Rel(dir, m)
			if err != nil {
				continue
			}
			ver := filepath.Base(filepath.Dir(m))
			var c adoptCand
			c.Target = "args:" + filepath.ToSlash(rel)
			c.Core = string(g.core)
			if b, err := readCapped(m, 1<<20); err == nil {
				toks := strings.Fields(string(b))
				for i := 0; i+1 < len(toks); i++ {
					switch toks[i] {
					case "--fml.mcVersion":
						c.MC = toks[i+1]
					case "--fml.forgeVersion", "--fml.neoForgeVersion":
						c.Build = toks[i+1]
					}
				}
			}
			if g.core == mcsrc.CoreForge { // 目录名是 <mc>-<forge>
				if mc, build, ok := strings.Cut(ver, "-"); ok {
					if c.MC == "" {
						c.MC = mc
					}
					if c.Build == "" {
						c.Build = build
					}
				}
			} else if c.Build == "" {
				c.Build = ver
			}
			group = append(group, c)
		}
		// 同一种加载器里新版本优先。按加载器自己的版本号比（Forge 的目录名前面带着 Minecraft 版本，不能整个拿来比）
		sort.SliceStable(group, func(a, b int) bool { return versionLess(group[b].Build, group[a].Build) })
		for n := range group {
			group[n].rank = rankArgs - min(n, 4)
		}
		out = append(out, group...)
	}
	return out
}

// scanRootJars 根目录里的 jar，逐个认。安装器、BuildTools 不算；读不开的（不是 zip）跳过。
func scanRootJars(dir string) []adoptCand {
	var out []adoptCand
	tried := 0
	for _, e := range listDir(dir, dirLimit) {
		name := e.Name()
		lname := strings.ToLower(name)
		if e.IsDir() || !strings.HasSuffix(lname, ".jar") || strings.Contains(lname, "installer") || strings.Contains(lname, "buildtools") {
			continue
		}
		if tried++; tried > 60 { // 根目录里堆了一大堆 jar 的，只看前面这些
			break
		}
		if c, ok := inspectJar(filepath.Join(dir, name)); ok {
			out = append(out, c)
		}
	}
	return out
}

// inspectJar 打开一个 jar，从清单和几个固定的文件里认出它是什么。
func inspectJar(p string) (adoptCand, bool) {
	var c adoptCand
	zr, err := zip.OpenReader(p)
	if err != nil {
		return c, false
	}
	defer zr.Close()
	files := map[string][]byte{}
	for _, f := range zr.File {
		switch f.Name {
		case "META-INF/MANIFEST.MF", "META-INF/versions.list", "version.json", "install.properties", "patch.properties":
			if rc, err := f.Open(); err == nil {
				b, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
				rc.Close()
				files[f.Name] = b
			}
		}
		if len(files) == 5 {
			break
		}
	}
	name := filepath.Base(p)
	lname := strings.ToLower(name)
	stem := strings.TrimSuffix(lname, ".jar")
	mf := parseManifest(files["META-INF/MANIFEST.MF"])
	main := mf["Main-Class"]
	lmain := strings.ToLower(main)
	c.Target = "jar:" + name

	var vj struct {
		ID   string `json:"id"`
		Java int    `json:"java_version"`
	}
	if b := files["version.json"]; b != nil {
		_ = json.Unmarshal(b, &vj)
	}
	c.JavaMajor = vj.Java
	brand, listMC := bundlerVersion(files["META-INF/versions.list"])
	inner := firstNonEmpty(listMC, vj.ID) // jar 自己写明的 Minecraft 版本

	switch {
	case strings.Contains(lmain, "leavesclip"):
		c.Core, c.rank = string(mcsrc.CoreLeaves), rankServer
		c.MC = firstNonEmpty(inner, propOf(files["patch.properties"], "version"))
	case strings.Contains(lmain, "paperclip"):
		c.rank = rankServer
		c.MC = firstNonEmpty(inner, propOf(files["patch.properties"], "version"))
		if brand == "" || brand == "server" { // 老版本的 jar 里没有 versions.list，看文件名
			brand, _, _ = strings.Cut(stem, "-")
		}
		switch brand {
		case "purpur":
			c.Core = string(mcsrc.CorePurpur)
		case "folia":
			c.Core = string(mcsrc.CoreFolia)
		case "leaves":
			c.Core = string(mcsrc.CoreLeaves)
		case "paper", "paperclip", "paperspigot", "server":
			c.Core = string(mcsrc.CorePaper)
		default:
			c.Core = string(mcsrc.CorePaper)
			c.note = "这个服务端是 Paper 的分支（用的是 Paper 的启动器），MineDeck 把它当 Paper 管理：启动、控制台、插件都一样。如果它其实是 Purpur、Folia 或 Leaves，可以在「服务端类型」里改。"
		}
	case main == "net.minecraft.bundler.Main" || strings.HasPrefix(main, "net.minecraft.server."):
		c.Core, c.rank, c.MC = string(mcsrc.CoreVanilla), rankVanilla, inner
	case strings.HasPrefix(main, "net.fabricmc."):
		c.Core, c.rank = string(mcsrc.CoreFabric), rankLoader
		ip := files["install.properties"]
		c.MC, c.Build = propOf(ip, "game-version"), propOf(ip, "fabric-loader-version")
	case strings.HasPrefix(main, "com.velocitypowered."):
		c.Core, c.rank = string(mcsrc.CoreVelocity), rankServer
		c.MC, _, _ = strings.Cut(mf["Implementation-Version"], " ")
	case strings.HasPrefix(main, "net.md_5.bungee.") || strings.Contains(lmain, "waterfall"):
		c.Core, c.rank = string(mcsrc.CoreWaterfall), rankServer
		c.MC = reNameVersion.FindString(mf["Implementation-Version"])
		if !strings.Contains(lname, "waterfall") {
			c.note = "这是 BungeeCord 系的代理端，MineDeck 把它当 Waterfall 管理。"
		}
	case strings.Contains(lmain, "banner"):
		c.Core, c.rank, c.MC = string(mcsrc.CoreBanner), rankHybrid, inner
	case strings.Contains(lmain, "mohist"):
		c.Core, c.rank, c.MC = string(mcsrc.CoreMohist), rankHybrid, inner
	case strings.HasPrefix(main, "net.minecraftforge.") || reLegacyForge.MatchString(name):
		c.Core, c.rank = string(mcsrc.CoreForge), rankLoader
		c.shim = strings.HasSuffix(stem, "-shim")
		if m := reLegacyForge.FindStringSubmatch(name); m != nil {
			c.MC, c.Build = m[1], m[2]
		}
	case strings.HasPrefix(main, "org.bukkit.craftbukkit."):
		c.Core, c.rank, c.MC = string(mcsrc.CorePaper), rankServer, inner
		c.note = "这是 Spigot / CraftBukkit，MineDeck 把它当 Paper 管理（启动、控制台、插件都一样）。"
	default:
		c.rank, c.MC = rankUnknown, inner
	}
	if c.MC == "" && c.Core != string(mcsrc.CoreFabric) { // Fabric 的文件名里是加载器版本，不能当 Minecraft 版本
		c.MC = reNameVersion.FindString(name)
	}
	return c, true
}

// parseManifest 读 jar 清单的主段（到第一个空行为止）；续行（以空格开头）接到上一个值后面。
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

// bundlerVersion 读 META-INF/versions.list 的第一行「哈希<TAB>版本<TAB>路径」：
// 版本是 Minecraft 版本，路径的文件名开头是服务端的名字（server、paper、purpur、folia…）。
func bundlerVersion(b []byte) (brand, mc string) {
	line, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	f := strings.Split(strings.TrimSpace(line), "\t")
	if len(f) < 3 {
		return "", ""
	}
	base := strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(f[2], "\\", "/"))), ".jar")
	brand, _, _ = strings.Cut(base, "-")
	return brand, strings.TrimSpace(f[1])
}

// propOf 从 .properties 内容里取一个键的值。
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

// targetLabel 候选启动方式给人看的说明。
func targetLabel(c *adoptCand) string {
	what := "没认出来的服务端"
	for _, ci := range mcsrc.Cores() {
		if string(ci.ID) == c.Core {
			what = ci.Name
		}
	}
	if rest, ok := strings.CutPrefix(c.Target, "jar:"); ok {
		if c.MC != "" {
			what += " " + c.MC
		}
		return rest + "（" + what + "）"
	}
	if c.Build != "" {
		what += " " + c.Build
	}
	if c.MC != "" {
		what += "（Minecraft " + c.MC + "）"
	}
	return what
}

// scriptHints 从原来的启动脚本里看出来的东西。
type scriptHints struct {
	targets     map[string]bool // 脚本点名用的启动方式（小写的 LaunchTarget 写法）
	xmxByTarget map[string]int  // 点名某个启动方式的那一行里分的内存
	xmxAny      int             // 脚本里第一个像样的 -Xmx
	otherFlags  bool            // 还有别的 JVM 参数（-XX:、-D、-javaagent、--add-opens）
}

func newScriptHints() scriptHints {
	return scriptHints{targets: map[string]bool{}, xmxByTarget: map[string]int{}}
}

// xmxFor 原来给这个启动方式分的内存：点名它的那一行里写的优先，否则脚本里随便哪个 -Xmx。
func (h scriptHints) xmxFor(target string) int {
	if x := h.xmxByTarget[strings.ToLower(target)]; x > 0 {
		return x
	}
	return h.xmxAny
}

// scanScripts 看根目录里的启动脚本：用的是哪个 jar 或参数文件、分了多少内存。
// 一行一行看，注释掉的行（rem、::、#）不算。只按 ASCII 找，脚本是 GBK 还是 UTF-8 都一样。MineDeck 自己生成的 run.bat 不算。
// Windows 的脚本（.bat / .cmd / .ps1）说了算；一个都没有时才看 .sh——Forge 安装器会同时留下 run.bat 和 run.sh，
// 用户后来只改了 run.bat 的话，run.sh 里写的还是老的。
func scanScripts(dir string) scriptHints {
	win, sh := newScriptHints(), newScriptHints()
	winSeen, seen := false, 0
	for _, e := range listDir(dir, dirLimit) {
		var h *scriptHints
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".bat", ".cmd", ".ps1":
			h = &win
		case ".sh":
			h = &sh
		default:
			continue
		}
		if e.IsDir() || seen >= 30 {
			continue
		}
		seen++
		b, err := readCapped(filepath.Join(dir, e.Name()), 256<<10)
		if err != nil || isOurRunBat(b) {
			continue
		}
		if h == &win {
			winSeen = true
		}
		for _, line := range bytes.Split(b, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if isScriptComment(line) {
				continue
			}
			var hit []string
			for _, m := range reScriptJar.FindAllSubmatch(line, -1) {
				name := m[1]
				if len(name) == 0 {
					name = m[2]
				}
				hit = append(hit, "jar:"+strings.ToLower(string(name)))
			}
			for _, m := range reScriptArgs.FindAllSubmatch(line, -1) {
				hit = append(hit, "args:"+strings.ToLower(strings.ReplaceAll(string(m[1]), "\\", "/"))+"/win_args.txt")
			}
			x := xmxOf(line)
			for _, t := range hit {
				h.targets[t] = true
				if x > 0 && h.xmxByTarget[t] == 0 {
					h.xmxByTarget[t] = x
				}
			}
			if x > 0 && h.xmxAny == 0 {
				h.xmxAny = x
			}
			if reOtherFlags.Match(line) {
				h.otherFlags = true
			}
		}
	}
	if winSeen {
		return win
	}
	return sh
}

// isScriptComment 这一行（已去掉首尾空白）是不是脚本里的注释：bat 的 rem 和 ::，sh / ps1 的 #。
func isScriptComment(line []byte) bool {
	l := bytes.ToLower(line)
	l = bytes.TrimPrefix(l, []byte("@"))
	return bytes.HasPrefix(l, []byte("::")) || bytes.HasPrefix(l, []byte("#")) ||
		bytes.Equal(l, []byte("rem")) || bytes.HasPrefix(l, []byte("rem ")) || bytes.HasPrefix(l, []byte("rem\t"))
}

// xmxOf 从一段文本里找 -Xmx，换算成 MB。写了好几个时取最后一个像样的（Java 也是后面的盖前面的）；
// 没有，或者都不像话（不到 512 MB、超过 1 TB）返回 0。
func xmxOf(b []byte) int {
	out := 0
	for _, m := range reXmx.FindAllSubmatch(b, -1) {
		n, err := strconv.Atoi(string(m[1])) // 正则限了最多 9 位数字，乘 1024 不会溢出
		if err != nil || n <= 0 {
			continue
		}
		switch m[2][0] | 0x20 {
		case 'g':
			n *= 1024
		case 'k':
			n /= 1024
		}
		if n >= 512 && n <= 1<<20 {
			out = n
		}
	}
	return out
}

// readUserJVMArgs 读 Forge / NeoForge 的 user_jvm_args.txt（Java 的参数文件写法）：-Xmx 换成内存，-Xms 不要，
// 其余的当自定义 JVM 参数带过来。带空格的参数带不过来（界面上的 JVM 参数是按空白分开的），只数个数。
func readUserJVMArgs(p string) (xmxMB int, extra []string, skipped int) {
	b, err := readCapped(p, 256<<10)
	if err != nil {
		return 0, nil, 0
	}
	for _, line := range strings.Split(string(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))), "\n") {
		for _, tok := range argfileTokens(line) {
			low := strings.ToLower(tok)
			switch {
			case strings.HasPrefix(low, "-xmx"):
				if n := xmxOf([]byte(tok)); n > 0 {
					xmxMB = n
				}
			case strings.HasPrefix(low, "-xms"):
			case strings.ContainsAny(tok, " \t"):
				skipped++
			case len(extra) < 100:
				extra = append(extra, tok)
			}
		}
	}
	return xmxMB, extra, skipped
}

// argfileTokens 按 Java 参数文件的写法拆一行：空白分隔；引号（单、双）里的空白不拆，引号本身去掉；
// 一个参数开头的 # 起到行尾是注释（参数中间的 # 不算）。
func argfileTokens(line string) []string {
	var out []string
	var cur strings.Builder
	inTok := false
	var quote rune
	flush := func() {
		if inTok {
			out = append(out, cur.String())
			cur.Reset()
			inTok = false
		}
	}
	rs := []rune(strings.TrimRight(line, "\r"))
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote != 0:
			if r == '\\' && i+1 < len(rs) && (rs[i+1] == quote || rs[i+1] == '\\') {
				i++
				cur.WriteRune(rs[i])
			} else if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inTok = r, true
		case r == ' ' || r == '\t':
			flush()
		case r == '#' && !inTok:
			return out
		default:
			inTok = true
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// isOurRunBat 这个 run.bat 是不是 MineDeck（或改名前的 AutoMCHUB）生成的：接管时不用备份，也不拿它当「用户原来的脚本」看。
func isOurRunBat(b []byte) bool {
	return bytes.Contains(b, []byte(" - MineDeck\r\ncd /d \"%~dp0\"")) || bytes.Contains(b, []byte(" - AutoMCHUB\r\ncd /d \"%~dp0\""))
}

// mcFromLogs 从最近一次的日志里找 Minecraft 版本。
func mcFromLogs(dir string) string {
	b, err := readCapped(filepath.Join(dir, "logs", "latest.log"), 512<<10)
	if err != nil {
		return ""
	}
	if m := reLogMC.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	if m := reLogFmlMC.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// mcFromLayout 从目录结构里猜 Minecraft 版本：Fabric 把原版 jar 放在 .fabric/server/<版本>-server.jar，
// 原版和 Paper 系（1.18 起）第一次启动时把自己解到 versions/<版本>/ 下面。有好几个时取最新的。
func mcFromLayout(dir string) string {
	best := ""
	consider := func(v string) {
		if reNameVersion.MatchString(v) && (best == "" || versionLess(best, v)) {
			best = v
		}
	}
	for _, e := range listDir(filepath.Join(dir, ".fabric", "server"), 200) {
		if v, ok := strings.CutSuffix(e.Name(), "-server.jar"); ok && !e.IsDir() {
			consider(v)
		}
	}
	if best != "" {
		return best
	}
	for _, e := range listDir(filepath.Join(dir, "versions"), 200) {
		if e.IsDir() {
			consider(e.Name())
		}
	}
	return best
}

// readCapped 读文件的前 max 字节。
func readCapped(p string, max int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max))
}

// listDir 列出文件夹里的东西，最多 max 项，按名字排好（顺序固定，识别结果才稳定）。打不开返回空。
func listDir(dir string, max int) []os.DirEntry {
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer f.Close()
	ents, _ := f.ReadDir(max)
	sort.Slice(ents, func(a, b int) bool { return ents[a].Name() < ents[b].Name() })
	return ents
}

// versionLess 版本 a 是不是比 b 旧。先比开头用点分开的数字段（1.20 比 1.20.1 旧，47.2.17 比 47.4.20 旧）；
// 数字部分一样时，后面带尾巴的（-beta、-rc1、-pre2）比不带的旧。
func versionLess(a, b string) bool {
	an, as := splitVersion(a)
	bn, bs := splitVersion(b)
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

// splitVersion 把版本号拆成开头的数字段和后面的尾巴："20.4.190-beta" → [20 4 190]、"-beta"。
func splitVersion(v string) (nums []int, rest string) {
	i := 0
	for i < len(v) {
		j := i
		for j < len(v) && v[j] >= '0' && v[j] <= '9' {
			j++
		}
		if j == i || j-i > 9 {
			break
		}
		n, _ := strconv.Atoi(v[i:j])
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

// suggestName 建议的名称：MineDeck 管过的用原来的名字，否则用文件夹名（去掉名称里不能有的字符）。
func suggestName(sc *AdoptScan, dir string) string {
	if sc.prev != nil && validateName(sc.prev.Name) == nil {
		return strings.TrimSpace(sc.prev.Name)
	}
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(invalidNameChars, r) || r < 0x20 {
			return -1
		}
		return r
	}, filepath.Base(dir))
	name = strings.TrimRight(strings.TrimSpace(name), ". ")
	if r := []rune(name); len(r) > 36 { // 留几个字给重名时加的序号
		name = strings.TrimRight(string(r[:36]), ". ")
	}
	if validateName(name) != nil {
		return "我的服务器"
	}
	return name
}

// noServerErr 文件夹里没找到服务端时的报错：下一层有像服务器的文件夹就点出来（常见的是选到了上一层）。
func noServerErr(dir string) error {
	var subs []string
	checked := 0
	for _, e := range listDir(dir, dirLimit) {
		if !e.IsDir() {
			continue
		}
		if checked++; checked > 200 || len(subs) >= 5 {
			break
		}
		if looksLikeServer(filepath.Join(dir, e.Name())) {
			subs = append(subs, "「"+e.Name()+"」")
		}
	}
	if len(subs) > 0 {
		return fmt.Errorf("这个文件夹里没找到服务端。它下面的%s像是服务器，请选到具体那一个文件夹", strings.Join(subs, ""))
	}
	return fmt.Errorf("这个文件夹里没找到服务端：最外层没有能启动的 jar，也没有 Forge / NeoForge 装好后的 libraries 文件夹。请选服务端 jar 所在的那个文件夹")
}

// looksLikeServer 粗看一个文件夹像不像服务器：有服务端常见的文件，或者根上直接放着 jar（只用来在报错里提示，不当识别结果）。
func looksLikeServer(dir string) bool {
	for _, e := range listDir(dir, dirLimit) {
		name := strings.ToLower(e.Name())
		switch name {
		case "server.properties", "instance.json", "eula.txt", "libraries", "velocity.toml":
			return true
		}
		if !e.IsDir() && strings.HasSuffix(name, ".jar") {
			return true
		}
	}
	return false
}

package diag

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// rules 规则库第一版（设计文档 §5.3 的 13 类），按顺序检查。每条规则都有 testdata 里的正例和反例。
var rules = []func(*ruleCtx) *Finding{
	ruleMissingDeps,    // 1 缺少前置模组
	ruleClientOnly,     // 2 客户端模组放进了服务端
	ruleWrongLoader,    // 3 装错加载器
	ruleDuplicate,      // 4 重复模组
	ruleIncompatible,   // 5 模组之间声明不兼容
	ruleMixin,          // 6 Mixin 冲突
	ruleJava,           // 7 Java 版本不对 / JVM 参数不被支持
	rulePort,           // 8 端口被占
	ruleMemory,         // 9 内存不够
	ruleWatchdog,       // 10 卡死被看门狗关掉
	ruleEULA,           // 11 没同意 EULA
	ruleClientMismatch, // 12 好友进不来（模组对不上）
	ruleBrokenConfig,   // 13 配置文件被改坏
}

// ---------- 1 缺少前置模组 ----------

var (
	// Forge / NeoForge 的 ModSorter：「Missing or unsupported mandatory dependencies:」下一行起每行一个
	// 版本范围可能是空的：Connector 转过来的 Fabric 模组（测试报告 E-06：Expected range: ''）
	depLineRe = regexp.MustCompile(`Mod ID: '(.+?)', Requested by: '(.+?)', Expected range: '(.*?)', Actual version: '(.+?)'`)
	depHeadRe = regexp.MustCompile(`Missing or unsupported mandatory dependencies`)
	// NeoForge 的汇总：「Mod create requires geckolib 4.2 or above」+「Currently, geckolib is not installed」；
	// 版本范围空着时只有「Mod continuity requires fabric_api 」
	neoReqRe = regexp.MustCompile(`Mod (\S+) requires (\S+)(?: (.+?))?\s*$`)
	neoCurRe = regexp.MustCompile(`Currently, (\S+) is (not installed|\S+)`)
	// Fabric：「Mod 'Create' (create) 0.5.1 requires version 0.6 or later of 'Flywheel' (flywheel), which is missing!」
	// 两种写法：「of fabric-api, which is missing」和「of mod 'Flywheel' (flywheel), which is missing」
	fabricReqRe = regexp.MustCompile(`Mod '(.+?)' \((\S+)\) \S+ requires (?:version (.+?)|any version) of (?:mod '.+?' \((\S+)\)|(\S+)), which is missing`)
)

func ruleMissingDeps(c *ruleCtx) *Finding {
	type miss struct{ dep, by, want, have string }
	var ms []miss
	var hit []int
	seen := map[string]bool{}
	add := func(m miss, i int) {
		if k := m.by + "→" + m.dep; !seen[k] {
			seen[k] = true
			ms = append(ms, m)
		}
		hit = append(hit, i)
	}
	block := "" // 正在读哪一段：deps（缺少依赖）或 incomp（不兼容，格式一样，由规则 5 处理）
	for i, l := range c.ls {
		if l.skip {
			continue
		}
		if depHeadRe.MatchString(l.text) {
			block = "deps"
			hit = append(hit, i)
			continue
		}
		if incompHeadRe.MatchString(l.text) {
			block = "incomp"
			continue
		}
		if m := depLineRe.FindStringSubmatch(l.text); m != nil {
			if block != "incomp" {
				add(miss{dep: m[1], by: m[2], want: m[3], have: m[4]}, i)
			}
			continue
		}
		if m := fabricReqRe.FindStringSubmatch(l.text); m != nil {
			want := m[3]
			if want == "" {
				want = "任意版本"
			}
			add(miss{dep: m[4] + m[5], by: m[2], want: want, have: "[MISSING]"}, i)
			continue
		}
		if m := neoReqRe.FindStringSubmatch(l.text); m != nil && !strings.Contains(l.text, "is incompatible") {
			have := ""
			if i+1 < len(c.ls) {
				if n := neoCurRe.FindStringSubmatch(c.ls[i+1].text); n != nil && n[1] == m[2] {
					have = strings.TrimRight(n[2], ",") // 汇总里几条之间用逗号隔开
				}
			}
			if have != "" {
				if have == "not installed" {
					have = "[MISSING]"
				}
				add(miss{dep: m[2], by: m[1], want: strings.TrimSpace(m[3]), have: have}, i)
			}
			continue
		}
		if l.text != "" && !strings.HasPrefix(l.text, "\t") && !strings.HasPrefix(l.text, " ") {
			block = ""
		}
	}
	if len(ms) == 0 {
		return nil
	}
	f := &Finding{Rule: "missing-deps", Title: "缺少前置模组", Evidence: c.evidence(hit)}
	var names []string
	for _, m := range ms {
		names = append(names, m.dep)
		f.Mods = append(f.Mods, m.dep, m.by)
		if file := c.modFile(m.by); file != "" {
			f.Files = append(f.Files, file)
		}
		want := m.want
		if want == "" {
			want = "任意版本"
		}
		switch {
		case m.have == "[MISSING]" || m.have == "":
			f.Details = append(f.Details, fmt.Sprintf("%s 需要 %s（版本 %s），但没有装", m.by, m.dep, want))
		case m.want == "": // 装了却说不满足：模组没写清要哪一版（多见于 Connector 转过来的 Fabric 模组）
			f.Details = append(f.Details, fmt.Sprintf("%s 需要 %s，装的是 %s，加载器认为版本不对（这个模组没写清要哪一版，一般是要更新的 %s）", m.by, m.dep, m.have, m.dep))
		default:
			f.Details = append(f.Details, fmt.Sprintf("%s 需要 %s（版本 %s），装的是 %s，版本不对", m.by, m.dep, m.want, m.have))
		}
	}
	f.Mods, f.Files = uniq(f.Mods), uniq(f.Files)
	f.Summary = fmt.Sprintf("少装了前置模组，或者前置的版本不对：%s", strings.Join(uniq(names), "、"))
	f.Fixes = []string{
		"按模组 ID 去 Modrinth 搜这个前置（选和服务器一样的加载器和 MC 版本），装进 mods 文件夹",
		"版本不对的，换成满足版本范围的那一版",
		"好友的客户端也要装同样的前置",
	}
	return f
}

// ---------- 2 客户端模组放进了服务端 ----------

var (
	distRe = regexp.MustCompile(`Attempted to load class (\S+) for invalid dist DEDICATED_SERVER`)
	// 堆栈帧：「at com.example.Mod.init(Mod.java:42) ~[examplemod-1.0.jar%23123!/:?]」或「~[examplemod-1.0.jar:?]」
	frameRe     = regexp.MustCompile(`^\s*at ([\w$.]+)\.[\w$<>]+\(.*?\)(?: ~?\[([^\]%!:]+\.jar))?`)
	modFailRe   = regexp.MustCompile(`Mod (\S+) has failed to load correctly|for modid (\S+)|ModID: ([\w\-]+)`)
	coreFrameRe = regexp.MustCompile(`^(net\.minecraft|net\.minecraftforge|net\.neoforged|java\.|javax\.|jdk\.|sun\.|cpw\.mods|org\.spongepowered|com\.mojang|org\.objectweb|org\.apache|com\.google|io\.netty|MC-BOOTSTRAP)`)
	coreJarRe   = regexp.MustCompile(`(?i)^(forge|neoforge|fmlloader|fmlcore|javafmllanguage|lowcodelanguage|mclanguage|server|minecraft|loader|securejarhandler|bootstraplauncher|modlauncher|mixin|sponge-mixin)[-.]`)
)

// mixinMissRe mixin 找不到目标类、跳过了这个 mixin：「@Mixin target <类> was not found <配置>.json:<mixin>」。
var mixinMissRe = regexp.MustCompile(`@Mixin target (\S+) was not found (\S+\.json):`)

// harmlessMixinTarget 唯一的例外：PlayerEngine（AI 伙伴的「身体」）的 mixin playerengine.mixins.json 里有一个只给客户端用的
// （ClientBlockBreakMixin，目标是 net.minecraft.client.multiplayer.MultiPlayerGameMode），在专用服务器上加载器会先打一行
// 「Attempted to load class … for invalid dist DEDICATED_SERVER」的 ERROR、mixin 再打一行带同样原文的 WARN，
// 随后说明目标没找到、跳过了，服务器照常启动（companion/README.md「已知问题」，NeoForge 21.1.265、Forge 47.4.28 实测）。
// 只放过这种组合：类在 net/minecraft/client/ 下，而且紧跟着 5 行以内就是 playerengine.mixins.json 对同一个类的「was not found」。
// 别的模组、别的写法（比如带堆栈的加载失败）照常算客户端模组放进了服务端。
func harmlessMixinTarget(c *ruleCtx, i int, cls string) bool {
	if !strings.HasPrefix(cls, "net/minecraft/client/") {
		return false
	}
	dotted := strings.ReplaceAll(cls, "/", ".")
	for j := i + 1; j < len(c.ls) && j <= i+5; j++ {
		if m := mixinMissRe.FindStringSubmatch(c.ls[j].text); m != nil && m[1] == dotted && m[2] == "playerengine.mixins.json" {
			return true
		}
	}
	return false
}

func ruleClientOnly(c *ruleCtx) *Finding {
	var idx []int
	for _, i := range c.find(distRe) {
		if !harmlessMixinTarget(c, i, distRe.FindStringSubmatch(c.ls[i].text)[1]) {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return nil
	}
	cls := distRe.FindStringSubmatch(c.ls[idx[0]].text)[1]
	f := &Finding{Rule: "client-only", Title: "客户端模组放进了服务端", Evidence: c.evidence(idx)}
	var files, mods []string
	// 往下看堆栈：第一个不属于游戏本体、加载器和 Java 的帧就是装错的模组（帧里常带着 jar 名）
	for _, i := range idx {
		for j := i + 1; j < len(c.ls) && j < i+80; j++ {
			m := frameRe.FindStringSubmatch(c.ls[j].text)
			if m == nil {
				continue
			}
			if coreFrameRe.MatchString(m[1]) {
				continue
			}
			if m[2] != "" && !coreJarRe.MatchString(baseName(m[2])) {
				files = append(files, baseName(m[2]))
			} else if c.in.FindClass != nil {
				files = append(files, c.in.FindClass(m[1]))
			}
			break
		}
		// 往上看：NeoForge 的「Mod xxx has failed to load correctly」、Forge 的「… for modid xxx」
		for j := i; j >= 0 && j > i-20; j-- {
			if m := modFailRe.FindStringSubmatch(c.ls[j].text); m != nil {
				mods = append(mods, m[1]+m[2]+m[3])
				break
			}
		}
	}
	// 报错里的类本身就是模组的（不是 net/minecraft/client/…）：直接按类名找 jar
	if !strings.HasPrefix(cls, "net/minecraft/") && !strings.HasPrefix(cls, "com/mojang/") && c.in.FindClass != nil {
		files = append(files, c.in.FindClass(cls))
	}
	for _, m := range mods {
		files = append(files, c.modFile(m))
	}
	f.Files, f.Mods = uniq(files), uniq(mods)
	who := "某个模组"
	if len(f.Files) > 0 {
		who = strings.Join(f.Files, "、")
	} else if len(f.Mods) > 0 {
		who = strings.Join(f.Mods, "、")
	}
	f.Summary = fmt.Sprintf("%s 只能装在客户端（小地图、界面美化、光影一类），服务端加载它时用到了客户端才有的类 %s", who, strings.ReplaceAll(cls, "/", "."))
	f.Fixes = []string{"把这个模组从服务端禁用（mods 文件夹里改名成 .jar.disabled），客户端照常装着就行"}
	if len(f.Files) == 0 {
		f.Fixes = append(f.Fixes, "认不出是哪个 jar：看报错下面的堆栈，第一个不是 net.minecraft、net.neoforged、net.minecraftforge 开头的类属于哪个模组")
	}
	return f
}

// ---------- 3 装错加载器 ----------

var (
	wrongLoaderRe = regexp.MustCompile(`File (.+?) is (a Fabric mod|for Minecraft Forge or an older version of NeoForge|a Bukkit or Bukkit-implementor)`)
	// Paper 系：把模组放进了 plugins
	notPluginRe = regexp.MustCompile(`Could not load '(.+?\.jar)' in folder '(.+?)'`)
	invalidYML  = regexp.MustCompile(`InvalidDescriptionException|Invalid plugin\.yml|plugin\.yml not found|does not contain plugin\.yml`)
)

func ruleWrongLoader(c *ruleCtx) *Finding {
	idx := c.find(wrongLoaderRe)
	var files, details []string
	for _, i := range idx {
		m := wrongLoaderRe.FindStringSubmatch(c.ls[i].text)
		file := baseName(strings.Trim(m[1], "'\""))
		files = append(files, file)
		switch {
		case strings.Contains(m[2], "Fabric"):
			details = append(details, file+" 是 Fabric 模组，"+c.loaderName()+" 加载不了")
		case strings.Contains(m[2], "Bukkit"):
			details = append(details, file+" 是插件（Bukkit / Spigot / Paper 用的），不是模组")
		default:
			details = append(details, file+" 是给 Forge（或旧版 NeoForge）的，NeoForge 加载不了")
		}
	}
	for _, i := range c.find(notPluginRe) {
		near := false
		for j := i; j < len(c.ls) && j < i+4; j++ {
			near = near || invalidYML.MatchString(c.ls[j].text)
		}
		if near {
			m := notPluginRe.FindStringSubmatch(c.ls[i].text)
			file := baseName(m[1])
			files = append(files, file)
			details = append(details, file+" 不是插件（多半是模组），插件服加载不了")
			idx = append(idx, i)
		}
	}
	if len(files) == 0 {
		return nil
	}
	f := &Finding{Rule: "wrong-loader", Title: "装错加载器", Files: uniq(files), Details: uniq(details), Evidence: c.evidence(idx),
		Summary: fmt.Sprintf("有 %d 个文件是给别的加载器用的：%s", len(uniq(files)), strings.Join(uniq(files), "、")),
		Fixes:   []string{"把它禁用（改名成 .jar.disabled）", "到 Modrinth 找这个模组的 " + c.loaderName() + " 版本（同一个 MC 版本）"}}
	if c.loaderName() == "插件服" {
		f.Fixes[1] = "模组要在模组服（NeoForge / Forge / Fabric）上用；插件服只能装插件"
	}
	return f
}

func (c *ruleCtx) loaderName() string {
	switch c.in.Core {
	case "neoforge":
		return "NeoForge"
	case "forge":
		return "Forge"
	case "fabric":
		return "Fabric"
	case "paper", "purpur", "leaves", "folia":
		return "插件服"
	}
	return "这个服务端"
}

// ---------- 4 重复模组 ----------

var (
	dupHeadRe = regexp.MustCompile(`Found duplicate mods`)
	dupLineRe = regexp.MustCompile(`Mod ID: '(.+?)' from mod files: (.+)$`)
	dupNeoRe  = regexp.MustCompile(`Mod (\S+) is present in multiple files: (.+)$`)
)

func ruleDuplicate(c *ruleCtx) *Finding {
	var idx []int
	var mods, files, details []string
	for i, l := range c.ls {
		if l.skip {
			continue
		}
		if dupHeadRe.MatchString(l.text) {
			idx = append(idx, i)
			continue
		}
		m := dupLineRe.FindStringSubmatch(l.text)
		if m == nil {
			m = dupNeoRe.FindStringSubmatch(l.text)
		}
		if m == nil {
			continue
		}
		idx = append(idx, i)
		mods = append(mods, m[1])
		fs := strings.Split(m[2], ",")
		for k := range fs {
			fs[k] = baseName(strings.TrimSpace(fs[k]))
		}
		files = append(files, fs...)
		details = append(details, fmt.Sprintf("%s 装了不止一份：%s", m[1], strings.Join(fs, "、")))
	}
	if len(mods) == 0 {
		return nil
	}
	return &Finding{Rule: "duplicate-mods", Title: "重复模组", Mods: uniq(mods), Files: uniq(files), Details: uniq(details), Evidence: c.evidence(idx),
		Summary: "同一个模组装了两份（多半是更新时旧版没删）：" + strings.Join(uniq(mods), "、"),
		Fixes:   []string{"每个模组只留版本新的那一份，旧的禁用（改名成 .jar.disabled）或删掉"}}
}

// ---------- 5 模组之间声明不兼容（NeoForge） ----------

var (
	incompHeadRe = regexp.MustCompile(`Incompatibilities between mods|Found incompatible mods`)
	incompNeoRe  = regexp.MustCompile(`Mod (\S+) is incompatible with (\S+)`)
)

func ruleIncompatible(c *ruleCtx) *Finding {
	var idx []int
	var mods, details []string
	inBlock := false
	for i, l := range c.ls {
		if l.skip {
			continue
		}
		if incompHeadRe.MatchString(l.text) {
			inBlock = true
			idx = append(idx, i)
			continue
		}
		if depHeadRe.MatchString(l.text) {
			inBlock = false
			continue
		}
		if m := incompNeoRe.FindStringSubmatch(l.text); m != nil {
			idx = append(idx, i)
			mods = append(mods, m[1], m[2])
			details = append(details, fmt.Sprintf("%s 声明和 %s 不兼容", m[1], m[2]))
			continue
		}
		if m := depLineRe.FindStringSubmatch(l.text); m != nil && inBlock {
			idx = append(idx, i)
			mods = append(mods, m[2], m[1])
			details = append(details, fmt.Sprintf("%s 声明和 %s（%s）不兼容", m[2], m[1], m[4]))
			continue
		}
		if !strings.HasPrefix(l.text, "\t") && !strings.HasPrefix(l.text, " ") {
			inBlock = false
		}
	}
	if len(details) == 0 {
		return nil
	}
	f := &Finding{Rule: "incompatible-mods", Title: "模组之间声明不兼容", Mods: uniq(mods), Details: uniq(details), Evidence: c.evidence(idx),
		Summary: "有模组声明了不能和另一个模组一起装：" + strings.Join(uniq(details), "；"),
		Fixes:   []string{"两个里面留一个：想留哪个由你决定，另一个禁用（改名成 .jar.disabled）"}}
	for _, m := range f.Mods {
		f.Files = append(f.Files, c.modFile(m))
	}
	f.Files = uniq(f.Files)
	return f
}

// ---------- 6 Mixin 冲突 ----------

var (
	mixinRe      = regexp.MustCompile(`MixinApplyError|InvalidInjectionException|InvalidMixinException|MixinTransformerError|Mixin application of .+ has failed|Mixin \[.+?\] from phase \[\w+\] in config \[.+?\] FAILED during APPLY`)
	mixinCfgRe   = regexp.MustCompile(`in config \[(.+?)\]|\b([\w.\-]+\.mixins?\.json)\b|\b([\w.\-]+mixins\.json)\b`)
	mixinClassRe = regexp.MustCompile(`Mixin \[([^\]]+)\] from phase`)
)

func ruleMixin(c *ruleCtx) *Finding {
	idx := c.find(mixinRe)
	if len(idx) == 0 {
		return nil
	}
	var cfgs, mods, files, classes []string
	for _, i := range idx {
		for j := i; j < len(c.ls) && j < i+3; j++ { // 配置名有时在下一两行
			for _, m := range mixinCfgRe.FindAllStringSubmatch(c.ls[j].text, -1) {
				cfgs = append(cfgs, m[1]+m[2]+m[3])
			}
		}
		if m := mixinClassRe.FindStringSubmatch(c.ls[i].text); m != nil {
			classes = append(classes, m[1])
		}
	}
	cfgs = uniq(cfgs)
	var details []string
	for _, cfg := range cfgs {
		id, file := c.modByMixin(cfg)
		if id == "" { // 没有模组清单时按配置名猜：create.mixins.json → create
			id = strings.SplitN(cfg, ".", 2)[0]
			file = c.modFile(id)
		}
		mods, files = append(mods, id), append(files, file)
		details = append(details, fmt.Sprintf("mixin 配置 %s 属于 %s", cfg, id))
	}
	f := &Finding{Rule: "mixin", Title: "Mixin 冲突", Mods: uniq(mods), Files: uniq(files), Details: details, Evidence: c.evidence(idx)}
	if len(f.Mods) > 0 {
		f.Summary = "模组修改游戏代码时失败了（Mixin 冲突），出问题的是：" + strings.Join(f.Mods, "、")
	} else {
		f.Summary = "有模组修改游戏代码时失败了（Mixin 冲突）"
	}
	if len(classes) > 0 {
		f.Details = append(f.Details, "出错的 mixin 类："+strings.Join(uniq(classes), "、"))
	}
	f.Fixes = []string{"先把这个模组更新到最新版（同一个 MC 版本）", "还不行就和最近新装的、同类功能的模组二选一，禁用其中一个"}
	return f
}

// ---------- 7 Java 版本不对 / JVM 参数不被支持 ----------

var (
	classVerRe  = regexp.MustCompile(`UnsupportedClassVersionError: (\S+) has been compiled by a more recent version of the Java Runtime \(class file version (\d+)\.\d+\), this version of the Java Runtime only recognizes class file versions up to (\d+)\.\d+`)
	urlLoaderRe = regexp.MustCompile(`ClassLoaders\$AppClassLoader cannot be cast to class java\.net\.URLClassLoader`)
	featureRe   = regexp.MustCompile(`Mod (\S+) is missing a feature it requires to run`)
	featureReq  = regexp.MustCompile(`It requires (.+?) but (.+?) is available`)
	vmOptRe     = regexp.MustCompile(`Unrecognized VM option '(.+?)'|Unrecognized option: (\S+)`)
	noJVMRe     = regexp.MustCompile(`Could not create the Java Virtual Machine`)
)

func ruleJava(c *ruleCtx) *Finding {
	if idx := c.find(classVerRe); len(idx) > 0 {
		m := classVerRe.FindStringSubmatch(c.ls[idx[0]].text)
		need, _ := strconv.Atoi(m[2])
		have, _ := strconv.Atoi(m[3])
		need, have = need-44, have-44 // 类文件版本 52 是 Java 8，61 是 Java 17，65 是 Java 21
		return &Finding{Rule: "java-version", Title: "Java 版本不对", Evidence: c.evidence(idx),
			Summary: fmt.Sprintf("%s 要 Java %d 或更新，现在用的是 Java %d", strings.ReplaceAll(m[1], "/", "."), need, have),
			Fixes:   []string{fmt.Sprintf("给这台服务器换成 Java %d（MineDeck 可以自动下载便携版）", need)}}
	}
	if idx := c.find(urlLoaderRe); len(idx) > 0 {
		return &Finding{Rule: "java-version", Title: "Java 版本不对", Evidence: c.evidence(idx),
			Summary: "老版本的 Forge（1.16.5 及更早）只能用 Java 8，现在用的 Java 太新了",
			Fixes:   []string{"给这台服务器换成 Java 8"}}
	}
	if idx := c.find(featureRe); len(idx) > 0 {
		f := &Finding{Rule: "java-version", Title: "Java 版本不对", Evidence: c.evidence(idx)}
		var mods []string
		need := ""
		for _, i := range idx {
			mods = append(mods, featureRe.FindStringSubmatch(c.ls[i].text)[1])
			if i+1 < len(c.ls) {
				if m := featureReq.FindStringSubmatch(c.ls[i+1].text); m != nil {
					need = m[1]
					f.Details = append(f.Details, fmt.Sprintf("要求 %s，现在是 %s", m[1], m[2]))
				}
			}
		}
		f.Mods = uniq(mods)
		f.Summary = "模组声明了运行环境的要求（多半是 Java 版本），现在的环境不满足：" + strings.Join(f.Mods, "、")
		f.Fixes = []string{"按要求给这台服务器换 Java（MineDeck 可以自动下载便携版）"}
		if need != "" {
			f.Fixes[0] = fmt.Sprintf("给这台服务器换成满足 %s 的 Java", need)
		}
		return f
	}
	if idx := c.find(vmOptRe); len(idx) > 0 && len(c.find(noJVMRe)) > 0 {
		var opts []string
		for _, i := range idx {
			m := vmOptRe.FindStringSubmatch(c.ls[i].text)
			opts = append(opts, m[1]+m[2])
		}
		opts = uniq(opts)
		return &Finding{Rule: "jvm-option", Title: "JVM 参数不被支持", Evidence: c.evidence(append(idx, c.find(noJVMRe)...)),
			Summary: "现在的 Java 不认识这些启动参数，Java 虚拟机没能启动：" + strings.Join(opts, " "),
			Fixes:   []string{"在「内存 / 启动」里删掉这些 JVM 参数（或者换一个支持它们的 Java 版本）"}}
	}
	return nil
}

// ---------- 8 端口被占 ----------

var (
	bindRe   = regexp.MustCompile(`\*\*\*\* FAILED TO BIND TO PORT|Address already in use|java\.net\.BindException|Couldn't bind to`)
	listenRe = regexp.MustCompile(`Starting Minecraft server on (\S*):(\d+)`)
	queryRe  = regexp.MustCompile(`Unable to initiali[sz]e (query|rcon|RCON)`)
)

func rulePort(c *ruleCtx) *Finding {
	idx := c.find(bindRe)
	if len(idx) == 0 {
		return nil
	}
	port := c.in.Port
	for _, l := range c.ls {
		if m := listenRe.FindStringSubmatch(l.text); m != nil {
			port, _ = strconv.Atoi(m[2])
		}
	}
	f := &Finding{Rule: "port-in-use", Title: "端口被占", Evidence: c.evidence(idx)}
	what := "服务器的端口"
	for _, i := range c.find(queryRe) {
		what = map[string]string{"query": "查询（query）端口", "rcon": "远程控制（RCON）端口", "RCON": "远程控制（RCON）端口"}[queryRe.FindStringSubmatch(c.ls[i].text)[1]]
		f.Evidence = c.evidence(append([]int{i}, idx...))
	}
	if port > 0 && what == "服务器的端口" {
		f.Summary = fmt.Sprintf("端口 %d 已经被占用了，服务器绑定不上（可能是上次没关干净、开了两个，或者被别的程序占了）", port)
	} else {
		f.Summary = what + "已经被占用了（可能是上次没关干净、开了两个，或者被别的程序占了）"
	}
	f.Fixes = []string{"查一下是哪个程序占着这个端口，关掉它", "或者在 server.properties 里把 server-port 改成别的（如 25566），好友连的时候也要换成新端口"}
	return f
}

// ---------- 9 内存不够 ----------

var (
	oomRe     = regexp.MustCompile(`OutOfMemoryError: (Java heap space|GC overhead limit exceeded|Metaspace)`)
	reserveRe = regexp.MustCompile(`Could not reserve enough space for .*object heap|There is insufficient memory for the Java Runtime Environment to continue|Invalid maximum heap size|Native memory allocation \(\w+\) failed`)
)

func ruleMemory(c *ruleCtx) *Finding {
	if idx := c.find(reserveRe); len(idx) > 0 {
		f := &Finding{Rule: "out-of-memory", Title: "内存不够", Evidence: c.evidence(idx)}
		f.Summary = "最大内存设得太大，电脑上的可用内存不够，Java 虚拟机没能启动"
		if s := c.suggestMB(false); s > 0 {
			f.Fixes = []string{fmt.Sprintf("把最大内存降到 %d MB 左右（现在是 %d MB，本机可用约 %d MB）", s, c.in.XmxMB, c.in.AvailMB)}
		} else {
			f.Fixes = []string{"在「内存 / 启动」里把最大内存调小，比本机可用内存少 1–2 GB"}
		}
		f.Fixes = append(f.Fixes, "关掉占内存的程序（浏览器、游戏客户端）再启动")
		return f
	}
	idx := c.find(oomRe)
	if len(idx) == 0 {
		return nil
	}
	kind := oomRe.FindStringSubmatch(c.ls[idx[0]].text)[1]
	f := &Finding{Rule: "out-of-memory", Title: "内存不够", Evidence: c.evidence(idx)}
	if kind == "Metaspace" {
		f.Summary = "类元数据空间（Metaspace）用完了：模组太多，或者 JVM 参数里给 Metaspace 设了上限"
		f.Fixes = []string{"在「内存 / 启动」里去掉 -XX:MaxMetaspaceSize 这类限制", "同时适当调高最大内存"}
		return f
	}
	f.Summary = fmt.Sprintf("服务器运行中内存用完了（%s），最大内存设得太小", kind)
	if s := c.suggestMB(true); s > 0 {
		f.Fixes = []string{fmt.Sprintf("把最大内存调到 %d MB（现在是 %d MB，本机共 %d MB）", s, c.in.XmxMB, c.in.RAMMB)}
	} else {
		f.Fixes = []string{"在「内存 / 启动」里调高最大内存（模组服一般 6–8 GB）"}
	}
	f.Fixes = append(f.Fixes, "装了很多模组或者在跑大范围地形生成的话，再预留一些")
	return f
}

// suggestMB 按本机内存算一个建议的最大内存（MB，取整到 1024）。up 为 true 表示要调高，否则调低。
// 不知道本机内存时返回 0。
func (c *ruleCtx) suggestMB(up bool) int {
	if up {
		if c.in.RAMMB <= 0 {
			return 0
		}
		limit := c.in.RAMMB - 2048 // 给系统留 2 GB
		want := max(c.in.XmxMB+2048, c.in.XmxMB*3/2)
		s := min(want, limit) / 1024 * 1024
		if s <= c.in.XmxMB || s < 1024 {
			return 0 // 本机已经给不了更多
		}
		return s
	}
	if c.in.AvailMB <= 0 {
		return 0
	}
	s := (c.in.AvailMB - 1024) / 1024 * 1024
	if s < 1024 {
		return 1024
	}
	return s
}

// ---------- 10 卡死被看门狗关掉 ----------

var (
	tickRe     = regexp.MustCompile(`A single server tick took ([\d.]+) seconds \(should be max 0\.05\)`)
	watchdogRe = regexp.MustCompile(`Considering it to be crashed, server will forcibly shutdown`)
)

func ruleWatchdog(c *ruleCtx) *Finding {
	idx := append(c.find(tickRe), c.find(watchdogRe)...)
	if len(idx) == 0 {
		return nil
	}
	secs := ""
	for _, i := range c.find(tickRe) {
		secs = tickRe.FindStringSubmatch(c.ls[i].text)[1]
	}
	f := &Finding{Rule: "watchdog", Title: "卡死被看门狗关掉", Evidence: c.evidence(idx)}
	if secs != "" {
		f.Summary = fmt.Sprintf("服务器的一次 tick 卡了 %s 秒，看门狗认为它死机了，把服务器强制关掉了", secs)
	} else {
		f.Summary = "服务器卡住太久，看门狗认为它死机了，把服务器强制关掉了"
	}
	f.Fixes = []string{
		"看崩溃报告里「Server thread」的堆栈，找出卡住主线程的模组，更新或禁用它",
		"生成新地形太重时，可以先预生成区块，或减小视距（view-distance）",
		"最后的办法：在 server.properties 里把 max-tick-time 设成 -1（关掉看门狗；卡住时不会自动重启）",
	}
	return f
}

// ---------- 11 没同意 EULA ----------

var eulaRe = regexp.MustCompile(`You need to agree to the EULA in order to run the server`)

func ruleEULA(c *ruleCtx) *Finding {
	idx := c.find(eulaRe)
	if len(idx) == 0 {
		return nil
	}
	return &Finding{Rule: "eula", Title: "没同意 EULA", Evidence: c.evidence(idx),
		Summary: "服务器要求先同意 Minecraft EULA（最终用户许可协议）才能运行",
		Fixes: []string{"请阅读 https://aka.ms/MinecraftEULA ；若同意协议，请手动将服务器文件夹下 eula.txt 中的 eula=false 改为 eula=true 后重新启动",
			"这一步只能由你自己决定，AI 不能替你同意"}}
}

// ---------- 12 好友进不来（模组对不上） ----------

var (
	channelRe  = regexp.MustCompile(`[Cc]hannel of mod "?([^"\s]+?)"? failed to connect: (.+)`)
	vanillaRe  = regexp.MustCompile(`You are trying to connect to a server that is running (NeoForge|Forge), but you are not`)
	voiceRe    = regexp.MustCompile(`has incompatible voice chat version \(server=(\S+), client=(\S+)\)`)
	mismatchRe = regexp.MustCompile(`mismatched mod channel list|This server has mods that require (?:Forge|NeoForge) to be installed on the client|Mod rejections`)
	lostRe     = regexp.MustCompile(`^(\S+?) (?:\(/[^)]*\) )?lost connection: `)
	// 登录阶段断开时（Forge 1.20.1 等）日志里的「玩家名」是 GameProfile 对象的 toString：
	// com.mojang.authlib.GameProfile@29746d4f[id=<null>,name=TesterA,properties={},legacy=false]
	profileRe = regexp.MustCompile(`GameProfile@?[0-9a-fA-F]*\[.*?\bname=([^,\]\s]+)`)
)

// playerName 从断开行里取出的「玩家名」：GameProfile 那种写法只留 name（测试报告 E-06：横幅把整个对象打了出来）；
// 取不出名字（name=<null>）时返回空。普通玩家名原样返回。
func playerName(s string) string {
	if !strings.Contains(s, "GameProfile") {
		return s
	}
	m := profileRe.FindStringSubmatch(s)
	if m == nil || m[1] == "<null>" || m[1] == "null" {
		return ""
	}
	return m[1]
}

func ruleClientMismatch(c *ruleCtx) *Finding {
	var idx []int
	var mods, details, players []string
	player := func(i int) {
		for j := i; j >= 0 && j > i-3; j-- {
			if m := lostRe.FindStringSubmatch(bodyOf(c.ls[j].text)); m != nil {
				if p := playerName(m[1]); p != "" {
					players = append(players, p)
				}
				return
			}
		}
	}
	for _, i := range c.find(channelRe) {
		m := channelRe.FindStringSubmatch(c.ls[i].text)
		idx, mods = append(idx, i), append(mods, m[1])
		details = append(details, fmt.Sprintf("%s：%s", m[1], strings.TrimSpace(m[2])))
		player(i)
	}
	for _, i := range c.find(vanillaRe) {
		idx = append(idx, i)
		details = append(details, "有人用没装加载器的原版客户端连了模组服")
		player(i)
	}
	for _, i := range c.find(voiceRe) {
		m := voiceRe.FindStringSubmatch(c.ls[i].text)
		idx, mods = append(idx, i), append(mods, "voicechat")
		details = append(details, fmt.Sprintf("Simple Voice Chat 的版本对不上（服务端兼容号 %s，客户端 %s）", m[1], m[2]))
	}
	for _, i := range c.find(mismatchRe) {
		idx = append(idx, i)
		details = append(details, "客户端和服务端装的模组对不上")
		player(i)
	}
	if len(idx) == 0 {
		return nil
	}
	f := &Finding{Rule: "client-mismatch", Title: "好友进不来（模组对不上）", Severity: "warn", Mods: uniq(mods), Details: uniq(details), Evidence: c.evidence(idx)}
	who := "有好友"
	if ps := uniq(players); len(ps) > 0 {
		who = strings.Join(ps, "、")
	}
	f.Summary = who + "连服时因为客户端和服务端的模组对不上被断开了"
	f.Fixes = []string{"让好友把整合包（或模组）更新到和服务端一致，加载器也要一样（NeoForge / Forge）", "服务端最近加了模组的，把新模组也发给好友装上"}
	if contains(f.Mods, "voicechat") {
		f.Fixes = append(f.Fixes, "Simple Voice Chat：服务端和每个人的客户端要装兼容号一样的版本")
	}
	return f
}

// bodyOf 去掉日志前缀，只留正文。
func bodyOf(text string) string {
	for _, sep := range []string{"]: "} {
		if i := strings.Index(text, sep); i >= 0 {
			return text[i+len(sep):]
		}
	}
	return text
}

// ---------- 13 配置文件被改坏 ----------

var (
	cfgFailRe  = regexp.MustCompile(`Failed loading config file (\S+?) of type (\w+) for modid (\S+)|Failed to load config(?:uration)? file (\S+)|Error loading config(?:uration)? file (\S+)`)
	cfgParseRe = regexp.MustCompile(`nightconfig\.core\.io\.ParsingException|ParsingException: (Invalid|Not enough data|Unexpected)|Invalid TOML`)
	cfgPathRe  = regexp.MustCompile(`((?:config|defaultconfigs|serverconfig)[\\/][\w.\-\\/]+\.(?:toml|json5?|cfg|properties|yml|yaml|txt|snbt))`)
)

func ruleBrokenConfig(c *ruleCtx) *Finding {
	idx := append(c.find(cfgFailRe), c.find(cfgParseRe)...)
	if len(idx) == 0 {
		return nil
	}
	var files, mods []string
	for _, i := range idx {
		t := c.ls[i].text
		m := cfgFailRe.FindStringSubmatch(t)
		if m != nil && m[3] != "" {
			mods = append(mods, m[3])
		}
		if p := cfgPathRe.FindStringSubmatch(t); p != nil {
			files = append(files, toSlash(p[1]))
		} else if m != nil { // 只有文件名（如 Forge 的 create-common.toml）
			files = append(files, baseName(m[1]+m[4]+m[5]))
		}
	}
	f := &Finding{Rule: "broken-config", Title: "配置文件被改坏", Mods: uniq(mods), Evidence: c.evidence(idx)}
	f.Details = uniq(files) // 这里是配置文件的相对路径，不是 jar
	if len(f.Details) > 0 {
		f.Summary = "模组的配置文件格式不对，读不进去：" + strings.Join(f.Details, "、")
	} else {
		f.Summary = "有模组的配置文件格式不对，读不进去"
	}
	f.Fixes = []string{"先备份这个配置文件，再删掉它（或恢复默认），让模组下次启动时重新生成", "改过它的话，检查有没有少了引号、括号或等号"}
	return f
}

// baseName、toSlash 和 filepath 的同名函数一样，只是 / 和 \ 都当作分隔符（日志里是 Windows 路径，测试在 Linux 上跑）。
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func toSlash(p string) string { return strings.ReplaceAll(p, `\`, "/") }

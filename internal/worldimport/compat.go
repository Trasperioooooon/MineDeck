package worldimport

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
)

// 兼容检查和布局映射：地图（候选）× 目标服务器（核心、版本、level-name）→ 关系、摆放方式、检查结果。
// 只做 docs/地图导入-设计.md 里写明有依据的几种挪动；资料没写、样本没验证过的组合拦下，不猜。

// firstModern 第一个用 26.1 新目录布局的版本（Mojang：26.1 快照 6 起）。清单里查不到这个名字时，退到 26.1 正式版。
var firstModern = []string{"26.1-snapshot-6", "26.1"}

// Target 目标服务器的现状。
type Target struct {
	Inst      string
	New       bool   // 新建服务器（目录还没有世界）
	Dir       string // 实例目录；新建时为空
	Core      mcsrc.Core
	MC        string
	LevelName string
	Props     map[string]string // server.properties（按 Java 的读法）；新建时是将要写的值
	jarWV     int32             // 目标核心 jar 里 version.json 的 world_version（读不到为 0）
	launch    string            // 实例的启动方式（jar:… / args:…）
}

// coreFamily 核心属于哪一类：决定世界怎么摆、模组线索怎么比。
func coreFamily(c mcsrc.Core) string {
	switch c {
	case mcsrc.CorePaper, mcsrc.CorePurpur, mcsrc.CoreLeaves, mcsrc.CoreFolia:
		return "bukkit"
	case mcsrc.CoreMohist:
		return "forge+bukkit"
	case mcsrc.CoreBanner:
		return "fabric+bukkit"
	case mcsrc.CoreForge:
		return "forge"
	case mcsrc.CoreNeoForge:
		return "neoforge"
	case mcsrc.CoreFabric:
		return "fabric"
	case mcsrc.CoreVelocity, mcsrc.CoreWaterfall:
		return "proxy"
	}
	return "vanilla"
}

func bukkitLike(fam string) bool { return strings.Contains(fam, "bukkit") }

// versionOrder 版本先后：a 比 b 新返回 1，相同 0，旧 -1；ok=false 表示判断不了（版本名不在官方清单里）。
type versionOrder func(ctx context.Context, a, b string) (int, bool)

// manifestOrder 用 Mojang 版本清单的发布顺序比（mcsrc 会缓存清单，联网失败时用磁盘上的那份）。
func manifestOrder(ctx context.Context, a, b string) (int, bool) {
	if a == b {
		return 0, true
	}
	ka, err1 := mcsrc.KnownVersion(ctx, a)
	kb, err2 := mcsrc.KnownVersion(ctx, b)
	if err1 != nil || err2 != nil || !ka || !kb {
		return 0, false
	}
	ge, err := mcsrc.NewerOrEqual(ctx, a, b)
	if err != nil {
		return 0, false
	}
	if ge {
		return 1, true
	}
	return -1, true
}

// isModern 目标版本用不用 26.1 起的新布局。ok=false：判断不了。
func isModern(ctx context.Context, order versionOrder, mc string) (modern, ok bool) {
	for _, fm := range firstModern {
		if c, ok := order(ctx, mc, fm); ok {
			return c >= 0, true
		}
	}
	return false, false
}

// relation 地图最后保存的版本和目标版本的关系。
func relation(ctx context.Context, order versionOrder, m LevelMeta, t *Target) (Relation, []Issue) {
	var is []Issue
	if !m.HasVersion || m.VersionName == "" {
		// 15w32a（1.9 快照）以前的存档没有 Version 这一节
		if m.DataVersion == 0 {
			if c, ok := order(ctx, t.MC, "1.9"); ok && c >= 0 {
				is = append(is, Issue{Sev: SevConfirm, Code: "very-old",
					Msg: "这张地图来自 1.9 以前的很老的版本，存档里没有记版本号。第一次启动时游戏会把它升级成 " + t.MC + " 的格式，老地图的机关、命令可能不再好用"})
				return RelOlder, is
			}
		}
		is = append(is, Issue{Sev: SevConfirm, Code: "version-unknown",
			Msg: "存档里没有记最后保存的版本，没法确认能不能在 " + t.MC + " 上安全打开"})
		return RelUnknown, is
	}
	name := m.VersionName
	c, ok := order(ctx, name, t.MC)
	if !ok {
		// 清单里没有这个版本名：用数据版本号兜一下（目标 jar 里读得到的话）
		if t.jarWV > 0 && m.DataVersion > 0 {
			switch {
			case m.DataVersion > t.jarWV:
				c, ok = 1, true
			case m.DataVersion < t.jarWV:
				c, ok = -1, true
			default:
				c, ok = 0, true
			}
		}
	}
	if !ok {
		is = append(is, Issue{Sev: SevConfirm, Code: "version-unknown",
			Msg:    "认不出这张地图最后保存的版本「" + name + "」，没法确认能不能在 " + t.MC + " 上安全打开",
			Detail: fmt.Sprintf("Version.Name=%q DataVersion=%d", name, m.DataVersion)})
		return RelUnknown, is
	}
	if c == 0 && t.jarWV > 0 && m.DataVersion > 0 && m.DataVersion != t.jarWV {
		is = append(is, Issue{Sev: SevConfirm, Code: "dataversion-mismatch",
			Msg:    "存档写的版本和服务器一样，但数据版本号对不上，可能是改过的存档或特殊版本",
			Detail: fmt.Sprintf("存档 %d，服务器 %d", m.DataVersion, t.jarWV)})
	}
	switch {
	case c > 0:
		is = append(is, Issue{Sev: SevBlock, Code: "newer",
			Msg: "这份存档来自更新的版本（" + name + "），当前服务器（" + t.MC + "）不能安全读取。请新建一台 " + name + " 的服务器来用这张地图；不要把存档改成旧版本号去硬开"})
		return RelNewer, is
	case c < 0:
		is = append(is, Issue{Sev: SevConfirm, Code: "older",
			Msg: "这张地图最后保存于 " + name + "，比服务器的 " + t.MC + " 旧。第一次启动时游戏会把导入的这份副本升级成新格式，升级以后不能再给旧版本用；原来的文件不受影响。老地图的机关、命令、数据包在新版本里不一定还好用。想原汁原味地玩，请新建一台 " + name + " 的服务器"})
		return RelOlder, is
	}
	return RelSame, is
}

// mapping 按目标核心和版本，算出暂存时每个来源目录放到哪（相对实例目录），以及额外要挪的文件。
type mapping struct {
	moves  []Move   // 前缀映射：来源里的 From（目录或文件）→ 实例里的 To
	drop   []string // 来源里属于这张地图、但不带过去的目录前缀（旧 Bukkit 维度目录里的 level.dat、uid.dat 等）
	layout Layout   // 摆好以后的布局（第一次启动前）
	notes  []Issue
}

func planMapping(c *Candidate, fam string, modernTarget bool, level string) (mapping, []Issue) {
	var mp mapping
	var is []Issue
	L := level
	switch c.Layout {
	case LayoutLegacy:
		mp.moves = []Move{{From: c.Root, To: L}}
		mp.layout = LayoutLegacy
		if bukkitLike(fam) && hasDim(c, "minecraft:the_nether", "minecraft:the_end") {
			mp.notes = append(mp.notes, Issue{Sev: SevInfo, Code: "bukkit-migrates",
				Msg: "这是原版布局的地图。目标是 Paper 一类核心，第一次启动时由核心自己把下界、末地搬到它的目录里（Paper 官方文档：从原版换到 Paper 直接启动即可）"})
		}
	case LayoutLegacyBukkit:
		if bukkitLike(fam) && !modernTarget {
			mp.moves = []Move{{From: c.Root, To: L}}
			for _, g := range c.Group {
				suf := "_the_end"
				if strings.HasSuffix(strings.ToLower(g), "_nether") {
					suf = "_nether"
				}
				mp.moves = append(mp.moves, Move{From: g, To: L + suf})
			}
			mp.layout = LayoutLegacyBukkit
		} else {
			// 按 Paper 文档的「Paper → 原版（26.1 以前）」：_nether/DIM-1、_the_end/DIM1 并进主目录，两个外层目录不要了
			mp.moves = []Move{{From: c.Root, To: L}}
			for _, g := range c.Group {
				low := strings.ToLower(g)
				switch {
				case strings.HasSuffix(low, "_nether"):
					mp.moves = append(mp.moves, Move{From: g + "/DIM-1", To: L + "/DIM-1"})
				case strings.HasSuffix(low, "_the_end"):
					mp.moves = append(mp.moves, Move{From: g + "/DIM1", To: L + "/DIM1"})
				}
				mp.drop = append(mp.drop, g)
			}
			mp.layout = LayoutLegacy
			mp.notes = append(mp.notes, Issue{Sev: SevInfo, Code: "bukkit-to-vanilla",
				Msg: "这张地图来自旧 Paper / Spigot 服务器，下界和末地分开放着。导入时按 Paper 官方迁移文档把它们并进主世界文件夹（_nether、_the_end 里的服务器专用文件不带过去）"})
			if modernTarget && bukkitLike(fam) {
				mp.notes = append(mp.notes, Issue{Sev: SevInfo, Code: "bukkit-via-vanilla",
					Msg: "Paper 26.1 起不能直接读旧 Bukkit 布局（官方文档要求先转成原版布局），所以先按原版布局放好，第一次启动时由 Paper 自己升级"})
			}
		}
	case LayoutModern:
		mp.moves = []Move{{From: c.Root, To: L}}
		mp.layout = LayoutModern
	case LayoutModernPaper:
		mp.moves = []Move{{From: c.Root, To: L}}
		mp.layout = LayoutModernPaper
		if !bukkitLike(fam) {
			// Paper 文档「Paper → 原版（26.1 起）」：五个共用数据文件从主世界目录挪回 data/minecraft/
			for _, f := range sharedDataFiles {
				mp.moves = append(mp.moves, Move{
					From: join(c.Root, "dimensions/minecraft/overworld/data/minecraft/"+f),
					To:   L + "/data/minecraft/" + f})
			}
			mp.layout = LayoutModern
			mp.notes = append(mp.notes, Issue{Sev: SevInfo, Code: "paper-to-vanilla",
				Msg: "这张地图来自 Paper 26.1 以后的服务器。导入时按 Paper 官方迁移文档把天气、游戏规则等五份共用数据挪回原版的位置"})
		}
	default:
		is = append(is, Issue{Sev: SevBlock, Code: "layout-unknown", Msg: "认不出这张地图的目录布局"})
	}
	// 布局和目标版本对不上：新布局放进 26.1 以前的服务器（版本比较一般已经拦下，这里兜一道）
	if (c.Layout == LayoutModern || c.Layout == LayoutModernPaper) && !modernTarget {
		is = append(is, Issue{Sev: SevBlock, Code: "modern-to-old",
			Msg: "这张地图是 26.1 以后的新存档格式，当前服务器的版本读不了它。请新建一台 26.1 或更新版本的服务器"})
	}
	return mp, is
}

func hasDim(c *Candidate, ids ...string) bool {
	for _, d := range c.Dimensions {
		for _, id := range ids {
			if d.ID == id {
				return true
			}
		}
	}
	return false
}

// sourceLoaders 存档里留下的加载器线索。
func sourceLoaders(c *Candidate) (loaders []string, modded bool, detail []string) {
	add := func(l string) {
		for _, x := range loaders {
			if x == l {
				return
			}
		}
		loaders = append(loaders, l)
	}
	for _, b := range c.Meta.ServerBrands {
		switch low := strings.ToLower(b); {
		case strings.Contains(low, "neoforge"):
			add("neoforge")
		case strings.Contains(low, "forge"), strings.Contains(low, "fml"):
			add("forge")
		case strings.Contains(low, "fabric"):
			add("fabric")
		case strings.Contains(low, "quilt"):
			add("quilt")
		case low == "vanilla":
		default:
			add("bukkit:" + low) // paper、spigot、purpur…：插件服的核心，本身不改世界格式
		}
	}
	for _, mk := range c.Meta.ModMarkers {
		switch strings.ToLower(strings.TrimPrefix(mk, "Data.")) {
		case "neoforge":
			add("neoforge")
		case "fml", "forge":
			add("forge")
		case "fabric":
			add("fabric")
		case "quilt":
			add("quilt")
		}
	}
	for _, l := range loaders {
		if !strings.HasPrefix(l, "bukkit:") {
			modded = true
		}
	}
	if c.Meta.WasModded {
		detail = append(detail, "WasModded=1")
	}
	if len(c.Meta.ServerBrands) > 0 {
		detail = append(detail, "ServerBrands="+strings.Join(c.Meta.ServerBrands, ","))
	}
	for _, d := range c.Dimensions {
		if !strings.HasPrefix(d.ID, "minecraft:") {
			detail = append(detail, "维度 "+d.ID)
		}
	}
	return loaders, modded, detail
}

// modChecks 模组线索 × 目标核心。
func modChecks(c *Candidate, fam string) []Issue {
	loaders, modded, detail := sourceLoaders(c)
	var is []Issue
	var custom []string
	for _, d := range c.Dimensions {
		if !strings.HasPrefix(d.ID, "minecraft:") {
			custom = append(custom, d.ID)
		}
	}
	pluginCore := false // Paper、Spigot 这类插件服的核心也会把 WasModded 记成 1，它们不改世界格式
	for _, l := range loaders {
		if strings.HasPrefix(l, "bukkit:") {
			pluginCore = true
		}
	}
	if !modded {
		switch {
		case c.Meta.WasModded && !pluginCore:
			is = append(is, Issue{Sev: SevWarn, Code: "was-modded",
				Msg:    "存档记着它被改版的游戏或服务端打开过，但没有留下是哪一种。如果原来用过模组，请在服务器上装好同样的模组；没用过可以不管",
				Detail: strings.Join(detail, "；")})
		case len(custom) > 0:
			is = append(is, Issue{Sev: SevWarn, Code: "custom-dims",
				Msg:    "地图里有自定义维度（" + strings.Join(firstN(custom, 4), "、") + "）。来自数据包的会跟着地图走；来自模组的需要装上同样的模组",
				Detail: strings.Join(detail, "；")})
		}
		return is
	}
	var want []string
	for _, l := range loaders {
		if !strings.HasPrefix(l, "bukkit:") {
			want = append(want, l)
		}
	}
	has := func(l string) bool {
		switch l {
		case "forge":
			return strings.Contains(fam, "forge") && fam != "neoforge"
		case "neoforge":
			return fam == "neoforge"
		case "fabric", "quilt":
			return strings.Contains(fam, "fabric")
		}
		return false
	}
	match := false
	for _, l := range want {
		if has(l) {
			match = true
		}
	}
	names := strings.Join(want, " / ")
	if match {
		is = append(is, Issue{Sev: SevWarn, Code: "mods-needed",
			Msg:    "这张地图用过 " + names + " 的模组。世界里没有完整的模组清单，MineDeck 不能自动补齐：请在这台服务器上装好和原来一样的模组（版本也要对得上），否则模组的方块、物品、维度会丢失",
			Detail: strings.Join(detail, "；")})
		return is
	}
	is = append(is, Issue{Sev: SevConfirm, Code: "mods-mismatch",
		Msg:    "这张地图用过 " + names + " 的模组，而目标服务器的核心不能加载这些模组。直接打开时，模组添加的方块、物品、维度会丢失或报错，而且会被写回存档。建议新建一台对应核心的服务器并装好同样的模组",
		Detail: strings.Join(detail, "；")})
	return is
}

// jarWorldVersion 读核心 jar 里 version.json 的 world_version（原版和一部分核心有）。读不到返回 0。
func jarWorldVersion(dir, launchTarget string) int32 {
	jar, ok := strings.CutPrefix(launchTarget, "jar:")
	if !ok || jar == "" || strings.ContainsAny(jar, `/\:`) {
		return 0
	}
	zr, err := zip.OpenReader(filepath.Join(dir, jar))
	if err != nil {
		return 0
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "version.json" || f.UncompressedSize64 > 64<<10 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return 0
		}
		b, _ := io.ReadAll(io.LimitReader(rc, 64<<10))
		rc.Close()
		var v struct {
			WorldVersion int32 `json:"world_version"`
		}
		if json.Unmarshal(b, &v) == nil {
			return v.WorldVersion
		}
	}
	return 0
}

var worldContainerRe = regexp.MustCompile(`(?m)^[ \t]+world-container:[ \t]*['"]?([^'"\r\n#]*)`)

// worldRootChecks 目标的世界根是不是就在服务器文件夹里、level-name 能不能照着用。
func worldRootChecks(t *Target) []Issue {
	var is []Issue
	if t.Dir == "" {
		return is
	}
	if name, ok := inst.CheckLevelName(t.Dir); !ok {
		is = append(is, Issue{Sev: SevBlock, Code: "level-name-unsafe",
			Msg:    "这台服务器的 level-name 指到了服务器文件夹外面，或者写法有歧义，MineDeck 不能确定世界在哪。请先在「全部配置」里把 level-name 改成一个普通的名字",
			Detail: name})
	} else if first, _, _ := strings.Cut(t.LevelName, "/"); sameOrInside(filepath.Join(t.Dir, filepath.FromSlash(first)), filepath.Join(t.Dir, ".minedeck")) {
		// 名字上已经拦了 .minedeck；这里按真实路径再看一遍，短文件名（MINEDE~1 这类）也指不进控制目录
		is = append(is, Issue{Sev: SevBlock, Code: "level-name-unsafe",
			Msg:    "这台服务器的 level-name 指到了 MineDeck 自己的控制文件夹，请先在「全部配置」里把 level-name 改成一个普通的名字",
			Detail: t.LevelName})
	}
	if bukkitLike(coreFamily(t.Core)) {
		if b, err := os.ReadFile(filepath.Join(t.Dir, "bukkit.yml")); err == nil {
			if m := worldContainerRe.FindSubmatch(b); m != nil {
				if v := strings.TrimSpace(string(m[1])); v != "" && v != "." && v != "./" {
					is = append(is, Issue{Sev: SevBlock, Code: "world-container",
						Msg:    "这台服务器在 bukkit.yml 里把世界放到了别的文件夹（world-container），首版导入不支持这种摆法",
						Detail: "settings.world-container: " + v})
				}
			}
		}
	}
	// 启动参数文件（Forge / NeoForge 的 win_args.txt 这类）里改了世界根：原版认 --universe、--world，
	// Bukkit 一类还认 -W、--world-dir、--world-container。MineDeck 自己启动时不加这些，有人手改过就拦下
	if rest, ok := strings.CutPrefix(t.launch, "args:"); ok && rest != "" {
		if b, err := os.ReadFile(filepath.Join(t.Dir, filepath.FromSlash(rest))); err == nil {
			for _, f := range strings.Fields(string(b)) {
				switch strings.ToLower(strings.SplitN(f, "=", 2)[0]) {
				case "--universe", "--world", "-w", "--world-dir", "--world-container":
					is = append(is, Issue{Sev: SevBlock, Code: "world-root-arg",
						Msg:    "这台服务器的启动参数改了世界的位置，首版导入不支持这种摆法",
						Detail: rest + "：" + f})
				}
			}
		}
	}
	// level-name 的每一段都不能是链接、目录联接：切换靠改名，跨到别的盘上改名会失败，跨出去更不行
	cur := t.Dir
	for _, seg := range strings.Split(filepath.ToSlash(t.LevelName), "/") {
		cur = filepath.Join(cur, seg)
		for _, p := range []string{cur, cur + "_nether", cur + "_the_end"} {
			if st, err := os.Lstat(p); err == nil && st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				is = append(is, Issue{Sev: SevBlock, Code: "world-is-link",
					Msg:    "这台服务器的世界文件夹是一个链接（符号链接或目录联接），首版导入不支持",
					Detail: p})
			}
		}
	}
	return is
}

// settingSuggestions 地图存档里的难度、模式、极限和服务器设置不一样时，列出来让用户选（不默认改）。
// 只列目标 server.properties 里已经有的键：没有的键说明这个版本不用它，不往里加。
func settingSuggestions(m LevelMeta, props map[string]string) []SettingChange {
	var out []SettingChange
	diffNames := []string{"peaceful", "easy", "normal", "hard"}
	modeNames := []string{"survival", "creative", "adventure", "spectator"}
	norm := func(v string, names []string) string {
		v = strings.ToLower(strings.TrimSpace(v))
		for k, n := range names {
			if v == fmt.Sprint(k) {
				return n
			}
		}
		return v
	}
	if cur, ok := props["difficulty"]; ok && m.HasDifficulty && m.Difficulty >= 0 && m.Difficulty < 4 {
		if want := diffNames[m.Difficulty]; norm(cur, diffNames) != want {
			out = append(out, SettingChange{Key: "difficulty", Before: cur, After: want,
				Why: "地图存档的难度是 " + want + "。服务器每次启动都以 server.properties 的难度为准"})
		}
	}
	if cur, ok := props["gamemode"]; ok && m.HasGameType && m.GameType >= 0 && m.GameType < 4 {
		if want := modeNames[m.GameType]; norm(cur, modeNames) != want {
			out = append(out, SettingChange{Key: "gamemode", Before: cur, After: want,
				Why: "地图存档的默认模式是 " + want + "（冒险地图常用 adventure）。服务器给新进服的玩家用 server.properties 里的模式"})
		}
	}
	if cur, ok := props["hardcore"]; ok && m.Hardcore && strings.ToLower(strings.TrimSpace(cur)) != "true" {
		out = append(out, SettingChange{Key: "hardcore", Before: cur, After: "true",
			Why: "这是一张极限模式的地图"})
	}
	return out
}

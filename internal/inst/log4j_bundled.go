package inst

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"minedeck/internal/zipguard"
)

// 服务端 jar 自带的 log4j 修没修过 log4shell。
//
// Mojang 给 1.7 ~ 1.16.5 的两份日志配置是照原版服务端写的。CraftBukkit 系（Spigot，以及 1.11.2 及以前的 Paper）启动时
// 会把 Console 类型的输出端摘掉，自己从名叫 TerminalConsole 的队列里取日志往控制台写；Mojang 的配置里没有这个队列，
// 套上以后标准输出在「Loading libraries」之后就没了，MineDeck 认不出启动完成（服务器其实起来了）。
// 2021 年 12 月以后的 Paper 构建把自带的 log4j 换成了修过的版本，不套 Mojang 的配置也连不出去
// （2026-11-18 实测 1.8.8-445、1.11.2-1106、1.12.2-1620、1.16.5-794，都是 2.17.0）。
//
// 所以：看得出实际运行的那个 jar 里 log4j-core 是修过的版本，就不套 Mojang 的配置；看得出没修过的、看不出来的，照旧套
// （防住了，CraftBukkit 系的控制台没输出，启动时在控制台里说明）。版本只认 jar 里 Maven 留下的记录，不猜：原版服务端、
// Forge、老构建的 Paper、Spigot 的 1.12 以后都没有这条记录，一律照旧。Spigot 修 log4shell 靠的是它自带的日志配置
// （%msg{nolookups}），log4j 本身还是 2.8.1，从 jar 上分不出修没修过，也照旧。
type log4jBundle int

const (
	log4jBundleUnknown log4jBundle = iota // 判断不了：没有版本记录、读不开、paperclip 还没生成过服务端……
	log4jBundleOld                        // 有版本记录，不是修过的版本
	log4jBundleFixed                      // 修过的版本：消息里的 ${…} 不再展开
)

const (
	// log4jCorePom log4j-core 打进 jar 时 Maven 留下的版本记录。
	log4jCorePom = "META-INF/maven/org.apache.logging.log4j/log4j-core/pom.properties"
	// termConsoleClass Paper 1.12 起的控制台实现：带着它的不靠 TerminalConsole 队列，套 Mojang 的配置控制台照常。
	termConsoleClass = "net/minecrell/terminalconsole/TerminalConsoleAppender.class"
	// log4jCoreClass log4j-core 本身打进了这个 jar 的凭据。只有版本记录、没有这个类的（比如别的启动器重新打包依赖时
	// 留下的记录，真正的 log4j 从 libraries 里另外加载），版本记录说明不了实际运行的是哪一版，不算修过（2026-11-19 Codex 复审）。
	log4jCoreClass = "org/apache/logging/log4j/core/LoggerContext.class"
)

// classifyLog4jCore 按 log4j-core 的版本号分。2.16.0 起「消息里的 ${…} 展开」整个去掉了，给 Java 7、Java 6 的回补版
// 2.12.2、2.3.1 起也一样；2.15.0 只是默认关掉、还能用配置打开，不算。带后缀的（2.0-beta9、-SNAPSHOT）不算修过的；
// 连主版本都读不出来的判断不了。
func classifyLog4jCore(ver string) log4jBundle {
	parts := strings.Split(strings.TrimSpace(ver), ".")
	if len(parts) < 2 || len(parts) > 4 {
		return log4jBundleUnknown
	}
	var n [3]int
	clean := true
	for k := 0; k < len(parts) && k < 3; k++ {
		digits := parts[k]
		if cut := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); cut >= 0 {
			digits, clean = digits[:cut], false
		}
		v, err := strconv.Atoi(digits)
		if err != nil { // 这一段不是数字开头（空的也算）
			return log4jBundleUnknown
		}
		n[k] = v
		if !clean {
			break
		}
	}
	if n[0] < 2 {
		return log4jBundleUnknown
	}
	fixed := n[0] >= 3 || n[1] >= 16 || (n[1] == 12 && n[2] >= 2) || (n[1] == 3 && n[2] >= 1)
	if fixed && clean && len(parts) <= 3 {
		return log4jBundleFixed
	}
	return log4jBundleOld
}

// paperclipPatch paperclip（Paper 系 1.17 以前的启动器）jar 里的补丁说明：启动时它把原版服务端打上补丁，
// 生成 cache 里的服务端 jar 再运行，每次启动都按 PatchedHash 核对那个 jar，对不上就重新生成。
type paperclipPatch struct {
	Version     string `json:"version"`
	PatchedHash string `json:"patchedHash"`
}

// serverJar 一个服务端 jar 里和日志有关的几样。
type serverJar struct {
	log4jCore   string         // log4j-core 的版本记录，没有是空串
	hasCore     bool           // jar 里真带着 log4j-core 的类（log4jCoreClass）
	craftBukkit bool           // 主类是 org.bukkit.craftbukkit.*（CraftBukkit、Spigot、Paper 生成的服务端）
	termConsole bool           // 带着 TerminalConsoleAppender
	clip        paperclipPatch // paperclip 的补丁说明；不是 paperclip 时是零值
}

// readServerJar 打开一个 jar 读出 serverJar。
func readServerJar(path string) (serverJar, error) {
	var j serverJar
	zr, err := zipguard.Open(path, zipguard.Default)
	if err != nil {
		return j, err
	}
	defer zr.Close()
	small := map[string][]byte{}
	for _, f := range zr.File {
		switch f.Name {
		case termConsoleClass:
			j.termConsole = true
		case log4jCoreClass:
			j.hasCore = true
		case log4jCorePom, "META-INF/MANIFEST.MF", "patch.properties", "patch.json":
			if rc, err := f.Open(); err == nil {
				b, err := io.ReadAll(io.LimitReader(rc, 64<<10))
				rc.Close()
				if err == nil { // 读到一半出错的不用：半截的记录可能读出错的版本
					small[f.Name] = b
				}
			}
		}
	}
	if b := small[log4jCorePom]; b != nil {
		j.log4jCore = propOf(b, "version")
	}
	j.craftBukkit = strings.HasPrefix(parseManifest(small["META-INF/MANIFEST.MF"])["Main-Class"], "org.bukkit.craftbukkit.")
	if b := small["patch.properties"]; b != nil { // 2020 年以后的 paperclip
		j.clip = paperclipPatch{Version: propOf(b, "version"), PatchedHash: propOf(b, "patchedHash")}
	} else if b := small["patch.json"]; b != nil { // 更早的
		_ = json.Unmarshal(b, &j.clip)
	}
	return j, nil
}

// runningServerJar 这台服务器实际运行的那个 jar。只看「jar:」启动的：启动的 jar 不是 paperclip 的就是它自己；
// 是 paperclip 的，看它在 cache 里生成的服务端 jar，而且要和 paperclip 记的哈希对得上（对不上说明换过服务端 jar
// 还没启动过，现在那个不是下次要运行的）。文件夹里有 paperclip.properties 的不看：它能改掉补丁说明。
// 读不出来、定不下来是哪一个时 ok=false。
func runningServerJar(dir, launchTarget string) (j serverJar, ok bool) {
	name, isJar := strings.CutPrefix(launchTarget, "jar:")
	if !isJar || name == "" || !filepath.IsLocal(filepath.FromSlash(name)) {
		return j, false
	}
	j, err := readServerJar(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		return j, false
	}
	if j.clip.PatchedHash == "" {
		return j, true
	}
	if _, err := os.Stat(filepath.Join(dir, "paperclip.properties")); err == nil {
		return j, false
	}
	cands := []string{"patched.jar"} // 2016 年的 paperclip
	if v := j.clip.Version; v != "" && len(v) <= 40 && !strings.ContainsFunc(v, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-')
	}) {
		cands = []string{"patched_" + v + ".jar", "patched.jar"}
	}
	for _, c := range cands {
		p := filepath.Join(dir, "cache", c)
		if !sha256Is(p, j.clip.PatchedHash) {
			continue
		}
		inner, err := readServerJar(p)
		return inner, err == nil
	}
	return j, false
}

// log4jJarView 整理 log4shell 防护时要知道的、关于服务端 jar 的几样。读不出来时都是零值，按「判断不了」办。
type log4jJarView struct {
	fixed bool   // 自带的 log4j-core 确认是修过的版本：不用套 Mojang 的日志配置
	ver   string // log4j-core 的版本记录（有的话）
	quiet bool   // CraftBukkit 系、不带 TerminalConsoleAppender：套上 Mojang 的配置以后控制台没有输出
	// clipPending 启动的是 paperclip，它生成的服务端还核对不了（cache 里没有、和补丁说明对不上、文件夹里有 paperclip.properties）：
	// 看不出是不是 quiet 的那一类，不替它补配置（见 log4jFixFor），等它开过一次再看（2026-11-19 Codex 复审第二轮）
	clipPending bool
}

func viewLog4jJar(dir, launchTarget string) log4jJarView {
	j, ok := runningServerJar(dir, launchTarget)
	if !ok {
		return log4jJarView{clipPending: j.clip.PatchedHash != ""}
	}
	return log4jJarView{
		fixed: j.log4jCore != "" && j.hasCore && classifyLog4jCore(j.log4jCore) == log4jBundleFixed,
		ver:   j.log4jCore,
		quiet: j.craftBukkit && !j.termConsole,
	}
}

// bundledLog4j 这台服务器实际运行的那个 jar 里，log4j-core 是什么版本、修没修过。
func bundledLog4j(dir, launchTarget string) (log4jBundle, string) {
	j, ok := runningServerJar(dir, launchTarget)
	if !ok || j.log4jCore == "" || !j.hasCore {
		return log4jBundleUnknown, ""
	}
	return classifyLog4jCore(j.log4jCore), j.log4jCore
}

// log4jSelfFixed 服务端自带的 log4j 是不是确认修过的（是的话不用再套 Mojang 的日志配置）。
func log4jSelfFixed(dir, launchTarget string) bool {
	return viewLog4jJar(dir, launchTarget).fixed
}

// sha256Is 文件的 SHA-256 是不是 want（十六进制，不分大小写）。读不了算不是。
func sha256Is(path, want string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), strings.TrimSpace(want))
}

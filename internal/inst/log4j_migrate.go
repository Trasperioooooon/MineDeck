package inst

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"minedeck/internal/atomicfile"
	"minedeck/internal/mcsrc"
)

// 已经建好的服务器，log4shell 防护和现在的做法对不上的，在这里整理。三种情况：
//
//   - 换：1.7 ~ 1.11.2 的服务器，v3.2.0 以前套的是 1.12 ~ 1.16.5 的那份日志配置（log4j2_112-116.xml）。它靠 %msg{nolookups}，
//     这段版本自带的 log4j 2.0-beta9 不认，等于没防。换成 Mojang 给这段版本的 log4j2_17-111.xml。
//   - 补：1.7 ~ 1.16.5 的服务器，启动参数里没有任何 -Dlog4j.configurationFile=（当初配置没下载成，只剩 noLookups，
//     对这段版本不起作用）。按版本下载对应的官方配置，补上引用。
//   - 摘：服务端自带的 log4j 看得出是修过的（见 log4j_bundled.go），却还引用着 Mojang 的配置。CraftBukkit 系套着它
//     控制台没有输出、MineDeck 认不出启动完成，又用不着它，把引用摘掉。
//
// 什么时候做：程序启动后在后台过一遍（MigrateLog4j）；启动服务器时再看一眼。
// 换和补要下载：先把文件下载好、核对过 SHA-1，再改启动参数，存 instance.json、重写 run.bat。下载不成什么都不动，照旧开：
// 引用一个不存在的配置文件，log4j 会退回只输出 ERROR 的默认配置，控制台和「启动完成」的判断都会失效。启动服务器时
// 不等下载：控制台说一声、后台去做，做成了下次启动生效。摘不用下载，启动服务器时当场做，这一次启动就生效。
// 只动 MineDeck 自己加的那两条引用，别的 JVM 参数一条不碰；用户自己引用了别的日志配置的不动。
// 不删任何文件：换下来的、摘掉引用的配置文件都留在服务器文件夹里。分不清用户自己的启动脚本（比如接管前那台服的 start.bat）
// 还用不用它，删了那边就只剩 ERROR 输出；启动脚本被占着、重写失败时，原来的 run.bat 也还引用着它（2026-11-18 Codex 复审）。

// log4jMigrating 正在整理的实例：程序启动时的那一遍和启动服务器时补的那一次，不会同时下载、同时改同一台。
var log4jMigrating sync.Map // *Instance → struct{}

// log4jMigrateTimeout 一台服务器整理时下载最多等多久（测试里改短）。
var log4jMigrateTimeout = 2 * time.Minute

// log4jFix 一台服务器的 log4shell 防护要怎么整理。
type log4jFix int

const (
	log4jFixNone log4jFix = iota
	log4jFixSwap          // 1.7 ~ 1.11.2 引用着 112-116 那份：换成 17-111
	log4jFixAdd           // 该套官方配置，却没有任何 -Dlog4j.configurationFile=：下载、补上
	log4jFixDrop          // 服务端自带的 log4j 修过了，还引用着 Mojang 的配置：摘掉
)

// log4jFixOf 按启动参数和「服务端自带的 log4j 修没修过」决定怎么整理；xml 是换、补时要用的那份官方配置。
// 只看本地清单，判断不了版本的不动；代理端不管；用不着官方配置的版本不动。
func log4jFixOf(s Settings, selfFixed bool) (fix log4jFix, xml string) {
	if mcsrc.KindOf(s.Core) == mcsrc.KindProxy {
		return log4jFixNone, ""
	}
	_, xml, ok := log4jPlanCached(s.MC)
	if !ok || xml == "" {
		return log4jFixNone, ""
	}
	if selfFixed {
		if hasOurLog4jConfig(s.ExtraJVM) {
			return log4jFixDrop, ""
		}
		return log4jFixNone, ""
	}
	switch {
	case !slices.ContainsFunc(s.ExtraJVM, func(a string) bool { return strings.HasPrefix(a, log4jConfigKey) }):
		return log4jFixAdd, xml
	case xml == log4jXML17 && slices.Contains(s.ExtraJVM, log4jConfig):
		return log4jFixSwap, xml
	}
	return log4jFixNone, "" // 已经是该用的那份，或者用户自己引用了别的日志配置
}

// log4jFixFor 同 log4jFixOf，自己去看服务端 jar（要读文件，paperclip 的还要算一遍哈希；用不着官方配置的版本不看）。
// 看到的结果一起带回来：改参数之前拿着锁再判断一遍时用同一个结论，不重读一遍 jar。
func log4jFixFor(s Settings, dir string) (fix log4jFix, xml string, jar log4jJarView) {
	if mcsrc.KindOf(s.Core) == mcsrc.KindProxy {
		return log4jFixNone, "", jar
	}
	if _, x, ok := log4jPlanCached(s.MC); !ok || x == "" {
		return log4jFixNone, "", jar
	}
	jar = viewLog4jJar(dir, s.LaunchTarget)
	fix, xml = log4jFixOf(s, jar.fixed)
	if fix == log4jFixAdd && (jar.quiet || jar.clipPending) {
		// CraftBukkit 系（Spigot、老构建的 Paper）套上 Mojang 的配置以后控制台没有输出，MineDeck 一直显示启动中，
		// 热备份、定时命令和定时重启都跟着不做了；v3.2.2 不替这类服补，这里照旧不补，启动时提醒（log4jQuietBare）。
		// 要不要给它们另做一份带控制台队列的配置，等作者定（2026-11-19 Codex 复审）。
		// paperclip 生成的服务端还核对不了的（cache 缺了、和补丁说明对不上）看不出是不是这一类，也先不补，等它开过一次再看。
		return log4jFixNone, "", jar
	}
	return fix, xml, jar
}

// log4jQuietBare 按版本该套 Mojang 的配置、启动参数里却没有任何日志配置引用的 CraftBukkit 系服务端（看不出自带的 log4j 修没修过）：
// 套上以后控制台没有输出，所以不替它补（见 log4jFixFor），只在启动时提醒。
func log4jQuietBare(s Settings, jar log4jJarView) bool {
	if !jar.quiet || jar.fixed {
		return false
	}
	fix, _ := log4jFixOf(s, false)
	return fix == log4jFixAdd
}

// log4jQuietBareNote 上面那类服务器启动时说的那一句。
const log4jQuietBareNote = "[MineDeck] ⚠ log4shell 防护：这台服务器的启动参数里没有 Mojang 的日志配置。Spigot 自带的日志配置一般防得住，" +
	"老构建的 Paper 防不住；Mojang 的那份套上以后，这类服务端的控制台没有输出，MineDeck 会一直显示启动中、定时任务也跟着不做，" +
	"所以没有替它补上。最稳妥的是换成修过 log4shell 的构建（Paper 用这个版本最新的构建），在那之前先别让不认识的人进服"

// log4jXMLOnDisk 服务器文件夹里这份官方配置的情况：在不在，是不是官方的那一份（按 SHA-1 核对）。
// 读不了的、不是普通文件的（链接，包括指向的东西已经不在了的）算在、但不是官方的：不覆盖，也不引用。
func log4jXMLOnDisk(dir, name string) (exists, official bool) {
	p := filepath.Join(dir, name)
	st, err := os.Lstat(p)
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist), false
	}
	if !st.Mode().IsRegular() {
		return true, false
	}
	f, err := os.Open(p)
	if err != nil {
		return true, false
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return true, false
	}
	sum, ok := log4jXMLSHA1[name]
	return true, ok && strings.EqualFold(hex.EncodeToString(h.Sum(nil)), sum)
}

// log4jXMLConflictParts 要补、要换上的配置文件名已经被一份不是官方的文件占着时的标题和正文（不带 ⚠ 前缀）。
func log4jXMLConflictParts(mc, xml string, fix log4jFix) (title, body string) {
	what, act := "没有套上 Mojang 的日志配置", "补上引用"
	if fix == log4jFixSwap {
		what, act = "还引用着 1.12 ~ 1.16.5 的那份日志配置，对这个版本不起作用", "换上引用"
	}
	title = fmt.Sprintf("log4shell 防护：这台 %s 的服务器%s，等于没防", mc, what)
	body = fmt.Sprintf("服务器文件夹里已经有一份 %s，内容和官方的不一样（可能是自己改过的），MineDeck 没有覆盖它，也没有替它%s。"+
		"要用官方的那份，把这个文件改名或挪走以后重开 MineDeck", xml, act)
	return title, body
}

// log4jXMLConflictNote 要补、要换上的配置文件名已经被一份不是官方的文件占着时说的那一句（不带 [MineDeck] 前缀）。
func log4jXMLConflictNote(mc, xml string, fix log4jFix) string {
	title, body := log4jXMLConflictParts(mc, xml, fix)
	return "⚠ " + title + "。" + body
}

// log4jQuietNote CraftBukkit 系套着 Mojang 的配置时，控制台里说明为什么没有输出（不带 [MineDeck] 前缀）。
const log4jQuietNote = "Spigot 和老构建的 Paper 套上 Mojang 的这份日志配置以后，控制台不会有输出，MineDeck 这边会一直显示启动中；" +
	"服务器其实照常在开，输出看服务器文件夹里的 logs\\latest.log。把服务端换成修过 log4shell 的构建（Paper 用这个版本最新的构建）以后，" +
	"MineDeck 会自动摘掉这份配置，控制台就回来了"

// hasOurLog4jConfig 启动参数里有没有 MineDeck 自己加的那两条日志配置引用。
func hasOurLog4jConfig(args []string) bool {
	return slices.Contains(args, log4jConfig) || slices.Contains(args, log4jConfig17)
}

// log4jPendingNote 启动时还没换成、没补上的，控制台里说的那一句。
func log4jPendingNote(mc string, fix log4jFix, xml string) string {
	switch fix {
	case log4jFixSwap:
		return fmt.Sprintf("[MineDeck] ⚠ log4shell 防护：%s 还引用着 1.12 ~ 1.16.5 的那份日志配置（%s），对这个版本不起作用。"+
			"这次照旧用它启动；后台正在下载 Mojang 给 1.7 ~ 1.11.2 的 %s，换成了下次启动生效。换成之前先别让不认识的人进服", mc, log4jXML, xml)
	case log4jFixAdd:
		return fmt.Sprintf("[MineDeck] ⚠ log4shell 防护：%s 要套上 Mojang 的日志配置（%s）才防得住，这台服务器的启动参数里没有（当初多半没下载成）。"+
			"这次照旧启动；后台正在下载，补上了下次启动生效。补上之前先别让不认识的人进服", mc, xml)
	}
	return ""
}

// log4jStartNote 启动时还没换成、没补上的，控制台里说的那一句；不用换也不用补时是空串。调用方拿着 procMu。
func log4jStartNote(i *Instance) string {
	fix, xml, _ := log4jFixFor(i.Settings, i.Dir)
	return log4jPendingNote(i.MC, fix, xml)
}

// log4jBeforeStartLocked 启动服务器之前看一眼 log4shell 防护要不要整理。调用方拿着 procMu。
// 摘引用不用下载，当场做，这一次启动就生效（不然 CraftBukkit 系这一次的控制台还是没有输出）；
// 换、补要下载，不等：控制台说一声，后台去做，做成了下次启动生效。
// 这一次还是带着 Mojang 的配置启动、服务端又是套上它就没有输出的那一类：先在控制台里说明，免得人对着空控制台干等。
func (m *Manager) log4jBeforeStartLocked(i *Instance) {
	fix, xml, jar := log4jFixFor(i.Settings, i.Dir)
	switch fix {
	case log4jFixDrop:
		changed, err := m.applyLog4jFixLocked(i, fix, xml, jar.fixed)
		switch {
		case !changed && err != nil:
			i.Console.Append(fmt.Sprintf("[MineDeck] ⚠ log4shell 防护：这个服务端自带的 log4j（%s）已经修过，用不着 Mojang 的日志配置，"+
				"但没能把它的引用从 instance.json 里摘掉（%v）。这次照旧带着它启动，控制台可能没有输出", jar.ver, err))
		case changed:
			i.Console.Append("[MineDeck] " + log4jDropNote(jar.ver, "这次启动就生效", err))
		}
	case log4jFixSwap, log4jFixAdd:
		// 要用的那份文件名被一份不是官方的文件占着（可能是自己改过的）：不覆盖、不引用，说清楚
		exists, official := log4jXMLOnDisk(i.Dir, xml)
		if exists && !official {
			i.Console.Append("[MineDeck] " + log4jXMLConflictNote(i.MC, xml, fix))
			break
		}
		// 官方的那一份已经在文件夹里（补：换回了没修过的构建、引用以前被摘掉过；换：17-111 以前下载过）：当场做，这一次启动就生效
		if official && m.log4jFixOnDiskLocked(i, fix, xml, jar.fixed) {
			break
		}
		i.Console.Append(log4jPendingNote(i.MC, fix, xml))
		go m.migrateLog4j(context.Background(), i)
	}
	if log4jQuietBare(i.Settings, jar) {
		i.Console.Append(log4jQuietBareNote)
	}
	if jar.quiet && hasOurLog4jConfig(i.ExtraJVM) {
		i.Console.Append("[MineDeck] ⚠ 这台服务器套着 log4shell 防护配置。" + log4jQuietNote)
	}
}

// log4jFixOnDiskLocked 开服前，要换上、补上的那份官方配置已经在服务器文件夹里：当场改启动参数，控制台说一声。
// 返回 false 是没做成、又不是设置文件写不进去（配置文件在这一下没了，或者参数刚被别处改过）：由调用方照旧交给后台去下载。
// 设置文件写不进去的照实说，不说成在后台下载（后台那一遍也只会再写不进去一次）。调用方拿着 procMu。
func (m *Manager) log4jFixOnDiskLocked(i *Instance, fix log4jFix, xml string, selfFixed bool) bool {
	what := fmt.Sprintf("服务器文件夹里有 Mojang 的这份日志配置（%s，核对过官方的校验值），启动参数里却没有引用", xml)
	act := "补上"
	if fix == log4jFixSwap {
		what = fmt.Sprintf("服务器文件夹里已经有 Mojang 给 1.7 ~ 1.11.2 的日志配置（%s，核对过官方的校验值），启动参数还引用着 1.12 ~ 1.16.5 的那份（%s），"+
			"对 %s 不起作用", xml, log4jXML, i.MC)
		act = "换上"
	}
	changed, err := m.applyLog4jFixLocked(i, fix, xml, selfFixed)
	switch {
	case changed:
		i.Console.Append(fmt.Sprintf("[MineDeck] log4shell 防护：%s，已经%s，这次启动就生效", what, act) + runBatNote(err))
		return true
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		i.Console.Append(fmt.Sprintf("[MineDeck] ⚠ log4shell 防护：%s；要%s，可这台服务器的设置文件（instance.json）写不进去（%v）。"+
			"这次照旧启动，%s之前先别让不认识的人进服；看看服务器文件夹是不是设成了只读、被别的程序占着，下次启动时会再试", what, act, err, act))
		return true
	}
	return false
}

// log4jDropNote 摘掉引用以后说的那一句（不带 [MineDeck] 前缀）。batErr 是重写 run.bat 的错误。
func log4jDropNote(ver, when string, batErr error) string {
	msg := fmt.Sprintf("log4shell 防护：这个服务端自带的 log4j 是 %s，已经修过，用不着 Mojang 的日志配置；"+
		"那份配置又和 Spigot、Paper 这类服务端的控制台对不上，套着的时候控制台没有输出、MineDeck 认不出启动完成。"+
		"已经把它的引用从启动参数里摘掉，别的参数没动，配置文件留在服务器文件夹里。%s", ver, when)
	return msg + runBatNote(batErr)
}

func runBatNote(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("。run.bat 没能重写（%v），手动双击它启动时还是旧的，到「设置 → 启动与内存」里保存一次就会重写", err)
}

// MigrateLog4j 程序启动后在后台调一次：把每台服务器的 log4shell 防护整理成现在的做法（换、补、摘，见本文件开头）。
// 结果写进各自的控制台和程序日志。
func (m *Manager) MigrateLog4j(ctx context.Context) {
	for _, i := range m.List() {
		m.migrateLog4j(ctx, i)
	}
}

// migrateLog4j 整理一台。已经有人在整理这台时直接返回。
func (m *Manager) migrateLog4j(ctx context.Context, i *Instance) {
	if _, busy := log4jMigrating.LoadOrStore(i, struct{}{}); busy {
		return
	}
	defer log4jMigrating.Delete(i)
	fix, xml, jar := log4jFixFor(i.SettingsSnapshot(), i.Dir)
	if fix == log4jFixNone {
		return
	}
	note := func(msg string) {
		i.Console.Append("[MineDeck] " + msg)
		log.Printf("「%s」%s", i.Name, msg)
	}
	release, err := m.UseWorldInstance(i, "更新 log4shell 防护")
	if err != nil {
		switch fix {
		case log4jFixSwap:
			note(fmt.Sprintf("⚠ log4shell 防护：这台 %s 的服务器还引用着 1.12 ~ 1.16.5 的那份日志配置，对这个版本不起作用，这次没换（%v）。下次启动或者重开 MineDeck 时再试", i.MC, err))
		case log4jFixAdd:
			note(fmt.Sprintf("⚠ log4shell 防护：这台 %s 的服务器没有套上 Mojang 的日志配置，等于没防，这次没补上（%v）。下次启动或者重开 MineDeck 时再试", i.MC, err))
		case log4jFixDrop:
			note(fmt.Sprintf("log4shell 防护：这个服务端自带的 log4j 已经修过，用不着 Mojang 的日志配置，这次没能摘掉它的引用（%v）。下次启动这台服务器时再试", err))
		}
		return
	}
	defer release()
	if fix == log4jFixAdd || fix == log4jFixSwap {
		if exists, official := log4jXMLOnDisk(i.Dir, xml); exists && !official {
			note(log4jXMLConflictNote(i.MC, xml, fix)) // 同名、不是官方的文件不覆盖（下载会把它换成官方的）
			return
		}
	}
	if fix != log4jFixDrop {
		ctx, cancel := context.WithTimeout(ctx, log4jMigrateTimeout)
		defer cancel()
		conflict, err := fetchAndPublishLog4jXML(ctx, i.Dir, xml)
		if err != nil {
			if fix == log4jFixSwap {
				note(fmt.Sprintf("⚠ log4shell 防护：这台 %s 的服务器还引用着 1.12 ~ 1.16.5 的那份日志配置（%s），对这个版本不起作用；"+
					"Mojang 给 1.7 ~ 1.11.2 的 %s 没下载成（%v）。服务器照样能开，先别让不认识的人进服；联网以后再启动一次或者重开 MineDeck 会再试",
					i.MC, log4jXML, xml, err))
			} else {
				note(fmt.Sprintf("⚠ log4shell 防护：这台 %s 的服务器没有套上 Mojang 的日志配置，等于没防；这次 %s 也没下载成（%v）。"+
					"服务器照样能开，先别让不认识的人进服；联网以后再启动一次或者重开 MineDeck 会再试", i.MC, xml, err))
			}
			return
		}
		if conflict {
			note(log4jXMLConflictNote(i.MC, xml, fix))
			return
		}
	}
	i.procMu.Lock()
	changed, err := m.applyLog4jFixLocked(i, fix, xml, jar.fixed)
	running := i.state != "" && i.state != "stopped"
	i.procMu.Unlock()
	if !changed {
		if err != nil {
			if fix == log4jFixDrop {
				note(fmt.Sprintf("log4shell 防护：没能把 Mojang 日志配置的引用从 instance.json 里摘掉（%v），启动参数还是原来的。下次启动这台服务器时再试", err))
			} else {
				// 官方那份可能本来就在文件夹里、这次没下载，不说成「下载好了」
				note(fmt.Sprintf("⚠ log4shell 防护：%s 已经在服务器文件夹里，但没能把它的引用存进 instance.json（%v），启动参数还是原来的。下次启动或者重开 MineDeck 时再试", xml, err))
			}
		}
		return // 没出错也没改：下载的这会儿启动参数被改过了，下一遍再看
	}
	when := "下次启动生效"
	if running {
		when = "正在运行的这一次还是旧的，重启以后生效"
	}
	switch fix {
	case log4jFixSwap:
		note(fmt.Sprintf("log4shell 防护换成了 Mojang 给 1.7 ~ 1.11.2 的日志配置（%s）：原来引用的 1.12 ~ 1.16.5 那份对 %s 不起作用。%s", xml, i.MC, when) + runBatNote(err))
	case log4jFixAdd:
		msg := fmt.Sprintf("log4shell 防护补上了 Mojang 的日志配置（%s，核对过官方的校验值）：这台 %s 的服务器原来的启动参数里没有它（当初多半没下载成），等于没防。%s", xml, i.MC, when) + runBatNote(err)
		if jar.quiet { // 补上以后这台的控制台就没有输出了，当场说清楚
			msg += "。" + log4jQuietNote
		}
		note(msg)
	case log4jFixDrop:
		note(log4jDropNote(jar.ver, when, err))
	}
}

// applyLog4jFixLocked 按 fix 改启动参数，存 instance.json、重写 run.bat。调用方拿着 procMu。
// 判断和动手之间（下载的这段时间里）参数可能被改过，所以拿着锁再判断一遍，结论变了就不动。
//   - 换：引用 112-116 那份的原地换成 17-111 那份，重复的只留一条；
//   - 补：在末尾加上引用（配置文件得在：不引用不存在的文件）；
//   - 摘：去掉 MineDeck 自己加的那两条引用。
//
// 存不进 instance.json 时参数改回原样，changed=false；存进去了、run.bat 没写成时 changed=true，err 是写 run.bat 的错误。
func (m *Manager) applyLog4jFixLocked(i *Instance, fix log4jFix, xml string, selfFixed bool) (changed bool, err error) {
	if now, nowXML := log4jFixOf(i.Settings, selfFixed); now != fix || nowXML != xml || fix == log4jFixNone {
		return false, nil
	}
	if fix != log4jFixDrop {
		if _, err := os.Stat(filepath.Join(i.Dir, xml)); err != nil {
			return false, err
		}
	}
	old := i.ExtraJVM
	next := make([]string, 0, len(old)+1)
	switch fix {
	case log4jFixSwap:
		seen := false
		for _, a := range old {
			if a == log4jConfig || a == log4jConfig17 {
				if seen {
					continue
				}
				a, seen = log4jConfig17, true
			}
			next = append(next, a)
		}
	case log4jFixAdd:
		next = append(append(next, old...), log4jConfigKey+xml)
	case log4jFixDrop:
		for _, a := range old {
			if a != log4jConfig && a != log4jConfig17 {
				next = append(next, a)
			}
		}
	}
	i.ExtraJVM = next
	if err := m.saveInstance(i); err != nil {
		i.ExtraJVM = old
		return false, err
	}
	return true, writeRunBat(i)
}

// fetchAndPublishLog4jXML 确保实例目录里有官方日志配置；需要下载时先下到临时目录，落进实例目录前拿着路径锁再看一次目标文件
// （A25 下载并发保护）：下载期间用户如果自己保存了同名、但不是官方那份的日志配置，绝不覆盖，返回 conflict=true。
func fetchAndPublishLog4jXML(ctx context.Context, dir, name string) (conflict bool, err error) {
	if _, official := log4jXMLOnDisk(dir, name); official {
		return false, nil
	}
	data, err := stageLog4jXML(ctx, name)
	if err != nil {
		return false, err
	}
	return publishLog4jXMLIfAbsent(ctx, dir, name, data)
}

func stageLog4jXML(ctx context.Context, name string) ([]byte, error) {
	stageDir, err := os.MkdirTemp("", "minedeck-log4j-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stageDir)
	if err := fetchLog4jXML(ctx, stageDir, name); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(stageDir, name))
}

func publishLog4jXMLIfAbsent(ctx context.Context, dir, name string, data []byte) (conflict bool, err error) {
	dest := filepath.Join(dir, name)
	unlock, err := atomicfile.LockPathContext(ctx, dest)
	if err != nil {
		return false, err
	}
	defer unlock()
	if exists, official := log4jXMLOnDisk(dir, name); exists {
		return !official, nil
	}
	tmpFile, err := os.CreateTemp(dir, ".minedeck-write-*.tmp")
	if err != nil {
		return false, err
	}
	tmp := tmpFile.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return false, err
	}
	if err := tmpFile.Chmod(0o644); err != nil {
		_ = tmpFile.Close()
		return false, err
	}
	if err := tmpFile.Close(); err != nil {
		return false, err
	}
	if err := atomicfile.PublishNew(tmp, dest); err != nil {
		if errors.Is(err, fs.ErrExist) {
			_, official := log4jXMLOnDisk(dir, name)
			return !official, nil
		}
		return false, err
	}
	return false, nil
}

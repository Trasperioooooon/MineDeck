package inst

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"minedeck/internal/mcsrc"
)

// 1.7 ~ 1.11.2 的服务器，v3.2.0 以前套的是 1.12 ~ 1.16.5 的那份日志配置（log4j2_112-116.xml）。它靠 %msg{nolookups}，
// 这段版本自带的 log4j 2.0-beta9 不认，等于没防。这里把已经建好的这些服务器换成 Mojang 给这段版本的 log4j2_17-111.xml。
//
// 什么时候换：程序启动后在后台过一遍（MigrateLog4j）。那时没换成的（多半是没联网），启动这台服务器时在控制台说一声，
// 再在后台试一次，换成了下次启动生效：启动本身不等下载。
// 怎么换：先把新文件下载好、核对过 SHA-1，再把启动参数里引用旧文件的那一条原地换成新的，存 instance.json、重写 run.bat。
// 下载不成什么都不动，照旧用旧配置开：引用一个不存在的配置文件，log4j 会退回只输出 ERROR 的默认配置，
// 控制台和「启动完成」的判断都会失效。别的 JVM 参数一条不碰。
// 换下来的 112-116 那份留在服务器文件夹里，不删：分不清用户自己的启动脚本（比如接管前那台服的 start.bat）还用不用它，
// 删了那边就只剩 ERROR 输出；启动脚本被占着、重写失败时，原来的 run.bat 也还引用着它。留着只占几 KB（2026-11-18 Codex 复审）。

// log4jMigrating 正在换配置的实例：程序启动时的那一遍和启动服务器时补的那一次，不会同时下载、同时改同一台。
var log4jMigrating sync.Map // *Instance → struct{}

// log4jMigrateTimeout 一台服务器换配置时下载最多等多久（测试里改短）。
var log4jMigrateTimeout = 2 * time.Minute

// needsLog4jMigrate 这台服务器是不是还引用着对它不起作用的 112-116 那份配置。只看本地清单，判断不了版本的不动。
func needsLog4jMigrate(s Settings) bool {
	if mcsrc.KindOf(s.Core) == mcsrc.KindProxy || !slices.Contains(s.ExtraJVM, log4jConfig) {
		return false
	}
	_, xml, ok := log4jPlanCached(s.MC)
	return ok && xml == log4jXML17
}

// log4jStartNote 启动时还没换成的，控制台里说的那一句；不用换时是空串。调用方拿着 procMu。
func log4jStartNote(i *Instance) string {
	if !needsLog4jMigrate(i.Settings) {
		return ""
	}
	return fmt.Sprintf("[MineDeck] ⚠ log4shell 防护：%s 还引用着 1.12 ~ 1.16.5 的那份日志配置（%s），对这个版本不起作用。"+
		"这次照旧用它启动；后台正在下载 Mojang 给 1.7 ~ 1.11.2 的 %s，换成了下次启动生效。换成之前先别让不认识的人进服", i.MC, log4jXML, log4jXML17)
}

// MigrateLog4j 程序启动后在后台调一次：把引用着 112-116 那份配置的 1.7 ~ 1.11.2 服务器换成 17-111 那份。
// 结果写进各自的控制台和程序日志。
func (m *Manager) MigrateLog4j(ctx context.Context) {
	for _, i := range m.List() {
		m.migrateLog4j(ctx, i)
	}
}

// migrateLog4j 换一台。已经有人在换这台时直接返回。
func (m *Manager) migrateLog4j(ctx context.Context, i *Instance) {
	if _, busy := log4jMigrating.LoadOrStore(i, struct{}{}); busy {
		return
	}
	defer log4jMigrating.Delete(i)
	if !needsLog4jMigrate(i.SettingsSnapshot()) {
		return
	}
	note := func(msg string) {
		i.Console.Append("[MineDeck] " + msg)
		log.Printf("「%s」%s", i.Name, msg)
	}
	release, err := m.UseWorldInstance(i, "更新 log4shell 防护")
	if err != nil {
		note(fmt.Sprintf("⚠ log4shell 防护：这台 %s 的服务器还引用着 1.12 ~ 1.16.5 的那份日志配置，对这个版本不起作用，这次没换（%v）。下次启动或者重开 MineDeck 时再试", i.MC, err))
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, log4jMigrateTimeout)
	defer cancel()
	if err := fetchLog4jXML(ctx, i.Dir, log4jXML17); err != nil {
		note(fmt.Sprintf("⚠ log4shell 防护：这台 %s 的服务器还引用着 1.12 ~ 1.16.5 的那份日志配置（%s），对这个版本不起作用；"+
			"Mojang 给 1.7 ~ 1.11.2 的 %s 没下载成（%v）。服务器照样能开，先别让不认识的人进服；联网以后再启动一次或者重开 MineDeck 会再试",
			i.MC, log4jXML, log4jXML17, err))
		return
	}
	i.procMu.Lock()
	swapped, err := m.swapLog4jConfigLocked(i)
	running := i.state != "" && i.state != "stopped"
	i.procMu.Unlock()
	switch {
	case !swapped && err != nil:
		note(fmt.Sprintf("⚠ log4shell 防护：%s 下载好了，但没能存进 instance.json（%v），启动参数还是原来的。下次启动或者重开 MineDeck 时再试", log4jXML17, err))
	case swapped:
		when := "下次启动生效"
		if running {
			when = "正在运行的这一次还是旧的，重启以后生效"
		}
		msg := fmt.Sprintf("log4shell 防护换成了 Mojang 给 1.7 ~ 1.11.2 的日志配置（%s）：原来引用的 1.12 ~ 1.16.5 那份对 %s 不起作用。%s", log4jXML17, i.MC, when)
		if err != nil {
			msg += fmt.Sprintf("。run.bat 没能重写（%v），手动双击它启动时还是旧的，到「设置 → 启动与内存」里保存一次就会重写", err)
		}
		note(msg)
	}
}

// swapLog4jConfigLocked 把启动参数里引用 112-116 那份的换成 17-111 那份（原地换，重复的只留一条），存 instance.json、重写 run.bat。
// 下载的这段时间里参数可能被改过，所以拿着锁再看一遍。存不进 instance.json 时参数改回原样，swapped=false；
// 存进去了、run.bat 没写成时 swapped=true，err 是写 run.bat 的错误。调用方拿着 procMu。
func (m *Manager) swapLog4jConfigLocked(i *Instance) (swapped bool, err error) {
	if !needsLog4jMigrate(i.Settings) {
		return false, nil
	}
	old := i.ExtraJVM
	next := make([]string, 0, len(old))
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
	i.ExtraJVM = next
	if err := m.saveInstance(i); err != nil {
		i.ExtraJVM = old
		return false, err
	}
	return true, writeRunBat(i)
}

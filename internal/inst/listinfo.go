package inst

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"minedeck/internal/mcsrc"
)

// 服务器列表、玩家名单、接管识别要给界面看的几样东西：朋友连哪个端口、白名单开着没有、移出列表后文件夹里会留下哪些 MineDeck 的文件。

// ConnectPort 朋友在游戏里连这台服务器用的端口（界面上「连接地址」的那半截）。
// 游戏服务器就是 server-port（同 Port）。代理端（Velocity、Waterfall）的端口写在它自己的配置文件里：
// Velocity 是 velocity.toml 的 bind，Waterfall 是 config.yml 里 listeners 的 host。配置文件要第一次启动以后才生成，
// 那之前、或者读不出来时返回 0，界面说「第一次启动以后才知道」，不拿一个猜的数冒充。
func (i *Instance) ConnectPort() int {
	if mcsrc.KindOf(i.Core) != mcsrc.KindProxy {
		return i.Port()
	}
	return proxyPort(i.Dir, i.Core)
}

var (
	reVelocityBind  = regexp.MustCompile(`(?m)^[ \t]*bind[ \t]*=[ \t]*["']([^"'\r\n]*)["']`)
	reWaterfallHost = regexp.MustCompile(`(?m)^[ \t]*-?[ \t]*host:[ \t]*["']?([^"'\s#]+)`)
)

// proxyPort 从代理端自己的配置文件里读监听端口；文件不在、没写、写得不对都返回 0。
func proxyPort(dir string, core mcsrc.Core) int {
	var addr string
	switch core {
	case mcsrc.CoreVelocity:
		b, err := readCapped(filepath.Join(dir, "velocity.toml"), 1<<20)
		if err != nil {
			return 0
		}
		if m := reVelocityBind.FindSubmatch(b); m != nil {
			addr = string(m[1])
		}
	case mcsrc.CoreWaterfall:
		b, err := readCapped(filepath.Join(dir, "config.yml"), 1<<20)
		if err != nil {
			return 0
		}
		s := string(b)
		// 只看 listeners 下面的：config.yml 里别处（servers 的后端地址）用的是 address，不是 host，这里再保险一点
		if k := strings.Index(s, "listeners:"); k >= 0 {
			s = s[k:]
		}
		if m := reWaterfallHost.FindStringSubmatch(s); m != nil {
			addr = m[1]
		}
	default:
		return 0
	}
	k := strings.LastIndex(addr, ":")
	if k < 0 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(addr[k+1:]))
	if err != nil || n <= 0 || n > 65535 {
		return 0
	}
	return n
}

// WhitelistOn server.properties 里的白名单开关，按服务端的读法取（同一项写了几行认最后一行，见 javaprops.go）。
// 没有这个文件、或者文件里没写这一项时返回 nil：这时服务端用它自己的默认值（26.3 起是开，更早是关），不能当成「关」。
func WhitelistOn(dir string) *bool {
	v, ok := javaPropsOf(filepath.Join(dir, "server.properties"))["white-list"]
	if !ok {
		return nil
	}
	on := strings.EqualFold(strings.TrimSpace(v), "true")
	return &on
}

// MineDeckFiles 接管来的服务器的文件夹里，MineDeck 自己写的、移出列表以后还会留在那儿的文件和文件夹（相对路径，存在的才列）。
// 移出列表的确认框里照着这份说（可用性走查 U30）；instance.json 不在里面，它会改名成 instance.json.bak，界面另外说。
// 照写它们的代码列：接管（adopt.go 开头的清单）、启动脚本（writeRunBat）、老版本的 log4shell 缓解、游戏内 AI 的设置和记账、
// 改配置文件时留的备份（.minedeck）、和自家模组之间的端口和令牌（config/minedeck）。
// eula.txt 不列：分不清是不是 MineDeck 写的（多数是服务端自己或用户写的），写的也只是一句同意。
// 不是接管来的服务器返回 nil：文件夹本来就是 MineDeck 建的。
func (i *Instance) MineDeckFiles() []string {
	if !i.Adopted {
		return nil
	}
	s := i.SettingsSnapshot()
	names := []string{"run.bat", runBatBackup}
	if strings.HasPrefix(s.LaunchTarget, "args:") { // 只有 Forge / NeoForge 的 user_jvm_args.txt 是 MineDeck 改写过的
		names = append(names, "user_jvm_args.txt", jvmArgsBackup)
	}
	names = append(names, log4jXML, "gameai.json", "gameai-state.json", ".minedeck", "config/minedeck")
	var out []string
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(i.Dir, filepath.FromSlash(n))); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// JavaMissing 这台服务器配的 java.exe 现在找不到了（被卸载、挪走，或者便携版的文件夹被删了）：
// 卡片和详情页头据此常驻一条警告，「设置 → 启动与内存」的 Java 下拉里把它标成「找不到」（可用性走查 R21）。
// 只看文件在不在，不跑它；路径是空的也算找不到（启动时同样会报「找不到 Java」）。
func (i *Instance) JavaMissing() bool {
	_, path := i.JavaSnapshot()
	if strings.TrimSpace(path) == "" {
		return true
	}
	st, err := os.Stat(path)
	return err != nil || st.IsDir()
}

// BackupDir 这台服务器的新备份放在哪个文件夹（当前备份根下面）。还没备份过的话文件夹可能还不存在。
func (i *Instance) BackupDir() string { return backupDirOf(i.Name) }

package inst

import (
	"os"
	"path/filepath"
	"strings"
)

// 别人给的文件（交接包、整合包的覆盖目录）落进实例目录之前要过的两道关：
// 哪些文件名是 MineDeck 自己的、不能由别人给；server.properties 里的 level-name 指没指到实例目录外面。
// 以后 MineDeck 再往实例目录里放自己读的文件，记得加进 controlRoot / controlDirs。

// controlRoot 实例根目录下由 MineDeck 自己写、自己读的文件（小写）：别人给的包里带了也不采用。
//   - instance.json、run.bat、user_jvm_args.txt 决定这台电脑上执行什么。交接包不拦的话，导入在解包之后、收尾之前被打断
//     （关程序、断电），目录里留下的就是包里那份 instance.json——下次启动扫描会把它登记成一台服务器，
//     用哪个程序当 Java、带什么参数全由包说了算。整合包的覆盖目录是在实例登记之后才写的，带一份 instance.json
//     会直接顶掉刚登记的那份，重启 MineDeck 以后生效。
//   - log4j2_112-116.xml 是老版本的日志配置：MineDeck 看到这个文件在，就给启动参数加上对它的引用；
//     老版本 log4j 的配置文件能跑脚本、能发 JNDI 请求，和 JVM 参数是同一类东西。只用官方下载的那份。
//   - gameai.json、gameai-state.json 是游戏内 AI 的设置和记账（谁能用、用了多少）：花的是这台电脑主人的钱，不能由包来定。
var controlRoot = map[string]bool{
	"instance.json": true, "instance.json.tmp": true, "instance.json.bak": true,
	"run.bat": true, "run.before-minedeck.bat": true, "user_jvm_args.txt": true,
	"log4j2_112-116.xml": true,
	"gameai.json":        true, "gameai-state.json": true,
}

// controlDirs 整个目录都不采用的（小写、斜杠结尾）：
//   - config/minedeck：MineDeck 和自家模组之间的端口、令牌。令牌是原来那台电脑上的，要用这些功能时 MineDeck 会重新生成。
//   - .minedeck：MineDeck 给配置文件留的备份。
//   - logs、crash-reports：排障功能和 AI 助手会读这里的内容，别人给的日志不收（MineDeck 自己导出的交接包里本来也没有）。
var controlDirs = []string{"config/minedeck/", ".minedeck/", "logs/", "crash-reports/"}

// IsControlFile rel（相对实例目录）是不是不能由别人给的文件。交接包解包、整合包应用覆盖目录时不采用，交接包导出时也不带。
func IsControlFile(rel string) bool {
	low := strings.ToLower(filepath.ToSlash(rel))
	if controlRoot[low] {
		return true
	}
	for _, d := range controlDirs {
		if strings.HasPrefix(low, d) {
			return true
		}
	}
	return false
}

// ScrubControlFiles 把 dir 里的控制文件和控制目录删掉（不存在就算了）。交接包解包之后、收尾之前调：
// 解包时已经按名字跳过了一遍，这里按磁盘上的实际情况再清一遍——名字怎么比由 Windows 自己定，
// 包里不管用大小写还是别的别名写法，都落不下这几样。只能对刚建出来、还没登记的目录用。
func ScrubControlFiles(dir string) {
	for name := range controlRoot {
		_ = os.Remove(filepath.Join(dir, name))
	}
	for _, d := range controlDirs {
		_ = os.RemoveAll(filepath.Join(dir, filepath.FromSlash(d)))
	}
}

// SafeLevelName server.properties 的 level-name 能不能照着用：必须落在实例目录里面，也不能就是实例目录自己。
// 可以带子目录（worlds/main）。备份和还原按这个名字找世界目录、还原前还要把它删掉，所以指到外面的
// （../别的服务器）、指回实例目录自己的（. 或 a/..）都不认。
func SafeLevelName(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(v))
	return clean != "." && filepath.IsLocal(clean)
}

// CheckLevelName 看 dir 里 server.properties 的 level-name 有没有指到实例目录外面。没有这个文件、没写这一项都算没问题。
// 别人给的 server.properties（交接包、整合包的覆盖目录）落盘以后调：有问题就不要这个包。
func CheckLevelName(dir string) (name string, ok bool) {
	p, err := LoadProps(filepath.Join(dir, "server.properties"))
	if err != nil {
		return "", true
	}
	v, has := p.Get("level-name")
	if !has || strings.TrimSpace(v) == "" {
		return "", true
	}
	return v, SafeLevelName(v)
}

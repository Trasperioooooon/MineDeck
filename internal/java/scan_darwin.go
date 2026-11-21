package java

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Mac 上的 Java：程序名是 java，装在 JDK 包（xxx.jdk）的 Contents/Home 里。
//
// 第 0 步摸底只做「用本机已装的 Java」：扫描系统的 JVM 目录、Homebrew、SDKMAN、官方启动器自带的运行时。
// 自动下载便携 Java 还没接上（Adoptium 的 Mac 包是 .tar.gz、Mojang 的运行时清单键是 mac-os-arm64，
// 解出来的文件还要补可执行权限），见 autoInstallErr。

const exeName = "java"

// IsExeName base（路径的最后一段）是不是 Java 程序本身。Mac 默认的文件系统不分大小写，比较也不分。
func IsExeName(base string) bool { return strings.EqualFold(base, "java") }

// homeExe Java 安装文件夹里的 java：给的是 JDK 包（xxx.jdk）时进 Contents/Home。
func homeExe(home string) string {
	if p := filepath.Join(home, "Contents", "Home", "bin", "java"); fileExists(p) {
		return p
	}
	return filepath.Join(home, "bin", "java")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// seenKey 扫描去重用的键：解开符号链接（Homebrew、~/Library/Java 里多半是指来指去的链接），不分大小写。
// 同一个 Java 有几种写法时，后加进来的那种留下（见 commonJavaDirs 的顺序）。
func seenKey(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return strings.ToLower(p)
}

// skipJava /usr/bin/java 是系统的转发程序：没装 JDK 时运行它会弹「需要安装 Java」的对话框，装了的话指的也是下面会扫到的某一个。
func skipJava(p string) bool { return p == "/usr/bin/java" }

// registeredJavas Mac 上没有注册表；/usr/libexec/java_home 列的也就是 commonJavaDirs 里的那两个 JVM 目录。
func registeredJavas(func(string)) {}

// commonJavaDirs 把常见安装位置的 Java 交给 add。系统的 JVM 目录放在最后：同一个 Java 也从 Homebrew 的路径扫到时，
// 留下的是 JVM 目录里的写法（不带 Homebrew 的版本号，升级以后路径不变）。
func commonJavaDirs(add func(string)) {
	home, _ := os.UserHomeDir()
	globs := []string{
		"/opt/homebrew/opt/openjdk*/libexec/openjdk.jdk", // Homebrew（Apple 芯片）
		"/usr/local/opt/openjdk*/libexec/openjdk.jdk",    // Homebrew（Intel）
	}
	if home != "" {
		globs = append(globs,
			filepath.Join(home, ".sdkman", "candidates", "java", "*"),
			filepath.Join(home, ".jdks", "*"), // IntelliJ IDEA 下载的 JDK
			// 官方启动器的运行时
			filepath.Join(home, "Library", "Application Support", "minecraft", "runtime", "*", "mac-os*", "*", "jre.bundle"),
		)
	}
	globs = append(globs, "/Library/Java/JavaVirtualMachines/*")
	if home != "" {
		globs = append(globs, filepath.Join(home, "Library", "Java", "JavaVirtualMachines", "*"))
	}
	for _, pat := range globs {
		ms, _ := filepath.Glob(pat)
		for _, m := range ms {
			add(m)
		}
	}
}

// autoInstallOK 自动下载便携 Java。Mac 版还没接上下载源，关着；是变量只为测试：下载、解压、记号那几步和平台无关，Mac 上跑测试时打开。
var autoInstallOK = false

// autoInstallErr Mac 版还不能自动下载便携 Java：说清楚怎么自己装。
func autoInstallErr(major int) error {
	if autoInstallOK {
		return nil
	}
	return fmt.Errorf("Mac 版还不能自动下载 Java %d。请先自己装一个（比如在终端里执行 brew install openjdk@%d，"+
		"或者到 adoptium.net 下载 Mac 的 .pkg 安装包），装好后到「设置 → Java 运行时」里点「重新扫描本机 Java」", major, major)
}

package java

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// exeName 便携运行时和扫描时认的 Java 程序名。
const exeName = "java.exe"

// IsExeName base（路径的最后一段）是不是 Java 程序本身：java.exe 或 javaw.exe，不分大小写。
func IsExeName(base string) bool {
	base = strings.ToLower(base)
	return base == "java.exe" || base == "javaw.exe"
}

// homeExe Java 安装文件夹里的 java.exe。
func homeExe(home string) string { return filepath.Join(home, "bin", "java.exe") }

// seenKey 扫描去重用的键：Windows 的路径不分大小写。
func seenKey(p string) string { return strings.ToLower(p) }

// skipJava 扫描时不看的路径：Windows 上没有。
func skipJava(string) bool { return false }

// autoInstallErr 这个平台上能不能自动下载便携 Java：Windows 上能，返回 nil。
func autoInstallErr(int) error { return nil }

// registeredJavas 把注册表里登记的 Java 交给 add（安装文件夹或 java.exe 都行）。
func registeredJavas(add func(string)) {
	for _, root := range []string{`SOFTWARE\JavaSoft`, `SOFTWARE\WOW6432Node\JavaSoft`} {
		for _, sub := range []string{"JDK", "JRE", "Java Development Kit", "Java Runtime Environment"} {
			k, err := registry.OpenKey(registry.LOCAL_MACHINE, root+`\`+sub, registry.READ)
			if err != nil {
				continue
			}
			names, _ := k.ReadSubKeyNames(-1)
			for _, n := range names {
				vk, err := registry.OpenKey(registry.LOCAL_MACHINE, root+`\`+sub+`\`+n, registry.READ)
				if err != nil {
					continue
				}
				if home, _, err := vk.GetStringValue("JavaHome"); err == nil {
					add(home)
				}
				vk.Close()
			}
			k.Close()
		}
	}
}

// commonJavaDirs 把常见安装目录里的 Java 交给 add。
func commonJavaDirs(add func(string)) {
	home, _ := os.UserHomeDir()
	var roots []string
	for _, pf := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if pf == "" {
			continue
		}
		for _, vendor := range []string{
			"Java", "Eclipse Adoptium", "Eclipse Foundation", "Microsoft",
			"Zulu", "BellSoft", "Amazon Corretto", "AdoptOpenJDK", "Semeru",
		} {
			roots = append(roots, filepath.Join(pf, vendor))
		}
	}
	if home != "" {
		roots = append(roots, filepath.Join(home, ".jdks"))
		// 官方启动器 / 第三方启动器的运行时目录
		for _, pat := range []string{
			filepath.Join(os.Getenv("APPDATA"), ".minecraft", "runtime", "*", "windows-x64", "*"),
			filepath.Join(home, "AppData", "Roaming", ".minecraft", "runtime", "*", "windows-x64", "*"),
		} {
			if ms, _ := filepath.Glob(pat); ms != nil {
				for _, m := range ms {
					add(m)
				}
			}
		}
	}
	for _, r := range roots {
		ents, err := os.ReadDir(r)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() {
				add(filepath.Join(r, e.Name()))
			}
		}
	}
}

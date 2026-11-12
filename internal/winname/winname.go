// Package winname 判断一个名字在 Windows 上能不能原样当文件名用。
//
// 导入别人给的东西时都用这一套（v3.1.0 正式版前检查 C-2e）：现成服务端压缩包、整合包的覆盖文件和清单里的落点、
// 交接包、CurseForge 给的文件名、从电脑添加或从 Modrinth 下载的模组。以前用的 filepath.IsLocal 只在当前这台
// Windows 真把名字当设备时才拦：Windows 11 上 CON.txt、COM1.txt 已经是普通文件名，老一些的 Windows 还当设备，
// 同一个包换一台电脑导入，结果就不一样。这里不看系统版本，一律从严。
//
// 它只管名字本身能不能用，叠加在各处原有的检查（往上走的 ..、绝对路径、落点前缀、MineDeck 自己的文件）之上，不替代它们。
package winname

import "strings"

// Reserved 是不是 Windows 的保留设备名：CON、PRN、AUX、NUL、CONIN$、CONOUT$、COM0–9、LPT0–9，
// COM、LPT 后面跟上标的 ¹ ² ³ 也算（Windows 的文档里列着 COM¹、LPT³ 这些）。不分大小写；
// 带扩展名的（CON.txt、com1.jar）、设备名后面先跟空格再接扩展名的（"NUL .txt"）也算。
func Reserved(name string) bool {
	base, _, _ := strings.Cut(strings.ToUpper(name), ".")
	base = strings.TrimRight(base, " ")
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if r := []rune(base); len(r) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return (r[3] >= '0' && r[3] <= '9') || r[3] == '¹' || r[3] == '²' || r[3] == '³'
	}
	return false
}

// OK 一段名字（文件名或文件夹名，不带路径分隔符）能不能原样写到 Windows 上：
//   - 不空，不是 . 或 ..；
//   - 结尾不是点或空格：Windows 落盘时会把它们去掉，落下来的名字就不是这个了（"run.bat." 落下来是 run.bat）；
//   - 不带 < > : " / \ | ? * 和控制字符（冒号还会写进备用数据流）；
//   - 不是保留设备名（见 Reserved）。
func OK(seg string) bool {
	if seg == "" || strings.TrimRight(seg, ". ") != seg {
		return false // 也挡住了 . 和 ..
	}
	for _, r := range seg {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return false
		}
	}
	return !Reserved(seg)
}

// PathOK 斜杠分隔的相对路径，每一段都 OK。不化简：带 . 和 .. 的段、开头的斜杠、连着的两个斜杠都不收。
func PathOK(rel string) bool {
	if rel == "" {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if !OK(seg) {
			return false
		}
	}
	return true
}

// Fold 比较两个路径在 Windows 上是不是同一个文件用的键：NTFS 不分大小写（按大写比），斜杠方向不论。
// 只差大小写的两个名字落到 Windows 上是同一个文件，后写的会盖掉先写的。
func Fold(rel string) string { return strings.ToUpper(strings.ReplaceAll(rel, `\`, "/")) }

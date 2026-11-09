// Package transfer MineDeck 实例的「服务器打包 / 收包」引擎：把一台实例打成一个 .mdpack 压缩包，
// 交给别人（打包下发、交接）后，对方解开就是一台可以直接开服的实例。 manifest（包内的 mdpack.json）是
// 双方的契约：
//
//	formatVersion   包格式版本（现在是 1）
//	mode            handover（服主交接）或 distributable（打包下发）
//	exportedAt      导出时间
//	exporterVersion 导出方 MineDeck 的版本号
//	instance        instance.json 的原文（接收方落地时按它复原，再改名字等适配）
//	mods            模组清单（mods / plugins 里每个 jar 的解析结果）
//	worldSize       世界存档的总大小（字节）；玩家进度在 playerdata/<uuid>.dat 里，一定在包里
//	excluded        实际剔除的条目清单（如下面 ExcludedFor 列的）
//
// 这个包不碰 Windows API，Linux 上可以直接测试，也不引用 inst：打包方与收包方只通过 manifest 字段对齐。
package transfer

import (
	"strings"
)

// FormatVersion mdpack 格式版本。收到更高版本时按不认识处理。
const FormatVersion = 1

// ManifestFile 包内放 manifest 的固定条目名。解压时这个条目不落盘（内容已经读进 Inspect 的结果里）。
const ManifestFile = "mdpack.json"

// Ext 包文件的扩展名。
const Ext = ".mdpack"

// 模式。
const (
	ModeDistributable = "distributable" // 打包下发：把服主的服务器发给成员，世界存档带全（收包即全员进度）
	ModeHandover      = "handover"      // 服主交接：把整台服务器交给下一任服主
)

// ValidMode mode 合法与否。
func ValidMode(mode string) bool {
	switch mode {
	case ModeDistributable, ModeHandover:
		return true
	}
	return false
}

// ExcludedFor 一个模式默认剔除什么（相对实例目录的路径；匹配规则见 Excluded）。
// MineDeck 自己写的簿记文件（instance.json、run.bat）接收方落地时会重新生成，剔除一份避免新旧混淆；
// session.lock 是运行中的锁，带着没有意义。 distributable 面向「收包的人直接开服」：连日志、崩溃报告、
// 可重建的缓存一起剔掉，包更小更干净；handover 面向「下一任服主」：记录文件对交接有价值，尽量保留。
func ExcludedFor(mode string) []string {
	base := []string{".minedeck", "instance.json", "run.bat", "session.lock"}
	switch mode {
	case ModeDistributable:
		return append(base, "logs", "crash-reports", "usercache.json")
	case ModeHandover:
		return base
	}
	return base
}

// Excluded 剔除项的匹配规则：
//   - 不带斜杠的项（如 logs、session.lock）：路径的任何一段与它同名就算剔除（Windows 不分大小写）——
//     也就是说剔除的就是这个目录和它里面的一切、任何深度上的同名文件；
//   - 带斜杠的项（如 .minedeck/config-backups）：从实例目录起的前缀，按整段匹配。
//
// rel 是相对实例目录的斜杠路径，directory 表示这条目是目录。
func Excluded(excludes []string, rel string, directory bool) bool {
	if rel == "" {
		return false
	}
	segments := strings.Split(rel, "/")
	// 无斜杠的项：任何一段同名就剔除
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		for _, e := range excludes {
			if !strings.Contains(e, "/") && strings.EqualFold(e, seg) {
				return true
			}
		}
	}
	// 带斜杠的项：从根起的前缀（整段比较）
	n := len(segments)
	if directory {
		n--
	}
	for i := 1; i <= n; i++ {
		prefix := strings.Join(segments[:i], "/")
		for _, e := range excludes {
			if strings.Contains(e, "/") && strings.EqualFold(e, prefix) {
				return true
			}
		}
	}
	return false
}

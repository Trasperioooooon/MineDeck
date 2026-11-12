package tunnel

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"minedeck/internal/dl"
)

// ---------- frpc 被杀毒软件拦下 ----------
//
// frp 这类内网穿透程序常被杀毒软件当成风险软件。2026-11-12 在作者电脑上实测：OpenFrp 的 frpc 解压出来几秒
// 就被 Windows 安全中心隔离了，报的是 PUA:Win32/FRProxy 和 Trojan:Win32/Wacatac.A!ml。原来用户看到的是一句
// 英文的系统报错，再点启动，程序又解压一遍、又被删。这里认出这几种情况，卡片上换成中文说明，系统给的原文记进日志。

// Windows 的两个系统错误码：225 是文件里有病毒或可能不需要的软件、操作没有完成；226 是同样的原因，并且文件已被删除。
// 杀毒软件拦下 frpc 时，写它、打开它、启动它都可能得到这两个。别的系统上没有这两个值。
const (
	errVirusInfected = syscall.Errno(225)
	errVirusDeleted  = syscall.Errno(226)
)

// errFrpcGone frpc 刚才还在，转眼就没了：解压出来以后找不到，刚找到、启动时却说文件不存在，
// 或者跑着跑着退出、exe 也没了。多半是杀毒软件把它隔离了。
var errFrpcGone = errors.New("frpc 不见了")

// isVirusErr 系统报「文件里有病毒或可能不需要的软件」。225、226 是 Windows 的通用错误码，
// Windows 安全中心和别的杀毒软件拦文件时都会给，所以说明里不咬定是哪一家。
func isVirusErr(err error) bool {
	return errors.Is(err, errVirusInfected) || errors.Is(err, errVirusDeleted)
}

// frpcBlockedHint 认出 frpc 被杀毒软件拦下或删掉时，返回给人看的说明；认不出时 ok 为 false。dir 是 frpc 所在的文件夹。
// 下载 frpc 的多个源里，有一个写盘时被拦下、最后一个只是 404 的，也要认出来（dl.AnyFailure 逐个看每个源）。
func frpcBlockedHint(err error, dir string) (hint string, ok bool) {
	var head string
	switch {
	case dl.AnyFailure(err, isVirusErr):
		head = "frpc 被杀毒软件拦下了"
	case errors.Is(err, errFrpcGone):
		head = "frpc 不见了，多半是被杀毒软件删掉了"
	default:
		return "", false
	}
	return head + "（frp 这类内网穿透程序常被当成风险软件）。要继续用：Windows 自带的防护，到 Windows 安全中心的「保护历史记录」里找到 frpc，" +
		"允许或还原它；别的杀毒软件，到它的隔离区里恢复。然后再启动隧道。frpc 在这个文件夹：" + dir, true
}

// frpcGoneHint frpc 意外退出以后 exe 也不在了（跑着被杀毒软件掐掉、隔离），返回说明；exe 还在时返回空串。
func frpcGoneHint(exe string) string {
	if _, err := os.Stat(exe); !errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	hint, _ := frpcBlockedHint(errFrpcGone, filepath.Dir(exe))
	return hint
}

// frpcAfterExtract 解压完以后找 frpc。压缩包里有 frpc、解压完却找不到，算 errFrpcGone；包里本来就没有，返回 notFound。
func frpcAfterExtract(dir string, extracted []string, notFound error) (string, error) {
	if exe := findExe(dir); exe != "" {
		return exe, nil
	}
	for _, n := range extracted {
		if isFrpcExeName(n) {
			return "", fmt.Errorf("解压出来的 %s：%w", n, errFrpcGone)
		}
	}
	return "", notFound
}

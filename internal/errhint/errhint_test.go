package errhint

import (
	"archive/zip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
)

// AssertValidUIHint 校验单条提示是否符合前端 errSummary 的全部展示要求。
func AssertValidUIHint(t *testing.T, hint string) {
	t.Helper()
	if err := CheckUIHint(hint); err != nil {
		t.Fatal(err)
	}
}

type staticHintErr struct {
	msg  string
	hint string
}

func (e staticHintErr) Error() string { return e.msg }
func (e staticHintErr) Hint() string  { return e.hint }

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestWrapPreservesChainAndInnerHint(t *testing.T) {
	if Wrap(nil, "提示") != nil {
		t.Fatal("nil 错误不应被包装")
	}
	sentinel := errors.New("dial tcp 1.2.3.4:443: i/o timeout")
	wrapped := Wrap(sentinel, "连不上远端，请检查网络")
	if wrapped.Error() != sentinel.Error() {
		t.Fatalf("Error() 原文被改了：%q", wrapped.Error())
	}
	if !errors.Is(wrapped, sentinel) {
		t.Fatal("errors.Is 无法穿透 Wrap")
	}
	var h interface{ Hint() string }
	if !errors.As(wrapped, &h) || h.Hint() != "连不上远端，请检查网络" {
		t.Fatalf("Wrap 未提供预期 Hint：%v", h)
	}
	AssertValidUIHint(t, h.Hint())

	inner := staticHintErr{msg: "HTTP 401", hint: "密钥不对"}
	outer := Wrap(fmt.Errorf("调用失败：%w", inner), "外层泛化提示")
	if !errors.As(outer, &h) || h.Hint() != "密钥不对" {
		t.Fatalf("已有内层 Hint 不应被外层覆盖：%q", h.Hint())
	}
}

func TestFromErrorBranchesAndUICompat(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"permission", fmt.Errorf("写配置失败：%w", os.ErrPermission), "权限"},
		{"zip_corrupt", fmt.Errorf("打开失败：%w", zip.ErrFormat), "压缩包损坏"},
		{"unexpected_eof", fmt.Errorf("读取响应：%w", io.ErrUnexpectedEOF), "压缩包损坏"},
		{"tls", &tls.CertificateVerificationError{}, "安全连接握手失败"},
		{"deadline", fmt.Errorf("请求：%w", context.DeadlineExceeded), "超时"},
		{"net_timeout", timeoutErr{}, "超时"},
		{"dns", &net.DNSError{Err: "no such host", Name: "example.com"}, "连不上远端服务器"},
		{"url_dial", &url.Error{Op: "Get", URL: "https://a.invalid", Err: errors.New("connection refused")}, "连不上远端服务器"},
	}
	for _, tc := range cases {
		got := FromError(tc.err)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want substring %q", tc.name, got, tc.want)
		}
		AssertValidUIHint(t, got)
	}
	if got := FromError(errors.New("请先停止服务器再删除")); got != "" {
		t.Fatalf("纯业务中文错误不应误给兜底提示：%q", got)
	}
	if got := FromError(&url.Error{Op: "Get", URL: "https://a.invalid", Err: context.Canceled}); got != "" {
		t.Fatalf("用户主动取消不应误判成网络故障：%q", got)
	}
	if !errors.Is(&fs.PathError{Op: "open", Path: "x", Err: fs.ErrPermission}, fs.ErrPermission) {
		t.Fatal("fs.ErrPermission sanity")
	}
}

func TestAllPackageStaticHintsValidForUI(t *testing.T) {
	hints := []string{
		"D 盘剩余空间不足，请清理磁盘空间或更换存放文件夹后再试",
		"磁盘剩余空间不足，请清理磁盘空间或更换存放文件夹后再试",
		"获取版本列表失败，请检查网络或在「设置 → 下载与网络」切换下载源",
		"自动下载运行时失败，可在「设置 → 下载与网络」切换源或手动添加",
		"便携运行时已装好，但临时标记未删掉，请检查目录权限",
		"这个路径下的程序无法运行，请确认选的是可用的运行时目录",
		"清理便携运行时失败，请确认没有服务器正在使用它并检查目录权限",
		"连不上更新服务器，请检查网络后重试，或直接去发布页查看",
		"下载新版本失败，请检查网络后重试，或去发布页手动下载",
		"打不开这个整合包，请确认是完整的压缩包后再试",
		"整合包里的清单损坏或格式不对，请重新下载整合包后再试",
		"找不到选择的服务器文件夹，请确认路径没有填错或被移动",
		"填写的运行时路径不可用，请重新选择或核对位置",
		"备份原有启动脚本失败，请检查文件夹写入权限或解除文件占用",
		"读取原有配置文件失败，请检查文件权限或是否被占用",
		"服务器存放文件夹不可用，请检查路径权限或更换位置",
		"服务端安装器运行失败，请切换下载源重试或查看安装日志",
		"生成备份压缩包失败，请检查备份盘剩余空间和写入权限",
		"还原前准备或自动备份失败，请检查磁盘空间和文件占用后再试",
		"备份压缩包打不开或已损坏，请检查备份文件是否完整",
		"清理或解压世界文件失败，请确认没有其他程序占着文件",
		"移走旧文件失败，可能正被服务器或杀毒软件占用，停服后再试",
		"读取未装文件清单失败，请检查文件夹权限或解除程序占用",
		"未装文件清单格式损坏，可备份后删除该清单文件再重新导入",
		"导出文件夹不可用，请检查路径权限或重新选择一个文件夹",
		"读取世界文件夹失败，请检查目录权限或关闭占用它的程序",
		"读取或写入交接包失败，请检查磁盘剩余空间和文件占用",
		"解压交接包失败，请检查磁盘空间或确认压缩包完整后再试",
		"读取服务器主配置失败，请检查文件权限或是否被占用",
		"读取地图来源失败，请确认文件或文件夹还在且未被占用",
		"创建地图暂存记录失败，请检查服务器文件夹写入权限与空间",
		"读取或解压地图文件失败，请检查压缩包完整性与磁盘空间",
		"打包整合包失败，请检查内存或磁盘空间后重试",
		"扫描模组文件夹失败，请检查文件夹权限与文件是否被占用",
		"获取模组版本和前置失败，请检查网络后重试",
		"压缩皮肤资源包失败，请检查磁盘剩余空间后重试",
		"写入下载缓存失败，请检查程序文件夹权限与磁盘空间",
		"保存到目标文件夹失败，请检查写入权限与磁盘剩余空间",
		"读写整合包草稿失败，请检查程序文件夹权限与磁盘空间",
		"穿透平台请求失败，请检查网络、账户授权或稍后再试",
		"自动下载穿透客户端失败，请检查网络或按详情说明手动放入",
		"穿透客户端异常，请查看详情说明并处理文件夹里的程序后重试",
		"启动穿透客户端失败，可能被安全软件拦截，请查看详情说明",
		"替换伙伴模组文件失败，可能正被占用，请先停止服务器再试",
		"下载伙伴前置模组失败，请检查网络后重试",
		"读写伙伴模组配置失败，请检查服务器文件夹权限与磁盘空间",
		"替换语音桥接模组失败，可能正被占用，请先停止服务器再试",
		"找不到所选的播放设备，可能已拔掉或禁用，请重新选择音源",
		"语音地址格式不对，请填写域名或地址加端口号",
		"拉取公开价格表失败，请检查网络，或直接填写自填单价",
		"保存设置或密钥失败，请检查程序文件夹的写入权限",
		"保存的密钥无法解开，可能换了电脑或系统账户，请重新填写",
		"保存接口设置或令牌失败，请检查程序文件夹的写入权限",
	}
	for _, h := range hints {
		AssertValidUIHint(t, h)
	}
}

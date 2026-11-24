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
	"runtime"
	"strings"
	"syscall"
	"testing"

	"minedeck/internal/errhint/hintcheck"
)

// AssertValidUIHint 校验单条提示是否符合前端 errSummary 的全部展示要求。
func AssertValidUIHint(t *testing.T, hint string) {
	t.Helper()
	if err := hintcheck.CheckUIHint(hint); err != nil {
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

	// 用户取消时 Wrap 应原样返回，不套任何提示。
	for _, cerr := range []error{context.Canceled, fmt.Errorf("下载：%w", context.Canceled)} {
		got := Wrap(cerr, "请检查网络")
		if got != cerr {
			t.Fatalf("Wrap 遇到 context.Canceled 应原样返回：%v", got)
		}
		var hh interface{ Hint() string }
		if errors.As(got, &hh) {
			t.Fatalf("取消错误不应带 Hint：%q", hh.Hint())
		}
	}
}

type fakeHostAddr string

func (a fakeHostAddr) Network() string { return "tcp" }
func (a fakeHostAddr) String() string  { return string(a) }

func TestFromErrorBranchesAndUICompat(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"permission", fmt.Errorf("写配置失败：%w", os.ErrPermission), "权限"},
		{"zip_corrupt", fmt.Errorf("打开失败：%w", zip.ErrFormat), "压缩包损坏"},
		{"tls_cert", &tls.CertificateVerificationError{}, "安全连接握手失败"},
		{"tls_prefix", errors.New("tls: bad certificate"), "安全连接握手失败"},
		{"x509_prefix", errors.New("x509: certificate signed by unknown authority"), "安全连接握手失败"},
		{"net_timeout", timeoutErr{}, "网络请求超时"},
		{"dns", &net.DNSError{Err: "no such host", Name: "example.com"}, "连不上远端服务器"},
		{"url_dial", &url.Error{Op: "Get", URL: "https://a.invalid", Err: &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.ParseIP("93.184.216.34"), Port: 443}, Err: errors.New("connection refused")}}, "连不上远端服务器"},
	}
	for _, tc := range cases {
		got := FromError(tc.err)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want substring %q", tc.name, got, tc.want)
		}
		AssertValidUIHint(t, got)
	}
	if strings.Contains(FromError(timeoutErr{}), "切换下载源") {
		t.Fatal("超时兜底不应再提切换下载源")
	}
	if runtime.GOOS == "windows" {
		got := FromError(fmt.Errorf("写模组：%w", syscall.Errno(1224)))
		if !strings.Contains(got, "占用") {
			t.Fatalf("Windows ERROR_USER_MAPPED_FILE(1224) 应提示被占用，got %q", got)
		}
		AssertValidUIHint(t, got)
	}

	emptyCases := []struct {
		name string
		err  error
	}{
		{"plain_cn", errors.New("请先停止服务器再删除")},
		{"url_dial_canceled", &url.Error{Op: "Get", URL: "https://a.invalid", Err: &net.OpError{Op: "dial", Err: context.Canceled}}},
		{"unexpected_eof", fmt.Errorf("读取响应：%w", io.ErrUnexpectedEOF)},
		{"op_listen", &net.OpError{Op: "listen", Net: "tcp", Addr: &net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 25565}, Err: errors.New("address already in use")}},
		{"loopback_v4", &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}, Err: errors.New("connection refused")}},
		{"loopback_v6", &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 8080}, Err: errors.New("connection refused")}},
		{"loopback_host", &net.OpError{Op: "dial", Net: "tcp", Addr: fakeHostAddr("localhost:8080"), Err: errors.New("connection refused")}},
		{"loopback_timeout", &net.OpError{Op: "read", Net: "tcp", Addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}, Err: timeoutErr{}}},
		{"bare_deadline", context.DeadlineExceeded},
		{"wrapped_deadline", fmt.Errorf("请求：%w", context.DeadlineExceeded)},
		{"url_plain", &url.Error{Op: "Get", URL: "https://a.invalid", Err: errors.New("unexpected status")}},
		{"tls_nospace", errors.New("my-tls:not-handshake")},
		{"x509_nospace", errors.New("my-x509:not-cert")},
	}
	for _, tc := range emptyCases {
		if got := FromError(tc.err); got != "" {
			t.Errorf("%s: 不应给出兜底提示，实际返回 %q", tc.name, got)
		}
	}
	if !errors.Is(&fs.PathError{Op: "open", Path: "x", Err: fs.ErrPermission}, fs.ErrPermission) {
		t.Fatal("fs.ErrPermission sanity")
	}
}

func TestAllPackageStaticHintsValidForUI(t *testing.T) {
	hints := []string{
		"D 盘剩余空间不足，请清理磁盘空间或更换存放文件夹后再试",
		"磁盘剩余空间不足，请清理磁盘空间或更换存放文件夹后再试",
		"请检查网络，或在「设置 → 下载与网络」切换下载源",
		"可在「设置 → 下载与网络」切换下载源，或手动添加 Java",
		"Java 已装好，但临时标记没删掉，请检查文件夹权限",
		"这个路径下的 Java 运行不了，请确认选的 Java 完整可用",
		"请检查网络后重试，或直接去发布页查看",
		"请检查网络后重试，或去发布页手动下载",
		"打不开这个整合包，请确认是完整的压缩包后再试",
		"打不开这个压缩包，请确认是完整的压缩包后再试",
		"整合包里的清单损坏或格式不对，请重新下载整合包后再试",
		"请确认选择的服务器文件夹路径没有填错或被移动",
		"填写的 Java 路径不可用，请重新选择或核对位置",
		"可以在「设置 → 下载与网络」切换下载源重试，或查看安装日志",
		"请检查所选 Java 能不能用，或者是不是被安全软件拦住了",
		"备份压缩包打不开或已损坏，请检查备份文件是否完整",
		"这份备份可能损坏或没写完整，请检查备份文件",
		"「没装上的文件」清单格式坏了，可删除该清单后按需手动补装",
		"交接包可能损坏或没写完整，请重新获取后再试",
		"地图压缩包可能损坏，没有替换任何文件",
		"地图压缩包里文件过多或清单超限，请检查压缩包",
		"找不到选择的地图压缩包，请重新选择后再试",
		"地图压缩包打不开或可能已损坏，请确认压缩包完整后重试",
		"找不到选择的地图文件夹，请确认它没有被移动或删除",
		"平台登录已失效，请重新连接这个平台账户",
		"请检查网络，或点「详情」按说明手动放入客户端",
		"客户端异常，请点「详情」或「复制原文」查看处理",
		"可能被安全软件拦截，请点「详情」或查看日志确认",
		"所选播放设备可能已拔掉或禁用，请重新选择音源",
		"请填写域名或地址，不带「http://」，可以带端口",
		"端口要在 1 到 65535 之间",
		"请检查网络，或直接填写自填单价",
		"密钥存储文件坏了，重新填写并保存即可自动重建",
		"保存的密钥无法解开，可能换了电脑或系统账户，请重新填写",
	}
	for _, h := range hints {
		AssertValidUIHint(t, h)
	}
}

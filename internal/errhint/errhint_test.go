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

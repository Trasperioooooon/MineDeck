package errhint

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"io/fs"
	"net"
	"net/url"
	"strings"
)

type hintedError struct {
	err  error
	hint string
}

func (e *hintedError) Error() string { return e.err.Error() }
func (e *hintedError) Unwrap() error { return e.err }
func (e *hintedError) Hint() string  { return e.hint }

// Wrap 为错误附上一句给界面展示的中文修复提示；已有非空 Hint 或用户取消时原样返回，原文和错误链保留。
func Wrap(err error, hint string) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return err
	}
	var h interface{ Hint() string }
	if errors.As(err, &h) && h.Hint() != "" {
		return err
	}
	return &hintedError{err: err, hint: hint}
}

// FromError 在错误链没有显式 Hint() 时，依据底层标准错误补充简短中文修复提示；认不出或用户取消返回空串。
func FromError(err error) string {
	if err == nil {
		return ""
	}
	var h interface{ Hint() string }
	if errors.As(err, &h) {
		if s := strings.TrimSpace(h.Hint()); s != "" {
			return s
		}
	}
	if errors.Is(err, context.Canceled) {
		return ""
	}
	if isDiskFull(err) {
		return "磁盘剩余空间不足，请清理磁盘空间或更换存放位置后再试"
	}
	if isFileBusy(err) {
		return "文件正被其他程序占用，请先停止服务器或关闭相关程序再试"
	}
	if errors.Is(err, fs.ErrPermission) {
		return "没有读写权限，请检查文件夹权限或关闭占用它的程序"
	}
	if errors.Is(err, zip.ErrFormat) || errors.Is(err, zip.ErrChecksum) || errors.Is(err, zip.ErrAlgorithm) ||
		errors.Is(err, gzip.ErrChecksum) || errors.Is(err, gzip.ErrHeader) {
		return "压缩包损坏或没有下载完整，请重新下载后再试"
	}
	var certErr *tls.CertificateVerificationError
	var recErr tls.RecordHeaderError
	msg := err.Error()
	if errors.As(err, &certErr) || errors.As(err, &recErr) || strings.Contains(msg, "x509: ") || strings.Contains(msg, "tls: ") {
		return "安全连接握手失败，请检查系统时间、代理或网络环境"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne != context.DeadlineExceeded && ne.Timeout() && !isListenOpErr(err) && !isLoopbackErr(err) {
		return "网络请求超时，请检查网络或代理后再试"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && !isLoopbackHost(dnsErr.Name) {
		return "连不上远端服务器，请检查网络、代理或稍后再试"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" && !isLoopbackAddr(opErr.Addr) && !isLoopbackURL(err) {
		return "连不上远端服务器，请检查网络、代理或稍后再试"
	}
	return ""
}

func isListenOpErr(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "listen"
}

func isLoopbackErr(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) && isLoopbackAddr(opErr.Addr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && isLoopbackHost(dnsErr.Name) {
		return true
	}
	return isLoopbackURL(err)
}

func isLoopbackURL(err error) bool {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return false
	}
	u, perr := url.Parse(urlErr.URL)
	if perr != nil {
		return false
	}
	return isLoopbackHost(u.Hostname())
}

func isLoopbackAddr(addr net.Addr) bool {
	if addr == nil {
		return false
	}
	switch a := addr.(type) {
	case *net.TCPAddr:
		return a.IP != nil && a.IP.IsLoopback()
	case *net.UDPAddr:
		return a.IP != nil && a.IP.IsLoopback()
	case *net.IPAddr:
		return a.IP != nil && a.IP.IsLoopback()
	}
	s := addr.String()
	if host, _, err := net.SplitHostPort(s); err == nil {
		return isLoopbackHost(host)
	}
	return isLoopbackHost(s)
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

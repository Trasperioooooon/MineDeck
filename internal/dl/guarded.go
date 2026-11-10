package dl

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

// AddrIsPublic 地址是否公网：回环、内网（RFC1918 / RFC4193）、链路本地、组播、
// CGNAT（100.64.0.0/10）和未指定地址一律不算。
// 取舍（评审确认过）：0.0.0.0/8 里除 0.0.0.0 本体、240.0.0.0/4、192.0.2.0/24 和
// 192.88.99.0/24 这几段文档保留范围 Go 判作全局单播，这里不再单列——它们要么
// 不可路由、要么只在 NAT64 环境里出现；把回环、内网、链路本地（云元数据 169.254.169.254
// 在这一段）和 CGNAT 挡住是这条防线的目标。
func AddrIsPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	if addr.Is4() {
		b := addr.As4()
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return false // CGNAT
		}
	}
	return addr.IsGlobalUnicast()
}

// controlPublic 网络连接建立前的最后一道门：Dialer 在每次解析出地址、真正连接之前
// 都会调它。只放行公网地址——之前的检查（记录时的设定、下载前的设定）与真正的连接
// 之间 DNS 可能变（TTL 极短的 rebinding、域名过期被重指向内网），这里补上连接级的一次。
func controlPublic(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("已拦截地址不明的连接: %s", address)
	}
	if !AddrIsPublic(addr) {
		return fmt.Errorf("已拒绝连接非公网地址 %s（只下载公网站点）", address)
	}
	return nil
}

// GuardedClient 整合包文件下载用的受控客户端（SSRF 防线的执行层）：
//   - 拨号级公网检查：每个解析出来的 IP 连接前过 AddrIsPublic，记忆里的信任站点
//     日后被解析到内网也连不出去；
//   - 重定向级检查：跳转目标是 https 且主机在 allowed 里（Modrinth/GitHub/GitLab 或
//     用户确认过的自定义站点）才跟，否则中断——自定义站点借 302 跳到内网端点走不通。
//
// allowed 为 nil 时只按拨号级公网检查放行（重定向到任何 https 公网主机都跟）。
func GuardedClient(allowed func(host string) bool) *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, Control: controlPublic}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		MaxIdleConnsPerHost:   8,
		DialContext:           dialer.DialContext,
	}
	return guardedRedirect(allowed, tr)
}

// GuardedClientWithTransport 同 GuardedClient，但换掉传输层（测试注入用：
// RoundTripper 直接给响应、不真正联网，拨号级检查自然不会发生）。
func GuardedClientWithTransport(allowed func(host string) bool, rt http.RoundTripper) *http.Client {
	return guardedRedirect(allowed, rt)
}

func guardedRedirect(allowed func(host string) bool, rt http.RoundTripper) *http.Client {
	return &http.Client{
		Transport: rt,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			host := strings.ToLower(strings.TrimSuffix(req.URL.Hostname(), "."))
			if req.URL.Scheme != "https" || host == "" {
				return fmt.Errorf("重定向到不允许的地址: %s", req.URL)
			}
			if allowed != nil && !allowed(host) {
				return fmt.Errorf("重定向到允许列表之外的站点 %s，已中断", host)
			}
			return nil
		},
	}
}

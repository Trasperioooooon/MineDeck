package dl

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
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
// 198.18.0.0/15 也算公网：Clash、sing-box 的 fake-ip 模式把所有域名都解析到这一段，
// 挡掉它，开着这类代理的人一个文件都下不了。
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

// maxRedirects 一次下载最多跟几次跳转（和 net/http 默认的上限一样；自己写了 CheckRedirect 就得自己数）。
const maxRedirects = 10

// GuardedClient 整合包文件下载用的受控客户端（SSRF 防线的执行层）。整合包里的下载地址是别人写的，
// 用户确认过的自定义站点还能随便跳转，所以每一跳都要保证连出去的是公网：
//
//   - 直连时在拨号那一步查：每个解析出来的 IP 连接前过 AddrIsPublic，查的就是真正要连的那个地址，
//     信任过的站点日后被解析到内网、或者借 302 跳到内网端点，都连不出去。
//   - 环境变量里配了代理（HTTP_PROXY / HTTPS_PROXY）时，连的是代理、目标地址由代理去解析，拨号那一步
//     查不到目标。退一步按主机名查：IP 字面量必须是公网，localhost 不行，本机能解析出来的地址里
//     有一个不是公网就不连；本机解析不出来的放行（开代理的人本机 DNS 常常是坏的）。known 说了算的
//     主机（内置的 Modrinth、GitHub、GitLab）不做这一步：这几个域名在国内常被污染成 127.0.0.1 或 0.0.0.0，
//     按本机的解析结果拦下来反而下不了。
//   - 跳转只跟 https 的，最多 maxRedirects 次。跳到哪个主机不限：GitHub 的发布文件会跳到
//     release-assets.githubusercontent.com，CurseForge 的会跳到 mediafilez.forgecdn.net，这些 CDN 域名
//     会换，列不全；跳转目标一样要过上面两条公网检查，下载内容还要过整合包给的校验值。
func GuardedClient(known func(host string) bool) *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, Control: controlPublic}
	return guardedRedirect(&guardedTransport{
		direct: &http.Transport{
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       90 * time.Second,
			DialContext:           dialer.DialContext,
		},
		proxied: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       90 * time.Second,
		},
		proxyFor: http.ProxyFromEnvironment,
		lookup:   lookupAddrs,
		known:    known,
	})
}

// GuardedClientWithTransport 同 GuardedClient，但换掉传输层（测试注入用：
// RoundTripper 直接给响应、不真正联网，公网检查自然不会发生；跳转的规矩还在）。
func GuardedClientWithTransport(rt http.RoundTripper) *http.Client {
	return guardedRedirect(rt)
}

func guardedRedirect(rt http.RoundTripper) *http.Client {
	return &http.Client{
		Transport: rt,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("跳转超过 %d 次，已中断", maxRedirects)
			}
			if req.URL.Scheme != "https" || req.URL.Hostname() == "" {
				return fmt.Errorf("重定向到不允许的地址: %s", req.URL)
			}
			return nil
		},
	}
}

// guardedTransport 按「这一个请求走不走环境代理」分两条路（见 GuardedClient 的说明）。
// 跳转的每一跳都是一次新的 RoundTrip，所以每一跳都会重新判断、重新检查。
type guardedTransport struct {
	direct   http.RoundTripper // 直连，拨号级查公网
	proxied  http.RoundTripper // 走环境代理，不查拨号地址（那是代理的地址）
	proxyFor func(*http.Request) (*url.URL, error)
	lookup   func(ctx context.Context, host string) ([]netip.Addr, error)
	known    func(host string) bool
}

func (g *guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if u, err := g.proxyFor(req); err != nil || u == nil {
		return g.direct.RoundTrip(req) // 代理配置读不懂时也直连：拨号级检查是更严的那一条
	}
	if err := g.checkProxiedHost(req.Context(), req.URL.Hostname()); err != nil {
		return nil, err
	}
	return g.proxied.RoundTrip(req)
}

// checkProxiedHost 走代理时按主机名判断目标是不是公网（规则见 GuardedClient 的说明）。
func (g *guardedTransport) checkProxiedHost(ctx context.Context, host string) error {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return errors.New("下载地址里没有主机名")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !AddrIsPublic(addr) {
			return fmt.Errorf("已拒绝连接非公网地址 %s（只下载公网站点）", host)
		}
		return nil
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("已拒绝连接本机地址 %s（只下载公网站点）", host)
	}
	if g.known != nil && g.known(host) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := g.lookup(ctx, host)
	if err != nil {
		return nil // 本机解析不出来：交给代理去解析
	}
	for _, a := range addrs {
		if !AddrIsPublic(a) {
			return fmt.Errorf("站点 %s 解析到非公网地址 %s，已拒绝连接（只下载公网站点）", host, a)
		}
	}
	return nil
}

func lookupAddrs(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

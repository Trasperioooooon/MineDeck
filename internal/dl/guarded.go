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

// reserved 标准库的 IsPrivate / IsLoopback 这些不管、但同样不是公网的地址段：CGNAT、「本网络」、
// 协议和文档保留段、保留未分配的 240/4、IPv6 的站点本地（已废弃）和文档段、Teredo。
var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("fec0::/10"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2001::/32"),
}

// 里面套着一个 IPv4 地址的 IPv6 段：NAT64（64:ff9b::/96，末 4 字节）、6to4（2002::/16，第 3–6 字节）、
// IPv4 兼容地址（::/96，末 4 字节，已废弃）。在有对应网关的网络里，连这种地址就是连里面那个 IPv4，
// 所以要把里面的地址取出来再判一次；里面是公网地址的照常放行（只有 IPv6 的网络靠 NAT64 访问 IPv4 的网站）。
var (
	nat64      = netip.MustParsePrefix("64:ff9b::/96")
	nat64Local = netip.MustParsePrefix("64:ff9b:1::/48") // 本地用的 NAT64 段：前缀长度不固定，取不准里面的地址，整段不算公网
	sixToFour  = netip.MustParsePrefix("2002::/16")
	v4Compat   = netip.MustParsePrefix("::/96")
)

// AddrIsPublic 地址是否公网：回环、内网（RFC1918 / RFC4193）、链路本地（云元数据 169.254.169.254 在这一段）、
// 组播、未指定地址，以及 reserved 里的保留段一律不算；套着 IPv4 的 IPv6 地址按里面的 IPv4 算。
// 198.18.0.0/15 算公网：Clash、sing-box 的 fake-ip 模式把所有域名都解析到这一段，
// 挡掉它，开着这类代理的人一个文件都下不了。
func AddrIsPublic(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, p := range reserved {
		if p.Contains(addr) {
			return false
		}
	}
	if addr.Is6() {
		b := addr.As16()
		switch {
		case nat64Local.Contains(addr):
			return false
		case nat64.Contains(addr), v4Compat.Contains(addr):
			return AddrIsPublic(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
		case sixToFour.Contains(addr):
			return AddrIsPublic(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}))
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
//     查不到目标。退一步按主机名查：IP 字面量必须是公网，从名字就看得出是本机、内网的不连（见 notPublicName），
//     本机能解析出来的地址里有一个不是公网就不连；本机解析不出来的放行（开代理的人本机 DNS 常常是坏的）。
//     known 说了算的主机（内置的 Modrinth、GitHub、GitLab）不做解析这一步：这几个域名在国内常被污染成
//     127.0.0.1 或 0.0.0.0，按本机的解析结果拦下来反而下不了。
//     这条路挡不住的：一个正常的公网域名，本机解析出来是公网地址，代理那头解析出来却是内网地址
//     （代理自己去解析，这边管不了）。能拿到的也只是一次看不到回应内容的 GET：内容对不上校验值不会留下。
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
	if why := notPublicName(host); why != "" {
		return fmt.Errorf("已拒绝连接 %s：%s（只下载公网站点）", host, why)
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

// privateSuffixes 只在本机或内网里有意义的域名后缀。
var privateSuffixes = []string{".localhost", ".local", ".lan", ".internal", ".intranet", ".corp", ".home", ".home.arpa", ".localdomain"}

// notPublicName 从名字本身就看得出不是公网站点的主机名（已转小写、去掉结尾的点、不是标准写法的 IP），返回原因；看不出来返回空串。
// 走代理时名字是原样交给代理去解析的，本机查一遍说明不了什么，所以先把这几类名字挡在前面：
//   - 没有点的名字（localhost、intranet、路由器的名字）：公网域名至少有两段。
//   - 最后一段是数字或 0x 开头：公网的顶级域没有这样的，这是 IP 的另一种写法（2130706433、0x7f.1、0177.0.0.1、127.1），
//     Go 不认它是 IP，可代理那头的系统多半认，连过去就是 127.0.0.1。
//   - 内网专用的后缀。
func notPublicName(host string) string {
	i := strings.LastIndexByte(host, '.')
	if i < 0 {
		return "这不是一个公网域名"
	}
	last := host[i+1:]
	if strings.HasPrefix(last, "0x") || strings.Trim(last, "0123456789") == "" {
		return "这是 IP 地址的另一种写法"
	}
	for _, suf := range privateSuffixes {
		if strings.HasSuffix(host, suf) {
			return "这是内网用的域名"
		}
	}
	return ""
}

func lookupAddrs(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

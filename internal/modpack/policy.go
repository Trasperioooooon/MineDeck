package modpack

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"minedeck/internal/dl"
)

// 整合包文件「无法自动下载」的原因（见 PendingFile.Reason）。
const (
	// ReasonHost 下载地址是 https、但主机不在 MineDeck 允许的站点列表里；
	// 用户可以在导入完成后逐个文件确认「信任这个站点」再下载，见 internal/inst 的待决处置。
	ReasonHost = "host"
	// ReasonNoHash 整合包没有给这个文件的 SHA-1 校验值：下载下来也没法核对是不是原文件。
	ReasonNoHash = "nohash"
	// ReasonBadURL 下载地址一条 https 的都没有（或根本没有下载地址）：任何情况下都不允许下载，
	// 只能由用户保留本地已有的文件、手动指定本地文件或跳过。
	ReasonBadURL = "badurl"
)

// PendingFile 解析时就判定为「无法自动下载」的整合包文件（见 Pack.Pending）。
// 之前只要有一项这样的文件就拒绝整个整合包；现在这些文件逐项列成待决清单，
// 由导入完成后的处置面板交给用户决定（internal/inst 的 resolveImportPending）。
type PendingFile struct {
	Path   string   // 相对实例目录（如 mods/xxx.jar）
	SHA1   string   // 整合包声明的 SHA-1（可能为空）
	URLs   []string // https 的候选地址（信任主机之后可以从中下载；badurl 时为空）
	Size   int64
	Reason string // host | nohash | badurl
	Note   string // 给用户看的一句话；空着就按 Reason 给通用的（PendingNote）
}

// AllowedHosts 内置允许的下载主机：Modrinth 整合包规范允许的托管站，
// 防用户构造的 .mrpack 借直链 SSRF 打内网 / 云元数据端点。自定义主机只能经用户逐个确认后
// 记进全局配置（app.Config.TrustedHosts，作用域 modpack），见 HostIsPublic 和处置流程。
var modrinthHosts = map[string]bool{
	"cdn.modrinth.com": true, "github.com": true, "raw.githubusercontent.com": true,
	"objects.githubusercontent.com": true, "gitlab.com": true,
}

// IsAllowedHost 主机是否允许自动下载：内置白名单，或 trusted 里登记过的自定义主机。
func IsAllowedHost(host string, trusted func(host string) bool) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(host, "."), ":443"))
	if modrinthHosts[host] {
		return true
	}
	return trusted != nil && trusted(host)
}

// AllowedURL 检查一个下载地址能不能自动下载：必须 https，主机要在内置白名单或用户逐个确认过的
// 信任列表里。返回规范化的主机名（小写、去尾部点）。trusted 为 nil 时只认内置白名单。
func AllowedURL(raw string, trusted func(host string) bool) (host string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return "", false
	}
	h := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return h, IsAllowedHost(h, trusted)
}

// httpsHost 取一个地址的规范主机名，并要求它是 https（不检查白名单）。
// 信任自定义主机的入口用它先把待决文件里的候选地址过一遍：非 https 的永远拿不到下载资格。
func httpsHost(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return "", false
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), ".")), true
}

// classifyURLs 把一个 mrpack 文件的下载地址分成「现在就能下载的」和「https 但主机未确认的」。
// 返回的 https 主机都除掉了非 https 的地址：那些不能靠任何一次确认变成可下载。
func classifyURLs(urls []string, trusted func(host string) bool) (allowed, pending []string) {
	for _, raw := range urls {
		if host, ok := httpsHost(raw); ok {
			if IsAllowedHost(host, trusted) {
				allowed = append(allowed, strings.TrimSpace(raw))
			} else {
				pending = append(pending, strings.TrimSpace(raw))
			}
		}
	}
	return allowed, pending
}

// Reclassify 把用户已确认过（trusted）的自定义站点的待决文件移回可下载清单：
// 导入启动时带着已有的信任记忆重新过一遍，同一站点的第二个整合包不再列待决。
func (p *Pack) Reclassify(trusted func(host string) bool) {
	keep := p.Pending[:0:0]
	for _, pf := range p.Pending {
		allowed, _ := classifyURLs(pf.URLs, trusted)
		if pf.Reason == ReasonHost && len(allowed) > 0 {
			p.Files = append(p.Files, File{Path: pf.Path, SHA1: pf.SHA1, URLs: allowed, Size: pf.Size})
			continue
		}
		keep = append(keep, pf)
	}
	p.Pending = keep
}

// HostIsPublic 判断主机是不是公网地址，用来挡把自定义下载地址指到本机、内网或云元数据端点的做法（SSRF）。
// IP 字面量直接查地址；域名把 DNS 解析出来的每一个 A/AAAA 记录都查一遍，任何一个是内网或不解析就整体拒绝。
// 授予信任和每次实际下载之前都要过这一关（连接级还有一道见 dl.GuardedClient，防查询与连接之间 DNS 变动）。
func HostIsPublic(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return dl.AddrIsPublic(addr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return false // 查不到就当不公网处理，宁可失败也不放开
	}
	for _, a := range addrs {
		ip, ok := netip.AddrFromSlice(a.IP)
		if !ok || !dl.AddrIsPublic(ip) {
			return false
		}
	}
	return true
}

// HostOf 返回待决处置用得到的主机展示：一个文件候选地址里全部 https 主机的并集（去重、保序）。
func HostOf(urls []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range urls {
		if host, ok := httpsHost(raw); ok && !seen[host] {
			seen[host] = true
			out = append(out, host)
		}
	}
	return out
}

// PendingNote 每个待决原因给用户看的一句话（处置面板里显示）。
func PendingNote(reason string) string {
	switch reason {
	case ReasonHost:
		return "下载地址不在 MineDeck 允许的站点列表里（只允许 Modrinth、GitHub、GitLab），确认站点可信后才能下载"
	case ReasonNoHash:
		return "整合包没有提供这个文件的校验值，自动下载没法核对，不能用下载替代你手里的文件"
	case ReasonBadURL:
		return "下载地址不是 https（或没有下载地址），任何情况下都不会从不安全地址下载"
	}
	return "无法自动下载"
}

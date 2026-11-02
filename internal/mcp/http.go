package mcp

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// maxBody 一个请求体最多多大（改配置文件的整份内容最多 50 万字）。
const maxBody = 8 << 20

// 桥转发时加的请求头：transport 是 stdio（只用来显示）；X-MineDeck-Client 是 stdio 客户端在 initialize 里报的名字
// （URL 编码），MineDeck 重启过、认不出旧会话时拿它记操作记录。都是客户端自己报的，不当真。
const (
	HeaderBridge = "X-MineDeck-Bridge"
	HeaderClient = "X-MineDeck-Client"
)

// ServeHTTP Streamable HTTP 的 MCP 端点（/mcp）。依次检查：只收 POST；只收本机的连接；Host 和 Origin 必须是本机
// （防 DNS 重绑定）；MCP 接口打开着；Authorization: Bearer <MCP 令牌>。都通过了才解析请求体。
// 每个请求回一个 JSON（不开 SSE 流，也没有 GET 的事件流）；旧版握手时发一个 Mcp-Session-Id。
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeRPCError(w, http.StatusMethodNotAllowed, codeInvalidRequest, "MineDeck 的 MCP 接口只收 POST：不提供 GET 的事件流，也不用 DELETE 结束会话")
		return
	}
	if !loopbackAddr(r.RemoteAddr) {
		writeRPCError(w, http.StatusForbidden, CodeForbidden, "MineDeck 的 MCP 接口只接受这台电脑上的连接（局域网的设备不能用）")
		return
	}
	if !loopbackHost(r.Host) {
		writeRPCError(w, http.StatusForbidden, CodeForbidden, "Host 请求头不对：MCP 接口的地址要写 127.0.0.1 或 localhost")
		return
	}
	if o := r.Header.Get("Origin"); o != "" && !loopbackOrigin(o) {
		writeRPCError(w, http.StatusForbidden, CodeForbidden, "Origin 不对：MineDeck 的 MCP 接口不接受网页发来的请求")
		return
	}
	if !s.Enabled() {
		writeRPCError(w, http.StatusForbidden, CodeDisabled, msgDisabled)
		return
	}
	switch s.auth(r.Header.Get("Authorization")) {
	case authBroken:
		writeRPCError(w, http.StatusUnauthorized, CodeAuth, msgBroken)
		return
	case authMissing, authWrong:
		w.Header().Set("WWW-Authenticate", `Bearer realm="MineDeck MCP"`)
		writeRPCError(w, http.StatusUnauthorized, CodeAuth, msgNoToken)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeRPCError(w, http.StatusRequestEntityTooLarge, codeInvalidRequest, "请求体太大了（最多 8 MB）")
			return
		}
		writeRPCError(w, http.StatusBadRequest, codeParse, "读请求体失败："+err.Error())
		return
	}
	hint, _ := url.QueryUnescape(r.Header.Get(HeaderClient))
	in := inbound{ctx: r.Context(), stdio: r.Header.Get(HeaderBridge) != "", header: r.Header, session: r.Header.Get("Mcp-Session-Id"),
		hint: cleanName(hint, 40)}
	res := s.handle(body, in)
	if res.session != "" {
		w.Header().Set("Mcp-Session-Id", res.session)
	}
	if res.body == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(res.status)
	_, _ = w.Write(res.body)
}

// writeRPCError 请求体还没解析时的拒绝：JSON-RPC 的错误响应，不带 id（桥转发时会补上原来的 id）。
func writeRPCError(w http.ResponseWriter, status, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(encode(errReply(nil, code, msg, nil)))
}

// loopbackAddr 连接来自本机（回环地址）。
func loopbackAddr(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loopbackHost Host 是本机的名字：127.0.0.1、localhost、[::1]（端口不管）。别的名字可能是 DNS 重绑定。
func loopbackHost(h string) bool {
	host := h
	if x, _, err := net.SplitHostPort(h); err == nil {
		host = x
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loopbackOrigin Origin 是本机的网页（http(s)://127.0.0.1、localhost、[::1]，端口不管）。null 和别的站点一律不认。
func loopbackOrigin(o string) bool {
	u, err := url.Parse(o)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	return loopbackHost(u.Host)
}

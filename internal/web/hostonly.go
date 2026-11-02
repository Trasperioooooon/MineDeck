package web

import (
	"net"
	"net/http"
)

// isLoopbackReq 判断请求是否来自本机：「仅本机」的操作（弹系统对话框、开资源管理器、指定主机路径）只认它。
func isLoopbackReq(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}

// handlePickDir 弹出系统文件夹选择对话框，返回所选目录（取消则 path 为空）。仅本机可用。
func (s *Server) handlePickDir(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "host.pickdir", nil)
}

// handleOpenPath 在资源管理器中打开指定目录。仅本机可用。
func (s *Server) handleOpenPath(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "host.openpath", bodyArgs(r))
}

// handleOpenDir 在资源管理器打开实例目录或其白名单子目录（sub 查询参数）。仅本机可用。
func (s *Server) handleOpenDir(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.opendir", args(map[string]any{"inst": r.PathValue("name"), "sub": r.URL.Query().Get("sub")}))
}

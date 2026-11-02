package web

import "net/http"

func (s *Server) handleJavas(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "java.list", nil)
}

func (s *Server) handleJavaScan(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "java.scan", nil)
}

// handleJavaAdd 仅限本机：请求体 path 会被作为可执行文件运行（java -version）。
func (s *Server) handleJavaAdd(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "java.add", bodyArgs(r))
}

package web

import "net/http"

// AI 设置（设计文档 §4.5）：修改类的接口仅本机；局域网登录的设备只能看（响应里的 local 为 false）。
// Key 只写不读，任何响应都只给 keySet / keyBroken。没配置 AI 时测试连接、刷新价格返回「未启用」。

func (s *Server) handleAISettings(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "ai.settings.get", nil)
}

func (s *Server) handleAIProvider(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "ai.provider.set", bodyArgs(r))
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "ai.models.list", bodyArgs(r))
}

func (s *Server) handleAITest(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "ai.test", nil)
}

func (s *Server) handleAIQuota(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "ai.quota.set", bodyArgs(r))
}

func (s *Server) handleAIPrices(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "ai.prices.set", bodyArgs(r))
}

func (s *Server) handleAIPricesRefresh(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "ai.prices.refresh", nil)
}

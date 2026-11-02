package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// MCP 接口（设计文档 §15）：
//   - /mcp：外部 AI 工具连的 Streamable HTTP 端点，自己检查本机、Origin 和 MCP 令牌（不认网页令牌），见 internal/mcp；
//   - /api/mcp*：「设置 → AI → MCP 接口」用的设置接口，走操作层（仅人工，修改类仅本机）；
//   - /api/mcp/events：等确认的外部 AI 提案（SSE），只给本机，界面据此弹提示、在助手面板里显示确认卡片。
func (s *Server) mcpRoutes(mux *http.ServeMux) {
	if s.mcp == nil {
		return
	}
	// 分方法登记：不带方法的 "/mcp" 会和 "GET /" 冲突。GET、DELETE 由 MCP 端点自己回 405
	for _, m := range []string{"POST", "GET", "DELETE"} {
		mux.Handle(m+" /mcp", s.mcp)
	}
	mux.HandleFunc("GET /api/mcp", func(w http.ResponseWriter, r *http.Request) { s.do(w, r, "mcp.get", nil) })
	mux.HandleFunc("PUT /api/mcp", func(w http.ResponseWriter, r *http.Request) { s.do(w, r, "mcp.set", bodyArgs(r)) })
	mux.HandleFunc("POST /api/mcp/token", func(w http.ResponseWriter, r *http.Request) { s.do(w, r, "mcp.token.reset", nil) })
	mux.HandleFunc("GET /api/mcp/config", func(w http.ResponseWriter, r *http.Request) { s.do(w, r, "mcp.config", nil) })
	mux.HandleFunc("GET /api/mcp/events", s.handleMCPEvents)
}

// handleMCPEvents SSE：先推一份等确认的外部 AI 提案 {"type":"pending","items":[…]}，之后每次有变化（新提案、处理完、过期、
// 关掉 MCP）再推一份完整的。只给本机：外部 AI 的提案只能在主机上处理。
func (s *Server) handleMCPEvents(w http.ResponseWriter, r *http.Request) {
	if !callerOf(r).Local {
		writeErr(w, http.StatusForbidden, fmt.Errorf("外部 AI（MCP）的请求只能在运行 MineDeck 的那台电脑上处理"))
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, fmt.Errorf("streaming unsupported"))
		return
	}
	ch, cancel := s.mcp.Subscribe()
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func() bool {
		b, _ := json.Marshal(map[string]any{"type": "pending", "items": s.mcp.Pending()})
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send() {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if !send() {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

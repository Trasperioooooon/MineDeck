package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"minedeck/internal/mcp"
	"minedeck/internal/ops"
)

// 第三阶段：应用内 AI 助手（设计文档 §5、§9）和提案的确认接口（§3.3）。
//
// 助手的接口不走操作层（对话本身不是一个操作），但助手调用的工具、提案的确认和拒绝都经过操作层：
// 权限、确认流程和操作记录都在那里。局域网的设备要主机开着「局域网可用 AI 助手」才能用，而且只看得见
// 局域网开的会话、只能处理这些会话的提案。

func (s *Server) assistantRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/assistant", s.handleAssistantStatus)
	mux.HandleFunc("GET /api/assistant/sessions", s.handleAssistantList)
	mux.HandleFunc("POST /api/assistant/sessions", s.handleAssistantCreate)
	mux.HandleFunc("DELETE /api/assistant/sessions", s.handleAssistantClear)
	mux.HandleFunc("GET /api/assistant/sessions/{id}", s.handleAssistantGet)
	mux.HandleFunc("DELETE /api/assistant/sessions/{id}", s.handleAssistantDelete)
	mux.HandleFunc("POST /api/assistant/sessions/{id}/messages", s.handleAssistantSend)
	mux.HandleFunc("POST /api/assistant/sessions/{id}/context", s.handleAssistantContext)
	mux.HandleFunc("POST /api/assistant/sessions/{id}/stop", s.handleAssistantStop)
	mux.HandleFunc("POST /api/assistant/sessions/{id}/continue", s.handleAssistantContinue)
	mux.HandleFunc("GET /api/assistant/sessions/{id}/events", s.handleAssistantEvents)
	mux.HandleFunc("GET /api/assistant/sessions/{id}/sent", s.handleAssistantSent)

	mux.HandleFunc("GET /api/ops/proposals", s.handleProposals)
	mux.HandleFunc("POST /api/ops/proposals/{id}/confirm", s.handleProposalConfirm)
	mux.HandleFunc("POST /api/ops/proposals/{id}/reject", s.handleProposalReject)

	mux.HandleFunc("PUT /api/ai/assistant", s.handleAIAssistant)
}

// reply 写回结果或错误（状态码取自操作层的错误）。
func reply(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	writeJSON(w, v)
}

// body 解析请求体；空的请求体按 {} 算。
func body(r *http.Request, v any) error {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return ops.Errorf(400, "请求体不是合法的 JSON：%v", err)
	}
	return nil
}

func (s *Server) handleAssistantStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.asst.Status(callerOf(r)))
}

func (s *Server) handleAssistantList(w http.ResponseWriter, r *http.Request) {
	l, err := s.asst.List(callerOf(r))
	reply(w, l, err)
}

func (s *Server) handleAssistantCreate(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Inst string `json:"inst"`
	}
	if err := body(r, &b); err != nil {
		reply(w, nil, err)
		return
	}
	v, err := s.asst.Create(callerOf(r), b.Inst)
	reply(w, v, err)
}

func (s *Server) handleAssistantClear(w http.ResponseWriter, r *http.Request) {
	n, err := s.asst.Clear(callerOf(r))
	reply(w, map[string]int{"deleted": n}, err)
}

func (s *Server) handleAssistantGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.asst.Get(callerOf(r), r.PathValue("id"))
	reply(w, v, err)
}

func (s *Server) handleAssistantDelete(w http.ResponseWriter, r *http.Request) {
	reply(w, "ok", s.asst.Delete(callerOf(r), r.PathValue("id")))
}

func (s *Server) handleAssistantSend(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Text   string `json:"text"`
		Attach bool   `json:"attach"`
	}
	if err := body(r, &b); err != nil {
		reply(w, nil, err)
		return
	}
	reply(w, "ok", s.asst.Send(callerOf(r), r.PathValue("id"), b.Text, b.Attach))
}

func (s *Server) handleAssistantContext(w http.ResponseWriter, r *http.Request) {
	p, err := s.asst.Context(r.Context(), callerOf(r), r.PathValue("id"))
	reply(w, p, err)
}

func (s *Server) handleAssistantStop(w http.ResponseWriter, r *http.Request) {
	reply(w, "ok", s.asst.Stop(callerOf(r), r.PathValue("id")))
}

func (s *Server) handleAssistantContinue(w http.ResponseWriter, r *http.Request) {
	reply(w, "ok", s.asst.Continue(callerOf(r), r.PathValue("id")))
}

func (s *Server) handleAssistantSent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	text, err := s.asst.Sent(callerOf(r), r.PathValue("id"), q.Get("item"), q.Get("tool"))
	reply(w, map[string]string{"text": text}, err)
}

// handleAssistantEvents SSE：先推一份完整的会话，再推增量（流式文字、工具、卡片、状态）。
func (s *Server) handleAssistantEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, fmt.Errorf("streaming unsupported"))
		return
	}
	v, ch, cancel, err := s.asst.Subscribe(callerOf(r), r.PathValue("id"))
	if err != nil {
		writeErr(w, ops.CodeOf(err), err)
		return
	}
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	first, _ := json.Marshal(map[string]any{"type": "session", "session": v})
	if _, err := fmt.Fprintf(w, "data: %s\n\n", first); err != nil {
		return
	}
	fl.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b, ok := <-ch:
			if !ok {
				return // 跟不上被断开（或会话被删了）：浏览器重连时重新拿一份完整的会话
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			fl.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// canDecide 这个人能不能处理这个提案：助手的规则（局域网只能处理局域网会话里的），外部 AI（MCP）提出的只能在本机处理。
func (s *Server) canDecide(who ops.Caller, p *ops.Proposal) error {
	if err := s.asst.CanDecide(who, p); err != nil {
		return err
	}
	return mcp.CanDecide(who, p)
}

// handleProposals GET /api/ops/proposals?session= 还在等确认的提案（只列这个人能处理的）。
func (s *Server) handleProposals(w http.ResponseWriter, r *http.Request) {
	who := callerOf(r)
	out := []*ops.Proposal{}
	for _, p := range s.ops.Pending(r.URL.Query().Get("session")) {
		if s.canDecide(who, p) == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, out)
}

// handleProposalConfirm POST /api/ops/proposals/{id}/confirm {ack, consent}：人确认提案，按提案里存下的参数执行。
// 危险操作要带 ack（第二次确认）；要求勾选同意的（新建实例的 EULA）要带 consent。
func (s *Server) handleProposalConfirm(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Ack     bool `json:"ack"`
		Consent bool `json:"consent"`
	}
	if err := body(r, &b); err != nil {
		reply(w, nil, err)
		return
	}
	who := callerOf(r)
	if p := s.ops.Proposal(r.PathValue("id")); p != nil {
		if err := s.canDecide(who, p); err != nil {
			reply(w, nil, err)
			return
		}
	}
	res, err := s.ops.ConfirmWith(r.Context(), who, r.PathValue("id"), ops.ConfirmInput{Ack: b.Ack, Consent: b.Consent})
	reply(w, res, err)
}

// handleProposalReject POST /api/ops/proposals/{id}/reject {reason}：人拒绝提案，理由（可以不填）交还给 AI。
func (s *Server) handleProposalReject(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Reason string `json:"reason"`
	}
	if err := body(r, &b); err != nil {
		reply(w, nil, err)
		return
	}
	who := callerOf(r)
	if p := s.ops.Proposal(r.PathValue("id")); p != nil {
		if err := s.canDecide(who, p); err != nil {
			reply(w, nil, err)
			return
		}
	}
	reply(w, "ok", s.ops.Reject(who, r.PathValue("id"), b.Reason))
}

func (s *Server) handleAIAssistant(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "ai.assistant.set", bodyArgs(r))
}

// ---------- 第三阶段新增的实例接口（排错和修复用的读写能力，§3.5、§5.2） ----------

func (s *Server) troubleshootRoutes(mux *http.ServeMux) {
	q := func(r *http.Request, names ...string) map[string]any {
		m := map[string]any{"inst": r.PathValue("name")}
		for _, n := range names {
			v := r.URL.Query().Get(n)
			if v == "" {
				continue
			}
			switch n {
			case "lines", "start":
				if i, err := strconv.Atoi(v); err == nil {
					m[n] = i
				} else {
					m[n] = v // 交给操作层的参数校验去报错
				}
			case "full", "detail":
				m[n] = v == "1" || v == "true"
			default:
				m[n] = v
			}
		}
		return m
	}
	get := func(op string, names ...string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { s.do(w, r, op, args(q(r, names...))) }
	}
	mux.HandleFunc("GET /api/instances/{name}/diag", get("instance.diag"))
	mux.HandleFunc("GET /api/instances/{name}/console/tail", get("instance.console.tail", "lines", "level", "q", "since"))
	mux.HandleFunc("GET /api/instances/{name}/logs", get("instance.logs.read", "file", "lines", "q"))
	mux.HandleFunc("GET /api/instances/{name}/crash-reports", get("instance.crashes", "file", "full"))
	mux.HandleFunc("GET /api/instances/{name}/mods", get("instance.mods.list", "q", "detail"))
	mux.HandleFunc("GET /api/instances/{name}/config", get("instance.config.read", "path", "start", "lines"))
	mux.HandleFunc("PUT /api/instances/{name}/config", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "instance.config.write", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/instances/{name}/mods/toggle", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "instance.mods.toggle", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("PUT /api/instances/{name}/java", func(w http.ResponseWriter, r *http.Request) {
		s.do(w, r, "instance.java.set", bodyArgs(r, "inst", r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/instances/{name}/restart", s.instOp("instance.restart"))
	mux.HandleFunc("GET /api/ports/{port}", func(w http.ResponseWriter, r *http.Request) {
		port, err := strconv.Atoi(r.PathValue("port"))
		if err != nil {
			writeErr(w, 400, fmt.Errorf("端口不对"))
			return
		}
		s.do(w, r, "port.check", args(map[string]any{"port": port, "proto": r.URL.Query().Get("proto")}))
	})
}

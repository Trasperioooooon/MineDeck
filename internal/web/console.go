package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"minedeck/internal/procutil"
)

func (s *Server) handleTunnelConsole(w http.ResponseWriter, r *http.Request) {
	s.streamConsole(w, r, s.tun.Console(r.PathValue("id")))
}

// handleConsole 通过 SSE 推送实例控制台输出（先回放历史，再实时跟踪）。
func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	i, err := s.mgr.Get(r.PathValue("name"))
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	s.streamConsole(w, r, i.Console)
}

func (s *Server) streamConsole(w http.ResponseWriter, r *http.Request, con *procutil.Console) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	replay, ch, cancel := con.Subscribe()
	defer cancel()
	send := func(line string) bool {
		b, _ := json.Marshal(line)
		_, err := fmt.Fprintf(w, "data: %s\n\n", b)
		return err == nil
	}
	for _, l := range replay {
		if !send(l) {
			return
		}
	}
	fl.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case l := <-ch:
			if !send(l) {
				return
			}
			// 批量吸收积压行后统一 flush
			for drained := false; !drained; {
				select {
				case l = <-ch:
					if !send(l) {
						return
					}
				default:
					drained = true
				}
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

package web

import (
	"fmt"
	"net/http"

	"minedeck/internal/ops/catalog"
)

func (s *Server) handleGetPlayers(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.players.get", instArg(r))
}

// handlePlayerAction 按 action 分到不同的操作：加减白名单是写操作，op、踢人、封禁这些是危险操作。
func (s *Server) handlePlayerAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
		Player string `json:"player"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, err)
		return
	}
	name, ok := catalog.PlayerOps[body.Action]
	if !ok {
		writeErr(w, 400, fmt.Errorf("未知操作: %s", body.Action))
		return
	}
	s.do(w, r, name, args(map[string]any{"inst": r.PathValue("name"), "player": body.Player}))
}

package web

import "net/http"

func (s *Server) handleGetProps(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.props.get", instArg(r))
}

func (s *Server) handleSetProps(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.props.set", bodyArgs(r, "inst", r.PathValue("name")))
}

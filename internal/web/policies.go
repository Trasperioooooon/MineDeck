package web

import "net/http"

func (s *Server) handleGetPolicies(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.policies.get", instArg(r))
}

func (s *Server) handleSetPolicies(w http.ResponseWriter, r *http.Request) {
	s.do(w, r, "instance.policies.set", bodyArgs(r, "inst", r.PathValue("name")))
}

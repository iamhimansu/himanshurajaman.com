package server

import "net/http"

// handleHealthz is liveness: 200 as long as the process is up. It never touches
// SQLite or GitHub, so a slow/absent content source can't make the pinger think
// the service is dead.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz is readiness: 503 {"status":"warming"} until the first ingest
// completes, then 200 {"ready":true,"commit":"<sha>"}.
func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	ready, commit := s.readyState()
	if !ready {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "warming"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": true, "commit": commit})
}

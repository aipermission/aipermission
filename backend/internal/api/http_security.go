package api

import (
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/localhttp"
	"github.com/gorilla/websocket"
)

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			w.Header().Add("Vary", "Origin")
			if !s.isAllowedOrigin(origin) {
				if r.Method == http.MethodOptions {
					writeError(w, http.StatusForbidden, "origin is not allowed")
					return
				}
				writeError(w, http.StatusForbidden, "origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-AIPermission-CSRF")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) isAllowedOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	if !isLoopbackOrigin(origin) {
		return false
	}
	for _, allowed := range s.config.AllowedOrigins {
		if strings.EqualFold(strings.TrimSpace(allowed), origin) {
			return true
		}
	}
	return false
}

func isLoopbackOrigin(origin string) bool {
	return localhttp.IsLoopbackOrigin(origin)
}

func (s *Server) upgradeWebSocket(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return s.isAllowedOrigin(r.Header.Get("Origin"))
		},
	}
	return upgrader.Upgrade(w, r, nil)
}

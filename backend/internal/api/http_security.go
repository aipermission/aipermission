package api

import (
	"net/http"

	"github.com/gorilla/websocket"
)

func (s *Server) isAllowedOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	return s.config.AllowsOrigin(origin)
}

func (s *Server) upgradeWebSocket(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return s.isAllowedOrigin(r.Header.Get("Origin"))
		},
	}
	return upgrader.Upgrade(w, r, nil)
}

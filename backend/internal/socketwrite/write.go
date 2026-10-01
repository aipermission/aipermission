// Package socketwrite owns bounded, serialized writes for live local consoles.
// Callers own message content, client membership and session authorization.
package socketwrite

import (
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const Timeout = 2 * time.Second

var ErrUnavailable = errors.New("console websocket is unavailable")

func JSON(ws *websocket.Conn, writeMu *sync.Mutex, message any) error {
	if writeMu != nil {
		writeMu.Lock()
		defer writeMu.Unlock()
	}
	return JSONLocked(ws, message)
}

// JSONLocked requires exclusive writer ownership, including a caller-held
// snapshot fence. Its socket deadline starts after that ownership is acquired.
func JSONLocked(ws *websocket.Conn, message any) error {
	if ws == nil {
		return ErrUnavailable
	}
	if err := ws.SetWriteDeadline(time.Now().Add(Timeout)); err != nil {
		return err
	}
	return ws.WriteJSON(message)
}

func Control(ws *websocket.Conn, writeMu *sync.Mutex, kind int, data []byte) error {
	if writeMu != nil {
		writeMu.Lock()
		defer writeMu.Unlock()
	}
	if ws == nil {
		return ErrUnavailable
	}
	return ws.WriteControl(kind, data, time.Now().Add(Timeout))
}

// KeepAlive uses a positive, owner-configured interval. Closing failed sockets
// wakes their reader so the session owner can unregister them.
func KeepAlive(ws *websocket.Conn, writeMu *sync.Mutex, interval time.Duration, stop <-chan struct{}) {
	if ws == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := Control(ws, writeMu, websocket.PingMessage, nil); err != nil {
				_ = ws.Close()
				return
			}
		case <-stop:
			return
		}
	}
}

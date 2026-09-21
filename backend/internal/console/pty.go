package console

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type ptyClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

type ptyServerMessage struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	Status    string `json:"status,omitempty"`
	SessionID int64  `json:"session_id,omitempty"`
}

func writePTYMessage(ws *websocket.Conn, writeMu *sync.Mutex, message ptyServerMessage) error {
	if writeMu != nil {
		writeMu.Lock()
		defer writeMu.Unlock()
	}
	return ws.WriteJSON(message)
}

func writePTYControl(ws *websocket.Conn, writeMu *sync.Mutex, messageType int, deadline time.Time) error {
	if writeMu != nil {
		writeMu.Lock()
		defer writeMu.Unlock()
	}
	return ws.WriteControl(messageType, nil, deadline)
}

func keepPTYAlive(ws *websocket.Conn, writeMu *sync.Mutex, stop <-chan struct{}) {
	ticker := time.NewTicker(ptyPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := writePTYControl(ws, writeMu, websocket.PingMessage, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		case <-stop:
			return
		}
	}
}

type consoleIntervalLimiter struct {
	minInterval time.Duration
	last        time.Time
}

func newConsoleIntervalLimiter(minInterval time.Duration) *consoleIntervalLimiter {
	return &consoleIntervalLimiter{minInterval: minInterval}
}

func (l *consoleIntervalLimiter) allow() bool {
	if l == nil || l.minInterval <= 0 {
		return true
	}
	now := time.Now()
	if !l.last.IsZero() && now.Sub(l.last) < l.minInterval {
		return false
	}
	l.last = now
	return true
}

func (l *consoleIntervalLimiter) wait(ctx context.Context) error {
	if l == nil || l.minInterval <= 0 {
		return nil
	}
	if delay := time.Until(l.last.Add(l.minInterval)); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	l.last = time.Now()
	return nil
}

func parsePositiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

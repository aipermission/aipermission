package console

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/gorilla/websocket"
)

// Attach upgrades an authorized request to the interactive session transport.
func (m *Manager) Attach(w http.ResponseWriter, r *http.Request, principal executionprincipal.Principal, id int64, upgrade func(http.ResponseWriter, *http.Request) (*websocket.Conn, error)) error {
	session := m.active(id)
	if session == nil {
		record, err := m.Get(r.Context(), id)
		if err != nil {
			return ErrNotFound
		}
		return InactiveError{Status: record.Status, Detail: record.Error}
	}
	if err := m.authorizeOperation(r.Context(), principal, session, OperationAttach, nil); err != nil {
		return err
	}

	ws, err := upgrade(w, r)
	if err != nil {
		return err
	}
	defer ws.Close()
	ws.SetReadLimit(maxPTYClientMessageBytes)
	_ = ws.SetReadDeadline(time.Now().Add(ptyPongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(ptyPongWait))
	})

	var writeMu *sync.Mutex
	if err := m.authorizeOperation(r.Context(), principal, session, OperationObserve, func() error {
		var snapshotStatus, transcript string
		var registerErr error
		writeMu, snapshotStatus, transcript, registerErr = session.addClientWithSnapshot(ws)
		if registerErr != nil {
			return registerErr
		}
		writeErr := ws.WriteJSON(ptyServerMessage{
			Type: "snapshot", Status: snapshotStatus, Data: transcript, SessionID: session.id,
		})
		writeMu.Unlock()
		return writeErr
	}); err != nil {
		if writeMu != nil {
			session.removeClient(ws)
		}
		return err
	}
	defer session.removeClient(ws)
	stopPing := make(chan struct{})
	defer close(stopPing)
	go keepPTYAlive(ws, writeMu, stopPing)

	inputLimiter := newConsoleIntervalLimiter(ptyInputMinInterval)
	resizeLimiter := newConsoleIntervalLimiter(ptyResizeMinInterval)
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return nil
		}
		var message ptyClientMessage
		if err := json.Unmarshal(data, &message); err != nil {
			continue
		}
		switch message.Type {
		case "input":
			if len(message.Data) > maxConsoleInputBytes {
				_ = writePTYMessage(ws, writeMu, ptyServerMessage{Type: "error", Status: "error", Data: ErrInputTooLarge.Error(), SessionID: session.id})
				continue
			}
			if err := inputLimiter.wait(r.Context()); err != nil {
				return nil
			}
			if err := m.authorizeOperation(r.Context(), principal, session, OperationInput, func() error {
				return session.submitManualInput(message.Data)
			}); err != nil {
				_ = writePTYMessage(ws, writeMu, ptyServerMessage{Type: "error", Status: "error", Data: err.Error(), SessionID: session.id})
			}
		case "resize":
			if !resizeLimiter.allow() {
				continue
			}
			_ = m.authorizeOperation(r.Context(), principal, session, OperationInput, func() error {
				session.resize(message.Cols, message.Rows)
				return nil
			})
		}
	}
}

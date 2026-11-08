package gatewayconnectorapi

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
	"github.com/aipermission/aipermission/backend/internal/socketwrite"
)

// The normal authenticated socket upgrade releases the workspace read lease.
// This stream never reads the database or returns target/credential metadata.
func (h *LiveConsoleHTTPHandlers) CapacityAttach(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.resolve(w)
	if !ok {
		return
	}
	sessions, ok := runtime.Sessions.(LiveConsoleCapacity)
	if !ok || runtime.UpgradeWebSocket == nil {
		httptransport.WriteInternalError(w)
		return
	}
	ws, err := runtime.UpgradeWebSocket(w, r)
	if err != nil {
		return
	}
	defer ws.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var writeMu sync.Mutex
	go socketwrite.KeepAlive(ws, &writeMu, 20*time.Second, ctx.Done())
	ws.SetReadLimit(1024)
	_ = ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	go func() {
		defer cancel()
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	var after uint64
	for {
		revision, err := sessions.WaitCapacity(ctx, after)
		if err != nil {
			return
		}
		if revision != after {
			if socketwrite.JSON(ws, &writeMu, map[string]any{"revision": revision}) != nil {
				return
			}
			after = revision
		}
	}
}

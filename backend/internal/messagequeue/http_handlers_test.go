package messagequeue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPHandlersPreserveStrictMessageContract(t *testing.T) {
	database := openTestDatabase(t)
	tokenID := insertTestToken(t, database)
	runtimeID := insertTestRuntime(t, database, "worker", "active", "live_console")
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{Store: NewStore(database, nil)}, true
	})

	created := requestJSON(t, handlers.Create, http.MethodPost, "/api/messages", CreateRequest{
		TokenID: tokenID, RuntimeID: &runtimeID, Message: "hello",
	})
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"message":"hello"`) {
		t.Fatalf("create response: %d %s", created.Code, created.Body.String())
	}

	listed := httptest.NewRecorder()
	handlers.List(listed, httptest.NewRequest(http.MethodGet, "/api/messages?runtime_id="+fmtID(runtimeID), nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"message":"hello"`) {
		t.Fatalf("list response: %d %s", listed.Code, listed.Body.String())
	}

	badQuery := httptest.NewRecorder()
	handlers.List(badQuery, httptest.NewRequest(http.MethodGet, "/api/messages?runtime_id=nope", nil))
	if badQuery.Code != http.StatusBadRequest || !strings.Contains(badQuery.Body.String(), "invalid runtime_id") {
		t.Fatalf("bad query response: %d %s", badQuery.Code, badQuery.Body.String())
	}

	unknown := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(`{"token_id":1,"message":"hello","unknown":true}`))
	request.Header.Set("Content-Type", "application/json")
	handlers.Create(unknown, request)
	if unknown.Code != http.StatusBadRequest || !strings.Contains(unknown.Body.String(), "invalid json body") {
		t.Fatalf("unknown field response: %d %s", unknown.Code, unknown.Body.String())
	}

	read := requestJSON(t, handlers.MarkRead, http.MethodPost, "/api/messages/read", MarkReadRequest{RuntimeID: runtimeID})
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"status":"read"`) {
		t.Fatalf("mark read response: %d %s", read.Code, read.Body.String())
	}
}

func TestHTTPHandlersFailClosedWithoutScope(t *testing.T) {
	handlers := NewHTTPHandlers(nil)
	response := httptest.NewRecorder()
	handlers.List(response, httptest.NewRequest(http.MethodGet, "/api/messages", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func requestJSON(t *testing.T, handler http.HandlerFunc, method, target string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func fmtID(id int64) string {
	return fmt.Sprintf("%d", id)
}

package gatewayoperations

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/messagequeue"
	"github.com/aipermission/aipermission/backend/internal/restcontract"
)

func TestMessageReadAcknowledgesOnlySelectedRuntimeAIRecords(t *testing.T) {
	database := openMessageTestDatabase(t)
	firstToken, secondToken := insertMessageTestToken(t, database), insertMessageTestToken(t, database)
	runtime, otherRuntime := insertMessageTestRuntime(t, database), insertMessageTestRuntime(t, database)
	store := messagequeue.NewStore(database, nil)
	insert := func(tokenID, runtimeID int64, direction string) int64 {
		t.Helper()
		item, err := store.Insert(t.Context(), messagequeue.CreateRequest{TokenID: tokenID, RuntimeID: &runtimeID, Direction: direction, Message: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		return item.ID
	}
	shown := insert(firstToken, runtime, "ai_to_user")
	hidden := insert(secondToken, runtime, "ai_to_user")
	other := insert(firstToken, otherRuntime, "ai_to_user")
	outbound := insert(firstToken, runtime, "user_to_ai")
	handlers := NewMessageHTTPHandlers(func(http.ResponseWriter) (MessageScope, bool) {
		return MessageScope{Store: NewMessageStore(database, func(_ context.Context, value string) string { return value })}, true
	})
	for _, want := range []int{1, 0} {
		response := performMessageRequest(t, handlers.MarkRead, http.MethodPost, "/api/messages/read", map[string]any{"runtime_id": runtime, "message_ids": []int64{shown, other, outbound}})
		var body struct {
			Count int `json:"count"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK || body.Count != want {
			t.Fatalf("selection ack=%d %s, want count %d: %v", response.Code, response.Body.String(), want, err)
		}
		if err := restcontract.ValidateTypedResponse(http.MethodPost, "/api/messages/read", response.Code, response.Body.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{hidden, other, outbound} {
		item, err := store.Get(t.Context(), id)
		if err != nil || item.ConsumedAt != nil {
			t.Fatalf("unseen or outbound message %d acknowledged: %#v %v", id, item, err)
		}
	}
}

func TestMessageReadRejectsUnboundedOrInvalidSelectionWithoutMutation(t *testing.T) {
	database := openMessageTestDatabase(t)
	token, runtime := insertMessageTestToken(t, database), insertMessageTestRuntime(t, database)
	store := messagequeue.NewStore(database, nil)
	item, err := store.Insert(t.Context(), messagequeue.CreateRequest{TokenID: token, RuntimeID: &runtime, Direction: "ai_to_user", Message: "unseen"})
	if err != nil {
		t.Fatal(err)
	}
	handlers := NewMessageHTTPHandlers(func(http.ResponseWriter) (MessageScope, bool) {
		return MessageScope{Store: NewMessageStore(database, func(_ context.Context, value string) string { return value })}, true
	})
	for name, ids := range map[string]any{"absent": nil, "empty": []int64{}, "zero": []int64{item.ID, 0}, "negative": []int64{-1}, "duplicate": []int64{item.ID, item.ID}, "too-many": make([]int64, 101), "fraction": []float64{1.5}} {
		t.Run(name, func(t *testing.T) {
			payload := map[string]any{"runtime_id": runtime, "message_ids": ids}
			if name == "absent" {
				delete(payload, "message_ids")
			}
			response := performMessageRequest(t, handlers.MarkRead, http.MethodPost, "/api/messages/read", payload)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid selection accepted: %d %s", response.Code, response.Body.String())
			}
			if err := restcontract.ValidateTypedResponse(http.MethodPost, "/api/messages/read", response.Code, response.Body.Bytes()); err != nil {
				t.Fatal(err)
			}
			current, err := store.Get(t.Context(), item.ID)
			if err != nil || current.ConsumedAt != nil {
				t.Fatalf("invalid selection mutated record: %#v %v", current, err)
			}
		})
	}
}

func TestMessageReadPreservesNotesOutsideTheBoundedLoadedWindow(t *testing.T) {
	database := openMessageTestDatabase(t)
	token, runtime := insertMessageTestToken(t, database), insertMessageTestRuntime(t, database)
	for range 101 {
		if _, err := database.Exec(`INSERT INTO message_queue (token_id, runtime_id, direction, message, created_at) VALUES (?, ?, 'ai_to_user', 'fixture', datetime('now'))`, token, runtime); err != nil {
			t.Fatal(err)
		}
	}
	store := messagequeue.NewStore(database, nil)
	items, err := store.List(t.Context(), messagequeue.Filter{RuntimeID: runtime})
	if err != nil || len(items) != 100 {
		t.Fatalf("bounded list=%d %v", len(items), err)
	}
	ids := make([]int64, len(items))
	for index, item := range items {
		ids[index] = item.ID
	}
	count, err := store.MarkRuntimeRead(t.Context(), runtime, ids)
	if err != nil || count != 100 {
		t.Fatalf("bounded read=%d %v", count, err)
	}
	var remaining int
	if err := database.QueryRow(`SELECT count(*) FROM message_queue WHERE consumed_at IS NULL`).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("unseen window consumed: remaining=%d %v", remaining, err)
	}
}

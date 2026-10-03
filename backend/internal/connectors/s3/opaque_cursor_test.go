package s3connector

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestS3OpaqueCursorSurvivesPublicSchemaAndProviderRoundTrip(t *testing.T) {
	for _, token := range []string{" +opaque%2F= ", " "} {
		t.Run(token, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					if r.URL.Query().Has("continuation-token") {
						t.Error("first request unexpectedly has a cursor")
					}
					_, _ = fmt.Fprintf(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>%s</NextContinuationToken><Contents><Key>first</Key></Contents></ListBucketResult>`, token)
					return
				}
				if calls != 2 || r.URL.Query().Get("continuation-token") != token {
					t.Errorf("provider received cursor %q on call %d; want %q", r.URL.Query().Get("continuation-token"), calls, token)
				}
				_, _ = w.Write([]byte(`<ListBucketResult><Contents><Key>second</Key></Contents></ListBucketResult>`))
			}))
			defer server.Close()
			var schema connectors.Schema
			for _, action := range objectActions() {
				if action.Name == ActionListObjects {
					schema = action.InputSchema
				}
			}
			if len(schema.Fields) == 0 {
				t.Fatal("public list_objects schema missing")
			}
			runtime := s3TestRuntime(t, server.URL)
			cursor := ""
			for page := 0; page < 2; page++ {
				input, err := connectors.NormalizeSchemaValues(schema, map[string]any{"cursor": cursor, "limit": 1})
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := New().PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, ActionName: ActionListObjects, Input: input})
				if err != nil || prepared.Payload["cursor"] != cursor {
					t.Fatalf("prepared cursor = %#v, want %q: %v", prepared.Payload["cursor"], cursor, err)
				}
				result, err := New().ExecuteAction(t.Context(), runtime, prepared)
				if err != nil {
					t.Fatal(err)
				}
				output := result.Output.(map[string]any)
				if page == 0 {
					cursor, _ = output["next_cursor"].(string)
					if cursor != token {
						t.Fatalf("returned cursor = %q, want %q", cursor, token)
					}
				} else if objects := output["objects"].([]map[string]any); len(objects) != 1 || objects[0]["key"] != "second" {
					t.Fatalf("second page = %#v", output)
				}
			}
			if calls != 2 {
				t.Fatalf("provider calls = %d, want 2", calls)
			}
		})
	}
}

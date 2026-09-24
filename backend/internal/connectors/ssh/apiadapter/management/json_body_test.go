package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func TestManagementJSONBodyIsBoundedAndSingleDocument(t *testing.T) {
	for _, test := range []struct {
		name   string
		body   string
		status int
	}{
		{name: "valid", body: `{"host":"example.com"}`, status: http.StatusOK},
		{name: "multiple documents", body: `{"host":"example.com"} {"host":"other"}`, status: http.StatusBadRequest},
		{name: "oversized", body: `{"host":"` + strings.Repeat("a", int(httptransport.DefaultJSONBodyBytes)) + `"}`, status: http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/ssh/test", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			var target struct {
				Host string `json:"host"`
			}
			ok := decodeJSON(response, request, &target)
			if test.status == http.StatusOK {
				if !ok || target.Host != "example.com" {
					t.Fatalf("ok=%t target=%#v", ok, target)
				}
				return
			}
			if ok || response.Code != test.status {
				t.Fatalf("ok=%t status=%d want=%d", ok, response.Code, test.status)
			}
		})
	}
}

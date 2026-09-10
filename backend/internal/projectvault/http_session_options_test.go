package projectvault

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type sessionOptionsCatalogStub struct {
	target       SessionOptionsTarget
	targetErr    error
	projects     []SessionOptionsProject
	projectsErr  error
	projectCalls int
}

func (s *sessionOptionsCatalogStub) ResolveSessionOptionsTarget(context.Context, int64) (SessionOptionsTarget, error) {
	return s.target, s.targetErr
}

func (s *sessionOptionsCatalogStub) ListSessionOptionsProjects(context.Context) ([]SessionOptionsProject, error) {
	s.projectCalls++
	return s.projects, s.projectsErr
}

func TestHTTPSessionOptionsUsesOwnerRuntimeAndCatalog(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	item := harness.create(t)
	catalog := &sessionOptionsCatalogStub{
		target: SessionOptionsTarget{
			ProjectID: harness.projectID, TargetID: 41, ProfileID: 42, SessionEnvironmentSupported: true,
		},
		projects: []SessionOptionsProject{{ID: harness.projectID, Name: "Runtime Owner", Slug: "runtime-owner"}},
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Runtime: harness.runtime, RuntimeID: "workspace-one", SessionCatalog: catalog}, true
	})
	response := httptest.NewRecorder()
	handlers.SessionOptions(response, httptest.NewRequest(http.MethodGet, "/api/vault-session-options?runtime_id=7", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"supported":true`) || !strings.Contains(body, item.Name) ||
		!strings.Contains(body, `"projects":[{"id":`) || strings.Contains(body, "runtime-secret-value") {
		t.Fatalf("response = %s", body)
	}
	if catalog.projectCalls != 1 {
		t.Fatalf("project calls = %d", catalog.projectCalls)
	}
}

func TestHTTPSessionOptionsReportsUnsupportedWithoutReadingVaultData(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	catalog := &sessionOptionsCatalogStub{target: SessionOptionsTarget{
		ProjectID: harness.projectID, TargetID: 41, ProfileID: 42, SessionEnvironmentSupported: false,
	}}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Runtime: harness.runtime, RuntimeID: "workspace-one", SessionCatalog: catalog}, true
	})
	response := httptest.NewRecorder()
	handlers.SessionOptions(response, httptest.NewRequest(http.MethodGet, "/api/vault-session-options?runtime_id=7", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"supported":false`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"projects":null`) {
		t.Fatalf("unsupported response changed projects contract: %s", response.Body.String())
	}
	if catalog.projectCalls != 0 {
		t.Fatalf("unsupported target loaded projects %d times", catalog.projectCalls)
	}
}

func TestHTTPSessionOptionsMapsCatalogFailures(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "runtime missing", err: ErrSessionRuntimeNotFound, status: http.StatusNotFound},
		{name: "target missing", err: ErrSessionTargetNotFound, status: http.StatusNotFound},
		{name: "catalog failure", err: errors.New("catalog failed"), status: http.StatusInternalServerError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newRuntimeTestHarness(t)
			handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
				return HTTPScope{
					Runtime: harness.runtime, RuntimeID: "workspace-one",
					SessionCatalog: &sessionOptionsCatalogStub{targetErr: testCase.err},
				}, true
			})
			response := httptest.NewRecorder()
			handlers.SessionOptions(response, httptest.NewRequest(http.MethodGet, "/api/vault-session-options?runtime_id=7", nil))
			if response.Code != testCase.status {
				t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
			}
		})
	}
}

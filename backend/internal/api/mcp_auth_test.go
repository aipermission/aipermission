package api

import (
	"context"
	"testing"
	"time"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestMCPAuthenticationSourcesKeepRuntimeIndexesAligned(t *testing.T) {
	missing := &gatewayinfra.WorkspaceHandle{}
	matching := &gatewayinfra.WorkspaceHandle{}
	source := mcpTokenSourceStub{}

	sources, runtimes := mcpAuthenticationSources(
		[]*gatewayinfra.WorkspaceHandle{missing, matching},
		func(runtime *gatewayinfra.WorkspaceHandle) (gatewayaccess.MCPTokenSource, bool) {
			if runtime == matching {
				return source, true
			}
			return nil, false
		},
	)
	if len(sources) != 1 || len(runtimes) != 1 {
		t.Fatalf("filtered sources=%d runtimes=%d", len(sources), len(runtimes))
	}
	if runtimes[0] != matching {
		t.Fatal("filtered MCP source no longer points to its matching workspace")
	}
}

type mcpTokenSourceStub struct{}

func (mcpTokenSourceStub) AuthenticateHash(context.Context, string, time.Time) (tokens.Authentication, error) {
	return tokens.Authentication{}, nil
}

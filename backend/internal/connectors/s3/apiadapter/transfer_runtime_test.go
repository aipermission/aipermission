package apiadapter

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type transferAuthority struct {
	connectorapi.TransferRuntime
	calls      int
	id         int64
	capability string
	surface    connectorapi.RuntimeSurface
	err        error
}

func (r *transferAuthority) ResolveRuntimeContext(_ context.Context, id int64, capability string) (connectors.RuntimeContext, connectorapi.RuntimeSurface, error) {
	r.calls++
	r.id, r.capability = id, capability
	return connectors.RuntimeContext{}, r.surface, r.err
}

type transferGateway struct {
	connectorapi.FileTransferGateway
	calls int
}

func (g *transferGateway) ConnectorRuntimeCapabilities() connectors.RuntimeCapabilityResolver {
	g.calls++
	return nil
}

func TestTransferEntrypointsRejectInvalidAuthority(t *testing.T) {
	const runtimeID int64 = 9007199254740993
	ctx := context.Background()
	localPath := filepath.Join(t.TempDir(), "object.txt")
	adapter := adapter{}
	entrypoints := map[string]func(connectorapi.FileTransferGateway, connectorapi.TransferRuntime) error{
		"browse": func(g connectorapi.FileTransferGateway, r connectorapi.TransferRuntime) error {
			_, err := adapter.BrowseRemoteFiles(ctx, g, r, runtimeID, "/")
			return err
		},
		"browse_page": func(g connectorapi.FileTransferGateway, r connectorapi.TransferRuntime) error {
			_, err := adapter.BrowseRemoteFilesPage(ctx, g, r, runtimeID, "/", "")
			return err
		},
		"stat": func(g connectorapi.FileTransferGateway, r connectorapi.TransferRuntime) error {
			_, err := adapter.StatRemotePath(ctx, g, r, runtimeID, "/object.txt")
			return err
		},
		"recursive": func(g connectorapi.FileTransferGateway, r connectorapi.TransferRuntime) error {
			_, err := adapter.ListRecursiveFiles(ctx, g, r, runtimeID, "/", 10, 1024, 2048)
			return err
		},
		"upload": func(g connectorapi.FileTransferGateway, r connectorapi.TransferRuntime) error {
			_, err := adapter.UploadFile(ctx, g, r, runtimeID, localPath, "/object.txt", false, connectors.TransferOptions{})
			return err
		},
		"download": func(g connectorapi.FileTransferGateway, r connectorapi.TransferRuntime) error {
			_, err := adapter.DownloadFile(ctx, g, r, runtimeID, "/object.txt", localPath, connectors.TransferOptions{})
			return err
		},
	}
	cases := []struct {
		name                 string
		noGateway, noRuntime bool
		kind, capability     string
		resolveErr, wantErr  error
	}{
		{name: "missing_gateway", noGateway: true},
		{name: "missing_runtime", noRuntime: true},
		{name: "resolve_cancelled", resolveErr: context.Canceled, wantErr: context.Canceled},
		{name: "resolve_missing", resolveErr: connectortargets.ErrRuntimeSurfaceNotFound, wantErr: connectortargets.ErrRuntimeSurfaceNotFound},
		{name: "foreign_connector", kind: "other", capability: connectortargets.RuntimeCapabilityFileTransfer, wantErr: connectortargets.ErrRuntimeSurfaceNotFound},
		{name: "foreign_capability", kind: "s3", capability: "other", wantErr: connectortargets.ErrRuntimeSurfaceNotFound},
	}
	for name, invoke := range entrypoints {
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				runtime := &transferAuthority{surface: connectorapi.RuntimeSurface{ConnectorKind: tc.kind, CapabilityKind: tc.capability}, err: tc.resolveErr}
				gateway := &transferGateway{}
				var g connectorapi.FileTransferGateway = gateway
				var r connectorapi.TransferRuntime = runtime
				if tc.noGateway {
					g = nil
				}
				if tc.noRuntime {
					r = nil
				}
				err := invoke(g, r)
				if tc.wantErr != nil {
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("error = %v, want %v", err, tc.wantErr)
					}
				} else if err == nil || !strings.Contains(err.Error(), "runtime is unavailable") {
					t.Fatalf("missing authority error = %v", err)
				}
				wantCalls := 1
				if tc.noGateway || tc.noRuntime {
					wantCalls = 0
				}
				if runtime.calls != wantCalls || gateway.calls != 0 {
					t.Fatalf("resolve calls = %d, capability calls = %d", runtime.calls, gateway.calls)
				}
				if wantCalls != 0 && (runtime.id != runtimeID || runtime.capability != connectortargets.RuntimeCapabilityFileTransfer) {
					t.Fatalf("authority binding = %d/%q", runtime.id, runtime.capability)
				}
			})
		}
	}
}

package operations

import (
	"sync"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	filetransferhttp "github.com/aipermission/aipermission/backend/internal/filetransfer/httpapi"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

type State struct {
	CommandRequests   *commandrequests.Runtime
	FileTransfers     *filetransferhttp.Runtime
	TransferLifecycle *filetransferhttp.Lifecycle
	actionWorkflowMu  sync.Mutex
	actionWorkflow    *actions.Runtime
	projectVaultMu    sync.Mutex
	projectVault      *projectvault.Runtime
}

type Port interface {
	CommandRequestRuntime() *commandrequests.Runtime
	SetCommandRequestRuntime(*commandrequests.Runtime)
	FileTransferRuntime() *filetransferhttp.Runtime
	SetFileTransferRuntime(*filetransferhttp.Runtime)
	FileTransferLifecycle() *filetransferhttp.Lifecycle
	ActionWorkflow() *actions.Runtime
	ActionWorkflowOrCreate(func() (*actions.Runtime, error)) (*actions.Runtime, error)
	ProjectVaultOrCreate(func() (*projectvault.Runtime, error)) (*projectvault.Runtime, error)
}

func New() State {
	return State{TransferLifecycle: filetransferhttp.NewLifecycle()}
}

func (s *State) CommandRequestRuntime() *commandrequests.Runtime {
	if s == nil {
		return nil
	}
	return s.CommandRequests
}

func (s *State) SetCommandRequestRuntime(runtime *commandrequests.Runtime) {
	if s != nil {
		s.CommandRequests = runtime
	}
}

func (s *State) FileTransferRuntime() *filetransferhttp.Runtime {
	if s == nil {
		return nil
	}
	return s.FileTransfers
}

func (s *State) SetFileTransferRuntime(runtime *filetransferhttp.Runtime) {
	if s != nil {
		s.FileTransfers = runtime
	}
}

func (s *State) FileTransferLifecycle() *filetransferhttp.Lifecycle {
	if s == nil {
		return nil
	}
	return s.TransferLifecycle
}

func (s *State) ActionWorkflow() *actions.Runtime {
	if s == nil {
		return nil
	}
	s.actionWorkflowMu.Lock()
	defer s.actionWorkflowMu.Unlock()
	return s.actionWorkflow
}

func (s *State) ActionWorkflowOrCreate(create func() (*actions.Runtime, error)) (*actions.Runtime, error) {
	s.actionWorkflowMu.Lock()
	defer s.actionWorkflowMu.Unlock()
	if s.actionWorkflow != nil {
		return s.actionWorkflow, nil
	}
	workflow, err := create()
	if err != nil {
		return nil, err
	}
	s.actionWorkflow = workflow
	return workflow, nil
}

func (s *State) ProjectVaultOrCreate(create func() (*projectvault.Runtime, error)) (*projectvault.Runtime, error) {
	s.projectVaultMu.Lock()
	defer s.projectVaultMu.Unlock()
	if s.projectVault != nil {
		return s.projectVault, nil
	}
	runtime, err := create()
	if err != nil {
		return nil, err
	}
	s.projectVault = runtime
	return runtime, nil
}

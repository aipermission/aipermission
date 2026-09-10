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

func New() State {
	return State{TransferLifecycle: filetransferhttp.NewLifecycle()}
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

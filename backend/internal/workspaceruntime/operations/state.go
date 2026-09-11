package operations

import transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"

type State struct {
	FileTransfers     *transferapp.Runtime
	TransferLifecycle *transferapp.Lifecycle
}

type Port interface {
	FileTransferRuntime() *transferapp.Runtime
	SetFileTransferRuntime(*transferapp.Runtime)
	FileTransferLifecycle() *transferapp.Lifecycle
}

func New() State {
	return State{TransferLifecycle: transferapp.NewLifecycle()}
}

func (s *State) FileTransferRuntime() *transferapp.Runtime {
	if s == nil {
		return nil
	}
	return s.FileTransfers
}

func (s *State) SetFileTransferRuntime(runtime *transferapp.Runtime) {
	if s != nil {
		s.FileTransfers = runtime
	}
}

func (s *State) FileTransferLifecycle() *transferapp.Lifecycle {
	if s == nil {
		return nil
	}
	return s.TransferLifecycle
}

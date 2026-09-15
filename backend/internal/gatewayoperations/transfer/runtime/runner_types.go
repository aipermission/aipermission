package transferruntime

import (
	"errors"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

var ErrRuntimeClosing = errors.New("file transfer runtime is shutting down")
var ErrLaunchInvalid = errors.New("file transfer launch is invalid")
var ErrExecutionStale = errors.New("file transfer credential or target changed after execution was accepted")

// Execution is an immutable, credential-redacted capability snapshot accepted
// before a worker is launched.
type Execution struct {
	RuntimeID int64
	Adapter   connectorapi.FileTransferAdapter
	Gateway   connectorapi.FileTransferGateway
	Runtime   connectorapi.TransferRuntime
	Boundary  actionresult.CredentialBoundary
}

func (execution Execution) ValidFor(runtimeID int64) bool {
	return execution.RuntimeID == runtimeID && execution.Adapter != nil && execution.Gateway != nil && execution.Runtime != nil && execution.Boundary.Valid()
}

type RunnerConfig struct {
	DataPath              string
	MaxObjectBytes        int64
	MaxBatchBytes         int64
	TransferTimeout       time.Duration
	BatchTimeout          time.Duration
	TempTTL               time.Duration
	TempCleanupRetry      time.Duration
	RemoteRecoveryTimeout time.Duration
	RemoteRecoveryRetry   time.Duration
	AdapterFor            func(string) connectorapi.FileTransferAdapter
}

type Runner struct {
	dataPath              string
	maxObjectBytes        int64
	maxBatchBytes         int64
	transferTimeout       time.Duration
	batchTimeout          time.Duration
	tempTTL               time.Duration
	tempCleanupRetry      time.Duration
	remoteRecoveryTimeout time.Duration
	remoteRecoveryRetry   time.Duration
	adapterFor            func(string) connectorapi.FileTransferAdapter
}

func NewRunner(config RunnerConfig) *Runner {
	remoteRecoveryTimeout := config.RemoteRecoveryTimeout
	if remoteRecoveryTimeout <= 0 {
		remoteRecoveryTimeout = 10 * time.Second
	}
	remoteRecoveryRetry := config.RemoteRecoveryRetry
	if remoteRecoveryRetry <= 0 {
		remoteRecoveryRetry = 30 * time.Second
	}
	tempCleanupRetry := config.TempCleanupRetry
	if tempCleanupRetry <= 0 {
		tempCleanupRetry = 30 * time.Second
	}
	return &Runner{
		dataPath: strings.TrimSpace(config.DataPath), maxObjectBytes: config.MaxObjectBytes, maxBatchBytes: config.MaxBatchBytes,
		transferTimeout: config.TransferTimeout, batchTimeout: config.BatchTimeout,
		tempTTL: config.TempTTL, tempCleanupRetry: tempCleanupRetry,
		remoteRecoveryTimeout: remoteRecoveryTimeout, remoteRecoveryRetry: remoteRecoveryRetry,
		adapterFor: config.AdapterFor,
	}
}

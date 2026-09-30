package management

import "github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"

const cleanupStatusOperation = "key-cleanup-status"
const cleanupAttestOperation = "key-cleanup-attest"

type cleanupIdentityChoice struct {
	Identity keycleanup.Identity              `json:"identity"`
	Digest   string                           `json:"digest"`
	Subjects []keycleanup.VerificationSubject `json:"subjects"`
}

type cleanupRecordView struct {
	Entry   keycleanup.Entry        `json:"entry"`
	Choices []cleanupIdentityChoice `json:"choices"`
}

type cleanupReconciliationContext struct {
	TargetID      int64               `json:"target_id"`
	ContextDigest string              `json:"deletion_context_digest"`
	Records       []cleanupRecordView `json:"records"`
}

type cleanupAttestInput struct {
	ResourceID     int64                        `json:"resource_id"`
	Generation     string                       `json:"generation"`
	ContextDigest  string                       `json:"deletion_context_digest"`
	IdentityDigest string                       `json:"identity_digest"`
	Coverage       []keycleanup.AbsenceEvidence `json:"coverage"`
	Reason         string                       `json:"reason"`
}

func (Management) RequiresTargetOperationExclusion(operation string) bool {
	return operation == cleanupAttestOperation
}

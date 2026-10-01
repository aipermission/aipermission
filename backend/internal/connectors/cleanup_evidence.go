package connectors

import "context"

// CleanupEvidenceContext has no credential accessor, admin identity, transport
// or action ports. Core supplies only reviewed read-only resource capabilities;
// their concrete types must not expose resource mutation or secret methods.
type CleanupEvidenceContext struct {
	Target       TargetView
	Capabilities RuntimeCapabilityResolver
}

// ProvisionedCredentialCleanupEvidence is an optional, read-only local proof
// check before core looks up or decrypts the original admin credential. A nil
// result requires normal external cleanup. A non-nil result must represent
// durably confirmed completed cleanup for this exact target and profile;
// remote absence or an unresolved dispatch record is never sufficient proof.
// The caller holds exclusive lifecycle admission through local retirement.
type ProvisionedCredentialCleanupEvidence interface {
	ReadCompletedCredentialCleanup(context.Context, CleanupEvidenceContext, CredentialProfileView) (*ActionResult, error)
}

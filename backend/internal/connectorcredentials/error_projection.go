package connectorcredentials

import (
	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

// RedactErrorForAudit applies mandatory credential masking before basic policy
// masking can alter a delivered value. This path has no optional runtime policy.
func RedactErrorForAudit(err error, boundary actionresult.CredentialBoundary) string {
	if err == nil {
		return ""
	}
	return actionresult.RedactCredentialText(err.Error(), boundary.Redact, securitypolicy.RedactBasic)
}

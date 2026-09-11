package gatewayaccess

import (
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/uisession"
)

func (*Component) RedactFallback(value string) string {
	return securitypolicy.RedactBasic(value)
}

func (*Component) IsUIExempt(path string) bool {
	return uisession.IsExempt(path)
}

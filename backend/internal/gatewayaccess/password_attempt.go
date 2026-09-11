package gatewayaccess

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
)

type PasswordAttempt struct {
	component *Component
	key       string
}

func (component *Component) BeginPasswordAttempt(request *http.Request, scope string) (PasswordAttempt, error) {
	if component == nil || component.databasePasswordLimiter == nil || request == nil {
		return PasswordAttempt{}, ErrComponentUnavailable
	}
	key := runtimecontrol.Key(request, scope)
	if err := component.databasePasswordLimiter.Wait(request.Context(), key); err != nil {
		return PasswordAttempt{}, err
	}
	return PasswordAttempt{component: component, key: key}, nil
}

func (attempt PasswordAttempt) Failure() {
	if attempt.component != nil && attempt.component.databasePasswordLimiter != nil && attempt.key != "" {
		attempt.component.databasePasswordLimiter.RecordFailure(attempt.key)
	}
}

func (attempt PasswordAttempt) Success() {
	if attempt.component != nil && attempt.component.databasePasswordLimiter != nil && attempt.key != "" {
		attempt.component.databasePasswordLimiter.RecordSuccess(attempt.key)
	}
}

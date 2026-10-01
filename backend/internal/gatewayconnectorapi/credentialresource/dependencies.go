package credentialresource

import (
	"errors"
	"reflect"
)

var (
	ErrCredentialResourceNotFound   = errors.New("connector credential resource not found")
	ErrCredentialResourceNameExists = errors.New("connector credential resource name already exists")
)

// IsNilDependency detects an absent optional interface port, including a typed
// nil implementation. It does not invoke the port to discover availability.
func IsNilDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

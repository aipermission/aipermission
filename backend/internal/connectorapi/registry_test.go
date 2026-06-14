package connectorapi

import "testing"

func TestRegisterRejectsDuplicateAdapter(t *testing.T) {
	const kind = "duplicate_test"
	Register(kind, struct{}{})

	defer func() {
		mu.Lock()
		delete(adapters, kind)
		mu.Unlock()
		if recovered := recover(); recovered == nil {
			t.Fatal("expected duplicate adapter registration panic")
		}
	}()

	Register(kind, struct{}{})
}

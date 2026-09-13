package httptransport

import "testing"

func TestValidateConnectorOwnedRoutePath(t *testing.T) {
	if err := ValidateConnectorOwnedRoutePath("mail", "/api/connectors/mail/folders"); err != nil {
		t.Fatalf("valid connector route: %v", err)
	}
	for _, path := range []string{
		"/api/connectors/other/folders",
		"/api/connectors/mail/credentials",
		"/api/connectors/mail/credentials/import",
		"/api/connectors/mail/%63redentials",
		"/api/connectors/mail/../credentials",
		"/api/connectors/mail/{resource}",
	} {
		if err := ValidateConnectorOwnedRoutePath("mail", path); err == nil {
			t.Errorf("accepted invalid connector route %q", path)
		}
	}
}

package httptransport

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

const ConnectorCredentialsSegment = "credentials"

// ValidateConnectorOwnedRoutePath keeps extension routes inside one static
// connector namespace and away from core-owned credential routes.
func ValidateConnectorOwnedRoutePath(kind, routePath string) error {
	prefix := "/api/connectors/" + kind + "/"
	if !strings.HasPrefix(routePath, prefix) || len(routePath) == len(prefix) {
		return fmt.Errorf("connector-owned route must use namespace %s...", prefix)
	}
	decoded, err := url.PathUnescape(routePath)
	if err != nil || decoded != routePath || path.Clean(routePath) != routePath || strings.ContainsAny(routePath, "{}") {
		return fmt.Errorf("connector-owned route path must be canonical and static")
	}
	segment, _, _ := strings.Cut(strings.TrimPrefix(routePath, prefix), "/")
	if segment == ConnectorCredentialsSegment {
		return fmt.Errorf("path uses core-owned route segment %q", segment)
	}
	return nil
}

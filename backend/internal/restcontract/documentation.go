package restcontract

import (
	"fmt"
	"strings"
)

// ValidateDocumentedRoutes requires an exact method/path inventory line for
// every registered route; mentioning a sibling method or subpath is not enough.
func ValidateDocumentedRoutes(routes []Route, documentation string) error {
	listed := make(map[Route]bool)
	for _, line := range strings.Split(documentation, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			listed[Route{Method: fields[0], Path: fields[1]}] = true
		}
	}
	var missing []string
	for _, route := range routes {
		if !listed[route] {
			missing = append(missing, route.Method+" "+route.Path)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("REST docs lack exact registered method/path lines: %s", strings.Join(missing, ", "))
	}
	return nil
}

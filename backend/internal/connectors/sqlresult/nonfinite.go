package sqlresult

import (
	"fmt"
	"math"
)

// NormalizeNonFinite is also used by recursive native aggregate normalization,
// so an array containing NaN or infinity does not lose its structure or NULLs.
func NormalizeNonFinite(value any) any {
	switch number := value.(type) {
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Sprint(number)
		}
	case float32:
		if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
			return fmt.Sprint(number)
		}
	}
	return value
}

package actioncapacity

import (
	"fmt"
	"os"
	"strconv"
)

const StorageBudgetEnvironment = "AIPERMISSION_CONNECTOR_REQUEST_STORAGE_MIB"

// RuntimeLimits reads operator-owned process configuration, never action input.
// Row and running ceilings remain fixed regardless of the storage budget.
func RuntimeLimits() (Limits, error) {
	return ParseStorageBudget(os.Getenv(StorageBudgetEnvironment))
}

func ParseStorageBudget(value string) (Limits, error) {
	limits := DefaultLimits()
	if value == "" {
		return limits, nil
	}
	mib, err := strconv.ParseInt(value, 10, 64)
	if err != nil || mib < 256 || mib > 4096 {
		return Limits{}, fmt.Errorf("%s must be an integer from 256 through 4096 MiB", StorageBudgetEnvironment)
	}
	limits.Bytes = mib << 20
	return limits, nil
}

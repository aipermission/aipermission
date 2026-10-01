package postgresconnector

import (
	"encoding/json"

	"github.com/aipermission/aipermission/backend/internal/jsonidentity"
)

func decodeProvisionScopeJSON(text string, decoded *map[string]any) error {
	data := []byte(text)
	if err := jsonidentity.Validate(data); err != nil {
		return err
	}
	return json.Unmarshal(data, decoded)
}

package rabbitmqconnector

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Some Management API versions ignore binding pagination. The HTTP client has
// already validated the entire JSON document and enforced its byte limit.
func decodeRabbitBindingArray(data []byte, pageSize, itemLimit int) (rabbitListPage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return rabbitListPage{}, err
	}
	result := rabbitListPage{Page: 1, PageSize: pageSize, PageCount: 1}
	for decoder.More() && len(result.Items) < itemLimit {
		var row map[string]any
		if err := decoder.Decode(&row); err != nil || row == nil {
			return rabbitListPage{}, fmt.Errorf("rabbitmq returned a non-object binding")
		}
		result.Items = append(result.Items, row)
	}
	result.FilteredCount = len(result.Items)
	result.ScanLimited = decoder.More()
	return result, nil
}

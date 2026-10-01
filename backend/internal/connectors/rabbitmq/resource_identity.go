package rabbitmqconnector

import (
	"fmt"
	"unicode/utf8"
)

func validateRabbitIdentities(inputs ...map[string]any) error {
	for _, input := range inputs {
		for _, field := range []string{"vhost", "queue", "exchange", "routing_key"} {
			value, exists := input[field]
			if !exists || value == nil {
				continue
			}
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s must be a string", field)
			}
			if !utf8.ValidString(text) {
				return fmt.Errorf("%s must be valid UTF-8", field)
			}
		}
	}
	return nil
}

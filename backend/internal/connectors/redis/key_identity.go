package redisconnector

import (
	"fmt"
	"unicode/utf8"
)

func validateRedisKeyIdentity(key string) error {
	if !utf8.ValidString(key) {
		return fmt.Errorf("redis key is not valid UTF-8; binary keys are unsupported by the JSON action boundary")
	}
	if len(key) > maxRESPBulkBytes {
		return fmt.Errorf("redis key exceeds %d bytes", maxRESPBulkBytes)
	}
	return nil
}

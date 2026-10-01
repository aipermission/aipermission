// Package jsonidentity rejects JSON that would lose string identity on decode.
package jsonidentity

import (
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"
)

// Validate checks raw bytes before encoding/json can repair malformed Unicode.
// Callers retain their typed decoder, field policy and request size limit.
func Validate(data []byte) error {
	if !utf8.Valid(data) || !json.Valid(data) {
		return errors.New("input must be valid UTF-8 JSON")
	}
	for index := 0; index < len(data); {
		if data[index] != '\\' {
			index++
			continue
		}
		if data[index+1] != 'u' {
			index += 2
			continue
		}
		unit := unicodeUnit(data[index+2 : index+6])
		index += 6
		if unit >= 0xdc00 && unit <= 0xdfff {
			return errors.New("JSON contains an unpaired Unicode surrogate")
		}
		if unit >= 0xd800 && unit <= 0xdbff {
			if index+6 > len(data) || data[index] != '\\' || data[index+1] != 'u' {
				return errors.New("JSON contains an unpaired Unicode surrogate")
			}
			low := unicodeUnit(data[index+2 : index+6])
			if low < 0xdc00 || low > 0xdfff {
				return errors.New("JSON contains an unpaired Unicode surrogate")
			}
			index += 6
		}
	}
	return nil
}

func unicodeUnit(data []byte) uint64 {
	value, _ := strconv.ParseUint(string(data), 16, 16) // json.Valid already checked four hex digits.
	return value
}

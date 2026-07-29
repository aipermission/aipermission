package projectvault

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
)

const passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*()-_=+"

func ValidateGeneratorKind(kind string) error {
	switch kind {
	case "random_token", "hex_secret", "password", "long_hmac_secret", "uuid_v4":
		return nil
	default:
		return ValidationError("unsupported generator kind")
	}
}

func Generate(kind string) (string, map[string]any, error) {
	if err := ValidateGeneratorKind(kind); err != nil {
		return "", nil, err
	}
	switch kind {
	case "random_token":
		value, err := randomBytes(32)
		return base64.RawURLEncoding.EncodeToString(value), map[string]any{"bytes": 32, "encoding": "base64url"}, err
	case "hex_secret":
		value, err := randomBytes(32)
		return hex.EncodeToString(value), map[string]any{"bytes": 32, "encoding": "hex"}, err
	case "password":
		value, err := randomPassword(32)
		return value, map[string]any{"characters": 32, "alphabet": "reviewed"}, err
	case "long_hmac_secret":
		value, err := randomBytes(64)
		return hex.EncodeToString(value), map[string]any{"bytes": 64, "encoding": "hex"}, err
	case "uuid_v4":
		value, err := randomUUID()
		return value, map[string]any{"version": 4}, err
	default:
		return "", nil, ValidationError("unsupported generator kind")
	}
}

func randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return nil, fmt.Errorf("generate random value: %w", err)
	}
	return value, nil
}

func randomPassword(size int) (string, error) {
	value := make([]byte, size)
	limit := big.NewInt(int64(len(passwordAlphabet)))
	for index := range value {
		selected, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		value[index] = passwordAlphabet[selected.Int64()]
	}
	return string(value), nil
}

func randomUUID() (string, error) {
	value, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
}

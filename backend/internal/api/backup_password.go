package api

import (
	"fmt"
	"strings"
	"unicode"
)

const remoteBackupPasswordMinimumLength = 18

func validateRemoteBackupPassword(password, databaseName string) error {
	if len(password) < remoteBackupPasswordMinimumLength {
		return fmt.Errorf("remote backup requires a database password of at least %d characters", remoteBackupPasswordMinimumLength)
	}
	var hasUpper, hasLower, hasDigit bool
	unique := map[rune]struct{}{}
	for _, char := range password {
		unique[char] = struct{}{}
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsDigit(char):
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("remote backup database password must include uppercase letters, lowercase letters, and numbers")
	}
	if len(unique) < 10 {
		return fmt.Errorf("remote backup database password must use at least 10 distinct characters")
	}
	lower := strings.ToLower(password)
	for _, weak := range []string{"password", "aipermission", "qwerty", "letmein", "changeme", "welcome", "administrator"} {
		if strings.Contains(lower, weak) {
			return fmt.Errorf("remote backup database password contains a common or product-related term")
		}
	}
	if containsRepeatedRun(lower, 4) || containsSequence(lower, 4) {
		return fmt.Errorf("remote backup database password contains a repeated or sequential pattern")
	}
	if normalizedName := alphanumericLower(databaseName); len(normalizedName) >= 4 && strings.Contains(alphanumericLower(password), normalizedName) {
		return fmt.Errorf("remote backup database password must not contain the database name")
	}
	return nil
}

func containsRepeatedRun(value string, minimum int) bool {
	var previous rune
	run := 0
	for _, char := range value {
		if char == previous {
			run++
		} else {
			previous = char
			run = 1
		}
		if run >= minimum {
			return true
		}
	}
	return false
}

func containsSequence(value string, minimum int) bool {
	const ordered = "abcdefghijklmnopqrstuvwxyz0123456789"
	const reversed = "zyxwvutsrqponmlkjihgfedcba9876543210"
	for _, sequence := range []string{ordered, reversed} {
		for start := 0; start+minimum <= len(sequence); start++ {
			if strings.Contains(value, sequence[start:start+minimum]) {
				return true
			}
		}
	}
	return false
}

func alphanumericLower(value string) string {
	var output strings.Builder
	for _, char := range strings.ToLower(value) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			output.WriteRune(char)
		}
	}
	return output.String()
}

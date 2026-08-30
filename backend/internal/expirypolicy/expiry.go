package expirypolicy

import "time"

// Active treats only an empty value as permanent. Malformed non-empty values
// fail closed so corrupted or imported authorization records cannot become
// implicitly permanent.
func Active(value string, now time.Time) bool {
	if value == "" {
		return true
	}
	expiresAt, err := time.Parse(time.RFC3339, value)
	return err == nil && expiresAt.After(now.UTC())
}

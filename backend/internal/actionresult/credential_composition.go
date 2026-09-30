package actionresult

// RedactCredentialText removes known values before optional rules can change
// their identity. Both callbacks must be configured. The final pass also masks
// known values introduced by an optional transformation.
func RedactCredentialText(value string, credentials, optional func(string) string) string {
	value = credentials(value)
	value = optional(value)
	return credentials(value)
}

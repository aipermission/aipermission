package connectortargets

import "strconv"

// TokenIdempotencyScopePrefix is shared with staged-upload SQL joins.
const TokenIdempotencyScopePrefix = "token:"

// ActionIdempotencyScope preserves the persisted request/tombstone owner key.
func ActionIdempotencyScope(tokenID *int64, source string) string {
	if tokenID != nil {
		return TokenIdempotencyScopePrefix + strconv.FormatInt(*tokenID, 10)
	}
	return "source:" + actionRequestSource(source)
}

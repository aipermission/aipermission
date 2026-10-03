package s3connector

import (
	"fmt"
	"net/url"
	"unicode/utf8"
)

func decodeListingNames(encoding string, names ...*string) error {
	if encoding == "" {
		return nil
	}
	if encoding != "url" {
		return fmt.Errorf("unsupported S3 listing encoding")
	}
	for _, name := range names {
		// Only resource names are encoded; continuation tokens and version IDs
		// are opaque. Path decoding also preserves literal '+' in key names.
		decoded, err := url.PathUnescape(*name)
		if err != nil || !utf8.ValidString(decoded) {
			return fmt.Errorf("invalid URL-encoded S3 listing resource name")
		}
		*name = decoded
	}
	return nil
}

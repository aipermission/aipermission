package s3connector

import "fmt"

const maxS3RecursivePages = 100

func requireListingProgress[T comparable](current, next T, missing bool, seen map[T]struct{}) error {
	if missing || current == next {
		return fmt.Errorf("truncated S3 listing has missing or repeated progress markers")
	}
	if seen != nil {
		seen[current] = struct{}{}
		if _, exists := seen[next]; exists {
			return fmt.Errorf("truncated S3 listing has cyclic progress markers")
		}
	}
	return nil
}

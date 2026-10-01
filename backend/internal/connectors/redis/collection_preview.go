package redisconnector

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	maxCollectionPreviewBytes = maxValueBytes
	maxKeyPreviewEncodedBytes = 3 << 20
	maxCollectionRangeOffset  = int64(math.MaxInt64 - maxValueLimit)
)

type redisPreviewPosition struct {
	cursor string
	offset int64
}

func collectionPreviewPosition(input map[string]any) (redisPreviewPosition, error) {
	if value, present := input["cursor"]; present && value != nil {
		if _, ok := value.(string); !ok {
			return redisPreviewPosition{}, fmt.Errorf("cursor must be an unsigned Redis scan cursor string")
		}
	}
	position := redisPreviewPosition{cursor: normalizeStringDefault(input, "cursor", "0")}
	if _, err := strconv.ParseUint(position.cursor, 10, 64); err != nil {
		return redisPreviewPosition{}, fmt.Errorf("cursor must be an unsigned Redis scan cursor")
	}
	if value, present := input["offset"]; present && value != nil {
		offset, ok := connectors.ExactInt64Value(value)
		if !ok || offset < 0 || offset > maxCollectionRangeOffset {
			return redisPreviewPosition{}, fmt.Errorf("offset must be a nonnegative integer with room for one preview page")
		}
		position.offset = offset
	}
	return position, nil
}

type redisCollectionPreview struct {
	kind         string
	maxBytes     int
	encodedBytes int
	count        int
	truncated    bool
	complete     bool
	scanLimit    bool
	next         redisPreviewPosition
	fields       map[string]string
	items        []string
	scores       []map[string]string
}

func newCollectionPreview(kind string, maxBytes int) *redisCollectionPreview {
	preview := &redisCollectionPreview{
		kind: kind, maxBytes: maxBytes, encodedBytes: 2,
		fields: map[string]string{}, items: []string{}, scores: []map[string]string{},
	}
	if kind == "list" {
		preview.items = nil
		preview.encodedBytes = 4
	}
	return preview
}

func (preview *redisCollectionPreview) value() any {
	switch preview.kind {
	case "hash":
		return preview.fields
	case "zset":
		return preview.scores
	default:
		return preview.items
	}
}

func (preview *redisCollectionPreview) appendEntry(entry []string) (bool, error) {
	overhead, replaced := 1, 0
	switch preview.kind {
	case "hash":
		if err := validateRedisKeyIdentity(entry[0]); err != nil {
			return false, fmt.Errorf("redis hash field: %w", err)
		}
		overhead += jsonStringBytes(entry[0]) + 1
		if previous, exists := preview.fields[entry[0]]; exists {
			replaced = overhead + jsonStringBytes(previous)
		}
	case "zset":
		if score, err := strconv.ParseFloat(entry[1], 64); err != nil || math.IsNaN(score) {
			return false, fmt.Errorf("unexpected ZRANGE response: invalid score")
		}
		overhead += len(`{"member":`) + len(`,"score":`) + jsonStringBytes(entry[1]) + len("}")
	}
	raw := entry[0]
	if preview.kind == "hash" {
		raw = entry[1]
	}
	text, cost, accepted, err := preview.fitValue(raw, overhead, replaced)
	if err != nil || !accepted {
		return false, err
	}
	preview.encodedBytes += cost - replaced
	if replaced == 0 {
		preview.count++
	}
	preview.truncated = preview.truncated || text != raw
	switch preview.kind {
	case "hash":
		preview.fields[strings.Clone(entry[0])] = text
	case "zset":
		preview.scores = append(preview.scores, map[string]string{"member": text, "score": strings.Clone(entry[1])})
	default:
		preview.items = append(preview.items, text)
	}
	return true, nil
}

func (preview *redisCollectionPreview) fitValue(raw string, overhead, replaced int) (string, int, bool, error) {
	if overhead+2 > maxCollectionPreviewBytes-2 {
		return "", 0, false, fmt.Errorf("redis collection identity exceeds the %d-byte encoded preview budget", maxCollectionPreviewBytes)
	}
	available := maxCollectionPreviewBytes - preview.encodedBytes + replaced
	text := truncateString(raw, preview.maxBytes)
	cost := overhead + jsonStringBytes(text)
	if cost > available {
		if preview.count > 0 {
			return "", 0, false, nil
		}
		text = fitEncodedPreviewString(raw, preview.maxBytes, available-overhead)
		cost = overhead + jsonStringBytes(text)
	}
	return strings.Clone(text), cost, cost <= available, nil
}

func fitEncodedPreviewString(raw string, maxBytes, encodedBudget int) string {
	low, high := 0, maxBytes
	for low < high {
		middle := low + (high-low+1)/2
		if jsonStringBytes(truncateString(raw, middle)) <= encodedBudget {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return truncateString(raw, low)
}

func jsonStringBytes(text string) int {
	encoded, _ := json.Marshal(text)
	return len(encoded)
}

func (preview *redisCollectionPreview) project(output map[string]any, input map[string]any) {
	output["value"] = preview.value()
	output["truncated"] = preview.truncated || !preview.complete
	output["complete"] = preview.complete
	output["returned_items"] = preview.count
	output["scan_limit_reached"] = preview.scanLimit
	if !preview.complete {
		next := map[string]any{
			"key": output["key"], "limit": normalizeInt(input, "limit", defaultValueLimit, 1, maxValueLimit),
			"max_bytes": preview.maxBytes, "offset": preview.next.offset,
		}
		if preview.kind == "hash" || preview.kind == "set" {
			next["cursor"] = preview.next.cursor
		}
		output["next_input"] = next
	}
}

package redisconnector

import (
	"fmt"
	"strconv"
)

func readCollectionPreview(client *redisClient, kind, key string, limit, maxBytes int, position redisPreviewPosition) (*redisCollectionPreview, error) {
	preview := newCollectionPreview(kind, maxBytes)
	if kind == "hash" || kind == "set" {
		return readScanCollectionPreview(client, preview, key, limit, position)
	}
	command, stride := "LRANGE", 1
	args := []string{command, key, strconv.FormatInt(position.offset, 10), strconv.FormatInt(position.offset+int64(limit)-1, 10)}
	if kind == "zset" {
		command, stride = "ZRANGE", 2
		args[0] = command
		args = append(args, "WITHSCORES")
	}
	value, err := client.Do(args...)
	if err != nil {
		return nil, err
	}
	items, err := redisStringSlice(value, command)
	if err != nil {
		return nil, err
	}
	if len(items)%stride != 0 {
		return nil, fmt.Errorf("unexpected %s response: member and score pairs are incomplete", command)
	}
	for index := 0; index < len(items) && preview.count < limit; index += stride {
		if err := client.ctx.Err(); err != nil {
			return nil, err
		}
		accepted, err := preview.appendEntry(items[index : index+stride])
		if err != nil {
			return nil, err
		}
		if !accepted {
			break
		}
	}
	preview.next.offset = position.offset + int64(preview.count)
	preview.complete = preview.count == len(items)/stride && preview.count < limit
	if !preview.complete && preview.next.offset > maxCollectionRangeOffset {
		return nil, fmt.Errorf("redis collection continuation offset exceeds the supported range")
	}
	return preview, nil
}

func readScanCollectionPreview(client *redisClient, preview *redisCollectionPreview, key string, limit int, position redisPreviewPosition) (*redisCollectionPreview, error) {
	command, stride := "SSCAN", 1
	if preview.kind == "hash" {
		command, stride = "HSCAN", 2
	}
	for pages := 0; pages < maxScanPages; pages++ {
		offset := position.offset
		if offset < 0 || offset > maxRESPArrayItems {
			return nil, fmt.Errorf("offset must fit between zero and the maximum Redis scan page size")
		}
		// A continuation replays a page, so its COUNT hint must not change with
		// the number of entries already retained in the previous preview.
		value, err := client.Do(command, key, position.cursor, "COUNT", strconv.Itoa(min(limit, 100)))
		if err != nil {
			return nil, err
		}
		nextCursor, items, err := redisScanPage(value, command)
		if err != nil {
			return nil, err
		}
		if len(items)%stride != 0 {
			return nil, fmt.Errorf("unexpected %s response: field and value pairs are incomplete", command)
		}
		if offset > int64(len(items)/stride) {
			return nil, fmt.Errorf("redis collection continuation offset no longer fits the scan page")
		}
		index := int(offset) * stride
		for ; index < len(items) && preview.count < limit; index += stride {
			if err := client.ctx.Err(); err != nil {
				return nil, err
			}
			accepted, err := preview.appendEntry(items[index : index+stride])
			if err != nil {
				return nil, err
			}
			if !accepted {
				break
			}
		}
		if index < len(items) {
			preview.next = redisPreviewPosition{cursor: position.cursor, offset: int64(index / stride)}
			return preview, nil
		}
		preview.next = redisPreviewPosition{cursor: nextCursor}
		preview.complete = nextCursor == "0"
		if preview.complete || preview.count == limit {
			return preview, nil
		}
		position = preview.next
	}
	preview.scanLimit = true
	return preview, nil
}

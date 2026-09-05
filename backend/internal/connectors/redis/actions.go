package redisconnector

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func openRedisClient(ctx context.Context, runtime connectors.RuntimeContext) (*redisClient, error) {
	transport, _ := runtime.Capability(connectors.NetworkTransportCapabilityName).(connectors.NetworkTransport)
	if transport == nil {
		return nil, ErrMissingTransport
	}
	conn, err := transport.DialConnectorTCP(ctx, connectors.NetworkDialRequest{
		SourceTargetRef:    runtime.Target.Ref,
		SourceProjectID:    runtime.Target.ProjectID,
		Mode:               connectionMode(runtime.Target),
		Host:               redisHost(runtime.Target),
		Port:               redisPort(runtime.Target),
		TransportTargetRef: strings.TrimSpace(stringValue(runtime.Target.Config, "transport_target_ref")),
	})
	if err != nil {
		return nil, err
	}
	if tlsConfig := redisTLSConfig(runtime.Target); tlsConfig != nil {
		handshakeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		tlsConn := tls.Client(conn, tlsConfig)
		handshakeErr := tlsConn.HandshakeContext(handshakeContext)
		cancel()
		if handshakeErr != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("redis TLS handshake: %w", handshakeErr)
		}
		conn = tlsConn
	}
	client := newRedisClient(conn)
	client.bindContext(ctx)
	if err := authenticateRedis(ctx, runtime, client); err != nil {
		_ = client.Close()
		return nil, err
	}
	if database := redisDatabase(runtime.Target); database > 0 {
		if _, err := client.Do("SELECT", strconv.Itoa(database)); err != nil {
			_ = client.Close()
			return nil, err
		}
	}
	return client, nil
}

func authenticateRedis(ctx context.Context, runtime connectors.RuntimeContext, client *redisClient) error {
	username := strings.TrimSpace(stringValue(runtime.Profile.Public, "username"))
	password, err := runtime.Secrets.GetSecret(ctx, "password")
	if errors.Is(err, connectors.ErrSecretNotFound) {
		password = ""
	} else if err != nil {
		return fmt.Errorf("%w: resolve redis password: %w", connectors.ErrSecretProvider, err)
	}
	password = strings.TrimSpace(password)
	if username == "" && password == "" {
		return nil
	}
	if password == "" {
		return ErrMissingSecret
	}
	if username != "" {
		_, err = client.Do("AUTH", username, password)
	} else {
		_, err = client.Do("AUTH", password)
	}
	return err
}

func executePing(client *redisClient) (connectors.ActionResult, error) {
	value, err := client.Do("PING")
	if err != nil {
		return connectors.ActionResult{}, err
	}
	response := respString(value)
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"response": response},
		DisplayText: response,
	}, nil
}

func executeInfo(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	section := strings.TrimSpace(stringValue(input, "section"))
	args := []string{"INFO"}
	if section != "" {
		args = append(args, section)
	}
	value, err := client.Do(args...)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	info := truncateString(respString(value), maxValueBytes)
	document := parseRedisInfoDocument(info)
	output := map[string]any{"section": section, "info": document.sections, "raw": info}
	if identity, ok := redisServerIdentityFromFields(document.fields); ok {
		output["server"] = identity.details()
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: info,
	}, nil
}

func executeScanKeys(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	pattern := normalizeStringDefault(input, "pattern", "*")
	cursor := normalizeStringDefault(input, "cursor", "0")
	limit := normalizeInt(input, "limit", defaultScanLimit, 1, maxScanLimit)
	keys := []string{}
	seen := make(map[string]struct{}, limit)
	nextCursor := cursor
	pages := 0
	bytes := 0
	for len(keys) < limit && pages < maxScanPages {
		args := []string{"SCAN", nextCursor, "MATCH", pattern, "COUNT", strconv.Itoa(min(limit-len(keys), 100))}
		value, err := client.Do(args...)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		pageCursor, page, err := redisScanPage(value, "SCAN")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		nextCursor = pageCursor
		pages++
		for _, key := range page {
			if _, exists := seen[key]; exists {
				continue
			}
			if len(key) > maxScanKeyBytes-bytes {
				return connectors.ActionResult{}, fmt.Errorf("redis scan keys exceed %d bytes; use a narrower MATCH pattern", maxScanKeyBytes)
			}
			seen[key] = struct{}{}
			bytes += len(key)
			keys = append(keys, key)
		}
		if nextCursor == "0" {
			break
		}
	}
	sort.Strings(keys)
	output := map[string]any{
		"pattern": pattern, "cursor": cursor, "next_cursor": nextCursor,
		"keys": keys, "count": len(keys), "complete": nextCursor == "0",
		"scan_limit_reached": pages == maxScanPages && nextCursor != "0",
	}
	encoded, err := json.Marshal(output)
	if err != nil || len(encoded) > maxScanEncodedBytes {
		return connectors.ActionResult{}, fmt.Errorf("redis scan output exceeds encoded byte limit; use a narrower MATCH pattern")
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: truncateString(strings.Join(keys, "\n"), maxValueBytes),
	}, nil
}

func executeGetKey(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	key := strings.TrimSpace(stringValue(input, "key"))
	if key == "" {
		return connectors.ActionResult{}, fmt.Errorf("key is required")
	}
	limit := normalizeInt(input, "limit", defaultValueLimit, 1, maxValueLimit)
	maxBytes := normalizeInt(input, "max_bytes", defaultMaxValueBytes, 1, maxValueBytes)
	keyType, err := redisKeyType(client, key)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	ttlValue, err := client.Do("PTTL", key)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if ttlValue.kind != respInteger {
		return connectors.ActionResult{}, fmt.Errorf("unexpected PTTL response: expected an integer")
	}
	ttl := ttlValue.number
	output := map[string]any{"key": key, "type": keyType, "ttl_ms": ttl}
	switch keyType {
	case "none":
		output["exists"] = false
	case "string":
		value, err := client.Do("GET", key)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		text := truncateString(respString(value), maxBytes)
		output["value"] = text
		output["truncated"] = len(respString(value)) > maxBytes
	case "hash":
		value, err := client.Do("HGETALL", key)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		fields, err := redisStringMap(value, "HGETALL")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		output["value"] = limitStringMap(fields, limit, maxBytes)
	case "list":
		value, err := client.Do("LRANGE", key, "0", strconv.Itoa(limit-1))
		if err != nil {
			return connectors.ActionResult{}, err
		}
		items, err := redisStringSlice(value, "LRANGE")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		output["value"] = limitStrings(items, limit, maxBytes)
	case "set":
		items, err := redisScanCollection(client, "SSCAN", key, limit, maxBytes)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		output["value"] = items
	case "zset":
		value, err := client.Do("ZRANGE", key, "0", strconv.Itoa(limit-1), "WITHSCORES")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		items, err := redisStringSlice(value, "ZRANGE")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		pairs, err := scorePairs(items, maxBytes)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		output["value"] = pairs
	default:
		output["value"] = fmt.Sprintf("Preview for Redis type %q is not supported yet.", keyType)
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      output,
		DisplayText: redisKeyDisplay(output),
	}, nil
}

func executeSetString(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	key := strings.TrimSpace(stringValue(input, "key"))
	value := fmt.Sprint(input["value"])
	if key == "" {
		return connectors.ActionResult{}, fmt.Errorf("key is required")
	}
	args := []string{"SET", key, value}
	if ttl := normalizeInt(input, "ttl_seconds", 0, 0, 31_536_000); ttl > 0 {
		args = append(args, "EX", strconv.Itoa(ttl))
	}
	response, err := client.DoMutation(args...)
	if err != nil {
		return connectors.ActionResult{}, classifyRedisMutationError("set string", err)
	}
	if response.kind != respSimpleString || response.text != "OK" {
		return connectors.ActionResult{}, invalidRedisMutationResponse("set string", "simple-string OK")
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"key": key, "response": respString(response)},
		DisplayText: fmt.Sprintf("Set key %q.", key),
	}, nil
}

func executeExpireKey(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	key := strings.TrimSpace(stringValue(input, "key"))
	ttl := normalizeInt(input, "ttl_seconds", 0, -1, 31_536_000)
	if key == "" {
		return connectors.ActionResult{}, fmt.Errorf("key is required")
	}
	var value respValue
	var err error
	if ttl < 0 {
		value, err = client.DoMutation("PERSIST", key)
	} else {
		value, err = client.DoMutation("EXPIRE", key, strconv.Itoa(ttl))
	}
	if err != nil {
		return connectors.ActionResult{}, classifyRedisMutationError("update key expiry", err)
	}
	if value.kind != respInteger || (value.number != 0 && value.number != 1) {
		return connectors.ActionResult{}, invalidRedisMutationResponse("update key expiry", "integer 0 or 1")
	}
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"key": key, "changed": value.number == 1, "ttl_seconds": ttl},
		DisplayText: fmt.Sprintf("Updated TTL for key %q.", key),
	}, nil
}

func executeDeleteKeys(client *redisClient, input map[string]any) (connectors.ActionResult, error) {
	keys, err := normalizeKeys(input["keys"])
	if err != nil {
		return connectors.ActionResult{}, err
	}
	args := append([]string{"DEL"}, keys...)
	value, err := client.DoMutation(args...)
	if err != nil {
		return connectors.ActionResult{}, classifyRedisMutationError("delete keys", err)
	}
	if value.kind != respInteger || value.number < 0 || value.number > int64(len(keys)) {
		return connectors.ActionResult{}, invalidRedisMutationResponse("delete keys", "non-negative integer within the requested key count")
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"keys":    keys,
			"deleted": value.number,
		},
		DisplayText: fmt.Sprintf("Deleted %d key(s).", value.number),
	}, nil
}

func classifyRedisMutationError(operation string, err error) error {
	var dispatchErr *redisPostDispatchError
	if !errors.As(err, &dispatchErr) {
		return err
	}
	return connectors.ClassifyOutcomeUnknown(
		"resp_command",
		nil,
		fmt.Errorf("Redis %s outcome is unknown after dispatch; inspect key state before retrying: %w", operation, err),
	)
}

func invalidRedisMutationResponse(operation string, expected string) error {
	return connectors.ClassifyOutcomeUnknown(
		"response_validation",
		nil,
		fmt.Errorf("Redis %s returned an invalid response after dispatch; expected %s, inspect key state before retrying", operation, expected),
	)
}

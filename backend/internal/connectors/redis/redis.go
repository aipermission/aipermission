// Package redisconnector defines the Redis connector contract.
package redisconnector

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind    = "redis"
	Label   = "Redis / Valkey"
	Version = "0.2"

	ServerFamilyRedis  = "redis"
	ServerFamilyValkey = "valkey"

	ActionPing       = "ping"
	ActionInfo       = "info"
	ActionScanKeys   = "scan_keys"
	ActionGetKey     = "get_key"
	ActionSetString  = "set_string"
	ActionExpireKey  = "expire_key"
	ActionDeleteKeys = "delete_keys"

	defaultRedisHost      = "127.0.0.1"
	defaultRedisPort      = 6379
	defaultScanLimit      = 100
	maxScanLimit          = 1000
	defaultValueLimit     = 256
	maxValueLimit         = 1000
	defaultMaxValueBytes  = 128 << 10
	maxValueBytes         = 512 << 10
	maxRedisCommandReason = 2000
)

var (
	ErrUnsupportedAction = errors.New("unsupported redis connector action")
	ErrMissingTransport  = errors.New("redis connector network transport is unavailable")
	ErrMissingSecret     = errors.New("redis connector credential is missing required secret")
	ErrInvalidConfig     = errors.New("redis connector target config is invalid")
)

// Connector describes Redis as a connector-shaped target with bounded key
// browsing and explicit write/destructive actions.
type Connector struct{}

func New() Connector {
	return Connector{}
}

func (Connector) Kind() string {
	return Kind
}

func (Connector) Label() string {
	return Label
}

func (Connector) Version() string {
	return Version
}

func (Connector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, redisCommandTimeout)
	defer cancel()
	client, err := openRedisClient(ctx, runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer client.Close()
	switch action.ActionName {
	case ActionPing:
		return executePing(client)
	case ActionInfo:
		return executeInfo(client, action.Payload)
	case ActionScanKeys:
		return executeScanKeys(client, action.Payload)
	case ActionGetKey:
		return executeGetKey(client, action.Payload)
	case ActionSetString:
		return executeSetString(client, action.Payload)
	case ActionExpireKey:
		return executeExpireKey(client, action.Payload)
	case ActionDeleteKeys:
		return executeDeleteKeys(client, action.Payload)
	default:
		return connectors.ActionResult{}, ErrUnsupportedAction
	}
}

func (connector Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	client, err := openRedisClient(ctx, runtime)
	if err != nil {
		return connectors.TestResult{Status: classifyRedisTestError(err), Message: err.Error()}, nil
	}
	defer client.Close()
	value, err := client.Do("PING")
	if err != nil {
		return connectors.TestResult{Status: classifyRedisTestError(err), Message: err.Error()}, nil
	}
	configuredFamily := serverFamily(runtime.Target)
	details := map[string]any{
		"response":                 respString(value),
		"database":                 redisDatabase(runtime.Target),
		"configured_server_family": configuredFamily,
	}
	detectedFamily := configuredFamily
	message := serverFamilyLabel(configuredFamily) + " connection ok."
	if identity, detectErr := detectRedisServer(client); detectErr == nil {
		detectedFamily = identity.Family
		for key, item := range identity.details() {
			details[key] = item
		}
		details["server_family_match"] = detectedFamily == configuredFamily
		message = serverFamilyLabel(detectedFamily) + " connection ok."
		if detectedFamily != configuredFamily {
			message = fmt.Sprintf(
				"%s connection ok; target is configured as %s.",
				serverFamilyLabel(detectedFamily),
				serverFamilyLabel(configuredFamily),
			)
		}
	} else {
		details["server_detection"] = "unavailable"
	}
	return connectors.TestResult{
		Status:  connectors.TestOK,
		Message: message,
		Details: details,
	}, nil
}

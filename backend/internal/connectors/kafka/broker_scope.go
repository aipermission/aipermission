package kafka

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

type brokerScope map[string]struct{}

func configuredBrokerScope(config map[string]any) ([]string, brokerScope, error) {
	for _, key := range []string{"bootstrap_brokers", "advertised_brokers"} {
		if value, exists := config[key]; exists {
			if _, ok := value.(string); !ok {
				return nil, nil, fmt.Errorf("%s must be text", key)
			}
		}
	}
	for _, key := range []string{"tls_enabled", "allow_insecure_plain_sasl", "allow_insecure_scram_sasl"} {
		if value, exists := config[key]; exists {
			if _, ok := value.(bool); !ok {
				return nil, nil, fmt.Errorf("%s must be a boolean", key)
			}
		}
	}
	bootstrap, _ := config["bootstrap_brokers"].(string)
	advertised, _ := config["advertised_brokers"].(string)
	brokers, err := parseBrokerList(bootstrap)
	if err != nil {
		return nil, nil, err
	}
	additional, err := parseScopedBrokerList(advertised, "advertised", maxClusterBrokers)
	if err != nil {
		return nil, nil, err
	}
	scope := make(brokerScope, len(brokers)+len(additional))
	for _, endpoints := range [][]string{brokers, additional} {
		for _, address := range endpoints {
			scope[address] = struct{}{}
		}
	}
	return brokers, scope, nil
}

func (scope brokerScope) resolve(address string) (string, int, error) {
	host, port, err := splitBrokerAddress(address)
	if err != nil {
		return "", 0, err
	}
	if _, allowed := scope[net.JoinHostPort(host, strconv.Itoa(port))]; !allowed {
		return "", 0, fmt.Errorf("broker endpoint %q is outside the configured scope; add its host:port to advertised_brokers", address)
	}
	return host, port, nil
}

func parseScopedBrokerList(value, label string, limit int) ([]string, error) {
	if len(value) > 65536 {
		return nil, fmt.Errorf("%s broker list exceeds 65536 bytes", label)
	}
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' ' })
	seen := map[string]bool{}
	brokers := make([]string, 0, min(len(fields), limit))
	for _, field := range fields {
		host, port, err := splitBrokerAddress(field)
		if err != nil {
			return nil, err
		}
		address := net.JoinHostPort(host, strconv.Itoa(port))
		if !seen[address] {
			seen[address] = true
			brokers = append(brokers, address)
			if len(brokers) > limit {
				return nil, fmt.Errorf("%s broker count must not exceed %d", label, limit)
			}
		}
	}
	return brokers, nil
}

func canonicalBrokerHost(host string) (string, error) {
	if len(host) == 0 || len(host) > 253 {
		return "", fmt.Errorf("broker host must contain 1 to 253 bytes")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.String(), nil
	}
	if strings.Trim(host, "0123456789.") == "" {
		return "", fmt.Errorf("broker host must be a DNS name or IP address")
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("broker host has an invalid DNS label")
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
				return "", fmt.Errorf("broker host must use an ASCII DNS name or IP address")
			}
		}
	}
	return strings.ToLower(host), nil
}

func (*Connector) ValidateTargetConfig(config map[string]any) error {
	_, _, err := configuredBrokerScope(config)
	return err
}

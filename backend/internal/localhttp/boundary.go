package localhttp

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

func IsLocalhostHeader(hostHeader string) bool {
	hostHeader = strings.TrimSpace(hostHeader)
	if hostHeader == "" {
		return false
	}
	host, ok := parseHostHeader(hostHeader)
	return ok && IsLoopbackHost(host)
}

func IsLocalRemoteAddr(remoteAddr string) bool {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return false
	}
	host, port, err := net.SplitHostPort(remoteAddr)
	if err != nil || !validTCPPort(port) {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func IsLoopbackOrigin(origin string) bool {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return IsLoopbackHost(parsed.Hostname())
}

func IsSameOrigin(origin string, host string) bool {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || !IsLoopbackOrigin(origin) {
		return false
	}
	return strings.EqualFold(parsed.Host, strings.TrimSpace(host))
}

func IsSameOriginReferer(referer string, host string) bool {
	parsed, err := url.Parse(strings.TrimSpace(referer))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	origin := parsed.Scheme + "://" + parsed.Host
	return IsSameOrigin(origin, host)
}

func parseHostHeader(value string) (string, bool) {
	if strings.HasPrefix(value, "[") {
		if strings.HasSuffix(value, "]") {
			host := strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
			return host, host != ""
		}
		host, port, err := net.SplitHostPort(value)
		return host, err == nil && host != "" && validTCPPort(port)
	}
	if strings.Count(value, ":") == 0 {
		return value, true
	}
	host, port, err := net.SplitHostPort(value)
	return host, err == nil && host != "" && validTCPPort(port)
}

func validTCPPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port > 0 && port <= 65535
}

func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

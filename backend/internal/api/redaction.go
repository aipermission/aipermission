package api

import (
	"context"
	"regexp"
	"strings"
)

var (
	privateKeyBlockPattern = regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	bearerTokenPattern     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	namedSecretPattern     = regexp.MustCompile(`(?i)\b(password|passwd|pwd|token|api[_-]?key|secret|access[_-]?key|private[_-]?key)\b(\s*[:=]\s*)(['"]?)([^\s'"]+)`)
	commonTokenPattern     = regexp.MustCompile(`\b(ghp|gho|ghu|ghs|github_pat|sk|xoxb|xoxp|xapp|ya29)[A-Za-z0-9_./=-]{16,}\b`)
)

type compiledRedactionRule struct {
	ID      int64
	Pattern string
	Regex   *regexp.Regexp
}

func (s *Server) redactForPersistence(ctx context.Context, runtime *databaseRuntime, value string) string {
	if value == "" || s.redactionMode(ctx, runtime) == redactionModeOff {
		return value
	}
	value = redactBasic(value)
	return s.redactCustom(ctx, runtime, value)
}

func (s *Server) runtimeRedactor(runtime *databaseRuntime) func(string) string {
	return func(value string) string {
		return s.redactForPersistence(context.Background(), runtime, value)
	}
}

func (s *Server) redactionMode(ctx context.Context, runtime *databaseRuntime) string {
	if runtime == nil || runtime.database == nil {
		return defaultRedactionMode
	}
	settings, err := readSecuritySettings(ctx, runtime)
	if err != nil {
		return defaultRedactionMode
	}
	return normalizeRedactionMode(settings.RedactionMode)
}

func redactBasic(value string) string {
	if value == "" {
		return value
	}
	value = privateKeyBlockPattern.ReplaceAllString(value, "[REDACTED PRIVATE KEY]")
	value = bearerTokenPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = namedSecretPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := namedSecretPattern.FindStringSubmatch(match)
		if len(parts) < 4 {
			return "[REDACTED]"
		}
		if parts[1] == "PWD" && len(parts) >= 5 && strings.HasPrefix(parts[4], "/") {
			return match
		}
		return parts[1] + parts[2] + parts[3] + "[REDACTED]"
	})
	value = commonTokenPattern.ReplaceAllStringFunc(value, func(match string) string {
		prefix := match
		if idx := strings.IndexAny(match, "_-"); idx > 0 && idx < 12 {
			prefix = match[:idx+1]
		} else if len(match) > 4 {
			prefix = match[:4]
		}
		return prefix + "[REDACTED]"
	})
	return value
}

func (s *Server) redactCustom(ctx context.Context, runtime *databaseRuntime, value string) string {
	if runtime == nil || runtime.database == nil || value == "" {
		return value
	}
	rules, err := s.compiledRedactionRules(ctx, runtime)
	if err != nil {
		return value
	}
	for _, rule := range rules {
		value = rule.Regex.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}

func (s *Server) compiledRedactionRules(ctx context.Context, runtime *databaseRuntime) ([]compiledRedactionRule, error) {
	runtime.redactionMu.RLock()
	if runtime.redactionLoaded {
		rules := append([]compiledRedactionRule(nil), runtime.redactionRules...)
		runtime.redactionMu.RUnlock()
		return rules, nil
	}
	runtime.redactionMu.RUnlock()

	runtime.redactionMu.Lock()
	defer runtime.redactionMu.Unlock()
	if runtime.redactionLoaded {
		return append([]compiledRedactionRule(nil), runtime.redactionRules...), nil
	}
	items, err := readRedactionRules(ctx, runtime, true)
	if err != nil {
		return nil, err
	}
	rules := make([]compiledRedactionRule, 0, len(items))
	for _, item := range items {
		pattern, err := regexp.Compile(item.Pattern)
		if err != nil {
			continue
		}
		rules = append(rules, compiledRedactionRule{
			ID:      item.ID,
			Pattern: item.Pattern,
			Regex:   pattern,
		})
	}
	runtime.redactionRules = rules
	runtime.redactionLoaded = true
	return append([]compiledRedactionRule(nil), runtime.redactionRules...), nil
}

func (s *Server) invalidateRedactionRules(runtime *databaseRuntime) {
	if runtime == nil {
		return
	}
	runtime.redactionMu.Lock()
	runtime.redactionRules = nil
	runtime.redactionLoaded = false
	runtime.redactionMu.Unlock()
}

package securitypolicy

import (
	"context"
	"regexp"
	"strings"
)

var (
	privateKeyBlockPattern = regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	bearerTokenPattern     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	namedSecretPattern     = regexp.MustCompile(`(?i)\b(` + namedSecretKeys + `)\b(\s*[:=]\s*)('(?:\\.|[^'\\])*'|"(?:\\.|[^"\\])*"|\[REDACTED(?: CREDENTIAL| VAULT VALUE| PRIVATE KEY)?\][^\s'";,]*|['"]?[^\s'"]+)`)
	commonTokenPattern     = regexp.MustCompile(`\b(ghp|gho|ghu|ghs|github_pat|sk|xoxb|xoxp|xapp|ya29)[A-Za-z0-9_./=-]{16,}\b`)
)

type compiledRule struct {
	regex *regexp.Regexp
}

const redactionFailureMarker = "[REDACTED]"
const namedSecretKeys = `password|passwd|pwd|token|api[_-]?key|secret|access[_-]?key|private[_-]?key`

func (s *Service) Mode(ctx context.Context) string {
	settings, err := s.ReadSettings(ctx)
	if err != nil {
		return RedactionModeBasic
	}
	return NormalizeRedactionMode(settings.RedactionMode)
}

func (s *Service) Redact(ctx context.Context, value string) string {
	if value == "" || s.Mode(ctx) == RedactionModeOff {
		return value
	}
	return s.RedactCustom(ctx, RedactBasic(value))
}

func (s *Service) RedactCustom(ctx context.Context, value string) string {
	if s == nil || s.database == nil || value == "" {
		return value
	}
	rules, err := s.compiledRules(ctx)
	if err != nil {
		return redactionFailureMarker
	}
	for _, rule := range rules {
		value = rule.regex.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}

func (s *Service) PrepareRedactor(ctx context.Context) func(string) string {
	if s == nil || s.database == nil {
		return RedactBasic
	}
	if s.Mode(ctx) == RedactionModeOff {
		return func(value string) string { return value }
	}
	rules, err := s.compiledRules(ctx)
	if err != nil {
		return failClosedRedactor
	}
	return func(value string) string {
		value = RedactBasic(value)
		for _, rule := range rules {
			value = rule.regex.ReplaceAllString(value, "[REDACTED]")
		}
		return value
	}
}

func failClosedRedactor(value string) string {
	if value == "" {
		return ""
	}
	return redactionFailureMarker
}

func (s *Service) Redactor() func(string) string {
	return func(value string) string {
		return s.Redact(context.Background(), value)
	}
}

func (s *Service) compiledRules(ctx context.Context) ([]compiledRule, error) {
	s.rulesMu.RLock()
	if s.rulesLoaded {
		rules := append([]compiledRule(nil), s.compiled...)
		s.rulesMu.RUnlock()
		return rules, nil
	}
	s.rulesMu.RUnlock()
	s.rulesMu.Lock()
	defer s.rulesMu.Unlock()
	if s.rulesLoaded {
		return append([]compiledRule(nil), s.compiled...), nil
	}
	items, err := listRules(ctx, s.database, true)
	if err != nil {
		return nil, err
	}
	rules := make([]compiledRule, 0, len(items))
	for _, item := range items {
		pattern, err := regexp.Compile(item.Pattern)
		if err == nil {
			rules = append(rules, compiledRule{regex: pattern})
		}
	}
	s.compiled, s.rulesLoaded = rules, true
	return append([]compiledRule(nil), rules...), nil
}

func RedactBasic(value string) string {
	// Masking malformed text can expose another recognized shape. Only return a
	// stable projection; withhold ambiguous text if bounded normalization stalls.
	for range 3 {
		redacted := redactBasicJSONText(value, 0)
		if redacted == value {
			return value
		}
		value = redacted
	}
	return redactionFailureMarker
}

func redactBasicText(value string) string {
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
		secret, openingQuote, closingQuote := namedSecretValue(parts[3])
		if parts[1] == "PWD" && strings.HasPrefix(secret, "/") {
			return match
		}
		if isRedactionMarker(secret) {
			return match
		}
		return parts[1] + parts[2] + openingQuote + "[REDACTED]" + closingQuote
	})
	return commonTokenPattern.ReplaceAllStringFunc(value, func(match string) string {
		prefix := match
		if index := strings.IndexAny(match, "_-"); index > 0 && index < 12 {
			prefix = match[:index+1]
		} else if len(match) > 4 {
			prefix = match[:4]
		}
		return prefix + "[REDACTED]"
	})
}

func isRedactionMarker(value string) bool {
	return value == "[REDACTED]" || value == "[REDACTED CREDENTIAL]" ||
		value == "[REDACTED VAULT VALUE]" || value == "[REDACTED PRIVATE KEY]"
}

func namedSecretValue(value string) (secret, openingQuote, closingQuote string) {
	if len(value) == 0 || value[0] != '\'' && value[0] != '"' {
		return value, "", ""
	}
	quote := value[:1]
	if len(value) > 1 && value[len(value)-1:] == quote {
		return value[1 : len(value)-1], quote, quote
	}
	return value[1:], quote, ""
}

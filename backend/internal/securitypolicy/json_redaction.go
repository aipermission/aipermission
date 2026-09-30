package securitypolicy

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	jsonStringFieldPattern = regexp.MustCompile(`("(?:\\.|[^"\\\x00-\x1f])*")(\s*:\s*)("(?:\\.|[^"\\\r\n])*")`)
	jsonStringPattern      = regexp.MustCompile(`"(?:\\.|[^"\\\x00-\x1f])*"`)
	jsonSecretKeyPattern   = regexp.MustCompile(`(?i)^(?:` + namedSecretKeys + `)$`)
)

const maxJSONRedactionDepth = 32

// Decode string leaves, not whole maps: preserve duplicate keys, numeric precision,
// formatting, and JSON snippets inside surrounding log text.
func redactBasicJSONText(value string, depth int) string {
	if depth >= maxJSONRedactionDepth {
		return redactionFailureMarker
	}
	if json.Valid([]byte(value)) {
		return redactJSONDocument(value, depth)
	}
	return redactJSONFragments(value, depth)
}

func redactJSONFields(value string, depth int) string {
	fields := jsonStringFieldPattern.FindAllStringSubmatchIndex(value, -1)
	if len(fields) == 0 {
		return redactBasicText(value)
	}
	var output strings.Builder
	previous := 0
	assignments := namedSecretPattern.FindAllStringIndex(value, -1)
	assignment := 0
	for _, field := range fields {
		if inPlaintextAssignment(field[0], field[1], assignments, &assignment) {
			continue
		}
		output.WriteString(redactBasicText(value[previous:field[0]]))
		output.WriteString(redactJSONStringField(value[field[0]:field[1]], depth))
		previous = field[1]
	}
	output.WriteString(redactBasicText(value[previous:]))
	return output.String()
}

func redactJSONDocument(value string, depth int) string {
	var output strings.Builder
	previous := 0
	for _, span := range jsonStringPattern.FindAllStringIndex(value, -1) {
		if span[0] < previous {
			continue
		}
		var literal string
		_ = json.Unmarshal([]byte(value[span[0]:span[1]]), &literal)
		following := strings.TrimLeft(value[span[1]:], " \t\r\n")
		end, replacement := span[1], value[span[0]:span[1]]
		if strings.HasPrefix(following, ":") {
			if jsonSecretKeyPattern.MatchString(literal) {
				end, replacement = redactJSONMember(value, span, literal)
			} else {
				replacement = replaceJSONString(replacement, literal, redactBasicText(literal))
			}
		} else {
			replacement = replaceJSONString(replacement, literal, redactBasicJSONText(literal, depth+1))
		}
		output.WriteString(value[previous:span[0]])
		output.WriteString(replacement)
		previous = end
	}
	output.WriteString(value[previous:])
	return output.String()
}

func redactJSONMember(document string, keySpan []int, key string) (int, string) {
	following := strings.TrimLeft(document[keySpan[1]:], " \t\r\n")
	valueText := strings.TrimLeft(following[1:], " \t\r\n")
	valueStart := len(document) - len(valueText)
	decoder := json.NewDecoder(strings.NewReader(valueText))
	var raw json.RawMessage
	_ = decoder.Decode(&raw)
	end := valueStart + int(decoder.InputOffset())
	var literal string
	if json.Unmarshal(raw, &literal) == nil {
		if isRedactionMarker(literal) {
			return end, document[keySpan[0]:end]
		}
		if key == "PWD" && strings.HasPrefix(literal, "/") {
			return end, document[keySpan[0]:valueStart] + replaceJSONString(string(raw), literal, redactBasicText(literal))
		}
	}
	return end, document[keySpan[0]:valueStart] + `"[REDACTED]"`
}

func redactJSONStringField(field string, depth int) string {
	parts := jsonStringFieldPattern.FindStringSubmatchIndex(field)
	var key, value string
	if json.Unmarshal([]byte(field[parts[2]:parts[3]]), &key) != nil {
		return redactBasicText(field)
	}
	keyText := replaceJSONString(field[parts[2]:parts[3]], key, redactBasicText(key))
	prefix := keyText + field[parts[4]:parts[5]]
	if json.Unmarshal([]byte(field[parts[6]:parts[7]]), &value) != nil {
		return prefix + `"[REDACTED]"`
	}
	redacted := value
	if jsonSecretKeyPattern.MatchString(key) && !(key == "PWD" && strings.HasPrefix(value, "/")) && !isRedactionMarker(value) {
		redacted = redactionFailureMarker
	} else {
		redacted = redactBasicJSONText(value, depth+1)
	}
	return prefix + replaceJSONString(field[parts[6]:parts[7]], value, redacted)
}

func replaceJSONString(original, value, redacted string) string {
	if value == redacted {
		return original
	}
	encoded, _ := json.Marshal(redacted)
	return string(encoded)
}

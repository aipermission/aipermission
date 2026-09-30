package securitypolicy

import (
	"encoding/json"
	"strings"
)

func redactJSONFragments(value string, depth int) string {
	value = privateKeyBlockPattern.ReplaceAllString(value, "[REDACTED PRIVATE KEY]")
	assignments := namedSecretPattern.FindAllStringIndex(value, -1)
	assignment, previous, failures := 0, 0, 0
	var output strings.Builder
	for cursor := 0; cursor < len(value) && failures < 32; {
		offset := strings.IndexAny(value[cursor:], "{[")
		if offset < 0 {
			break
		}
		start := cursor + offset
		cursor = start + 1
		if markerEnd := jsonRedactionMarkerEnd(value, start); markerEnd > start {
			cursor = markerEnd
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(value[start:]))
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			failures++
			continue
		}
		end := start + int(decoder.InputOffset())
		if inPlaintextAssignment(start, end, assignments, &assignment) {
			cursor = assignments[assignment][1]
			continue
		}
		output.WriteString(redactJSONFields(value[previous:start], depth))
		output.WriteString(redactJSONDocument(value[start:end], depth))
		previous, cursor = end, end
	}
	if failures >= 32 {
		output.WriteString(redactionFailureMarker)
		return output.String()
	}
	output.WriteString(redactJSONFields(value[previous:], depth))
	return output.String()
}

func jsonRedactionMarkerEnd(value string, start int) int {
	limit := min(len(value), start+len("[REDACTED PRIVATE KEY]"))
	if end := strings.IndexByte(value[start:limit], ']'); end >= 0 && isRedactionMarker(value[start:start+end+1]) {
		return start + end + 1
	}
	return start
}

func inPlaintextAssignment(offset, end int, spans [][]int, index *int) bool {
	for *index < len(spans) && spans[*index][1] <= offset {
		*index++
	}
	return *index < len(spans) && spans[*index][0] <= offset && end <= spans[*index][1]
}

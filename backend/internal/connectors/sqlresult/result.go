// Package sqlresult builds bounded, JSON-safe tabular connector results.
package sqlresult

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

type Result struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	RowCount  int              `json:"row_count"`
	MaxRows   int              `json:"max_rows"`
	Truncated bool             `json:"truncated"`
}

type Builder struct {
	result          Result
	maxBytes        int
	maxCellBytes    int
	truncatedSuffix string
	estimatedBytes  int
}

func NewBuilder(columns []string, maxRows, maxBytes, maxCellBytes int, truncatedSuffix string) *Builder {
	result := Result{
		Columns: uniqueColumns(columns),
		Rows:    make([]map[string]any, 0, min(maxRows, 100)),
		MaxRows: maxRows,
	}
	builder := &Builder{
		result:          result,
		maxBytes:        maxBytes,
		maxCellBytes:    maxCellBytes,
		truncatedSuffix: truncatedSuffix,
	}
	builder.estimatedBytes = encodedSize(builder.result.ToMap(maxBytes, nil))
	return builder
}

// Add appends one row. It returns false when the row or byte limit is reached.
func (b *Builder) Add(values []any, normalize func(any) any) bool {
	if len(b.result.Rows) >= b.result.MaxRows {
		b.result.Truncated = true
		return false
	}
	row := make(map[string]any, len(b.result.Columns))
	for index, column := range b.result.Columns {
		var value any
		if index < len(values) {
			value = normalize(values[index])
		}
		bounded, truncated := BoundValue(value, b.maxCellBytes, b.truncatedSuffix)
		row[column] = bounded
		b.result.Truncated = b.result.Truncated || truncated
	}
	rowBytes := encodedSize(row)
	if b.estimatedBytes+rowBytes+1 > b.maxBytes {
		b.result.Truncated = true
		return false
	}
	b.result.Rows = append(b.result.Rows, row)
	b.result.RowCount = len(b.result.Rows)
	b.estimatedBytes += rowBytes + 1
	return true
}

func (b *Builder) Result(extras map[string]any) Result {
	return b.result.Fit(b.maxBytes, extras)
}

func (r Result) Fit(maxBytes int, extras map[string]any) Result {
	r.RowCount = len(r.Rows)
	if maxBytes < 1 || encodedSize(r.ToMap(maxBytes, extras)) <= maxBytes {
		return r
	}
	r.Truncated = true
	low, high := 0, len(r.Rows)
	for low < high {
		middle := (low + high + 1) / 2
		candidate := r
		candidate.Rows = r.Rows[:middle]
		candidate.RowCount = middle
		if encodedSize(candidate.ToMap(maxBytes, extras)) <= maxBytes {
			low = middle
		} else {
			high = middle - 1
		}
	}
	r.Rows = r.Rows[:low]
	r.RowCount = low
	if encodedSize(r.ToMap(maxBytes, extras)) <= maxBytes {
		return r
	}
	low, high = 0, len(r.Columns)
	for low < high {
		middle := (low + high + 1) / 2
		candidate := r
		candidate.Columns = r.Columns[:middle]
		if encodedSize(candidate.ToMap(maxBytes, extras)) <= maxBytes {
			low = middle
		} else {
			high = middle - 1
		}
	}
	r.Columns = r.Columns[:low]
	return r
}

func (r Result) ToMap(maxBytes int, extras map[string]any) map[string]any {
	output := map[string]any{
		"columns": r.Columns, "rows": r.Rows, "row_count": r.RowCount,
		"max_rows": r.MaxRows, "max_bytes": maxBytes, "truncated": r.Truncated,
	}
	for key, value := range extras {
		output[key] = value
	}
	return output
}

func (r Result) DisplayText() string {
	text := fmt.Sprintf("%d row", r.RowCount)
	if r.RowCount != 1 {
		text += "s"
	}
	if r.Truncated {
		text += " (truncated)"
	}
	return text
}

func BoundValue(value any, limit int, suffix string) (any, bool) {
	if limit <= 0 {
		return "", true
	}
	if text, ok := value.(string); ok {
		if len(text) <= limit {
			return text, false
		}
		return truncateString(text, limit, suffix), true
	}
	encoded, err := json.Marshal(value)
	if err == nil && len(encoded) <= limit {
		return value, false
	}
	return truncateString(fmt.Sprint(value), limit, suffix), true
}

func uniqueColumns(columns []string) []string {
	result := make([]string, 0, len(columns))
	used := make(map[string]struct{}, len(columns))
	for index, column := range columns {
		base := column
		if base == "" {
			base = fmt.Sprintf("column_%d", index+1)
		}
		candidate := base
		for suffix := 2; ; suffix++ {
			if _, exists := used[candidate]; !exists {
				break
			}
			candidate = fmt.Sprintf("%s_%d", base, suffix)
		}
		used[candidate] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

func truncateString(value string, limit int, suffix string) string {
	if len(value) <= limit {
		return value
	}
	if limit <= len(suffix) {
		return truncateUTF8(suffix, limit)
	}
	return truncateUTF8(value, limit-len(suffix)) + suffix
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 || value == "" {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

func encodedSize(value any) int {
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return len(encoded)
}

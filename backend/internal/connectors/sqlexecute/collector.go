package sqlexecute

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/connectors/sqlresult"
)

type Command struct {
	Tag          string `json:"tag"`
	ReportedRows string `json:"reported_rows"`
}

// Collector bounds retained output only. Adapters must continue reading all
// commands after truncation; canceling on a row cap could skip later mutations.
type Collector struct {
	results       []sqlresult.Result
	commands      []Command
	notices       []string
	active        *sqlresult.Builder
	inResult      bool
	discardRows   bool
	columns       int
	rowLimit      int
	rows          int
	resultCount   int
	truncated     bool
	retainedBytes int
	metadataBytes int
}

func NewCollector(rowLimit int) *Collector {
	return &Collector{rowLimit: rowLimit, results: []sqlresult.Result{}, commands: []Command{}, notices: []string{}}
}

func (c *Collector) BeginResult(columns []string) error {
	if c.inResult {
		return fmt.Errorf("previous SQL result is still active")
	}
	c.columns = len(columns)
	c.inResult = true
	c.discardRows = false
	c.resultCount++
	budget := MaxOutputBytes - 100000 - c.retainedBytes
	if len(columns) > 256 || c.resultCount > 32 || budget < 1024 || c.rows >= c.rowLimit {
		c.truncated = true
		return nil
	}
	c.active = sqlresult.NewBuilder(columns, c.rowLimit-c.rows, budget, MaxCellBytes, "...[truncated]")
	return nil
}

func (c *Collector) AddRow(values []any) error {
	if !c.inResult || len(values) != c.columns {
		return fmt.Errorf("SQL row does not match its column metadata")
	}
	if !c.RetainingRows() {
		c.truncated = true
		return nil
	}
	if !c.active.Add(values, func(value any) any { return value }) {
		c.truncated = true
		c.discardRows = true
	}
	return nil
}

// RetainingRows lets native adapters drain an already truncated result without
// allocating/normalizing another row for the retained output.
func (c *Collector) RetainingRows() bool { return c.active != nil && !c.discardRows }

// OmitCurrentResult keeps protocol/result counts without retaining unsupported
// values. The adapter must still consume the result and the current batch.
func (c *Collector) OmitCurrentResult() {
	c.active = nil
	c.discardRows = true
	c.truncated = true
}

func (c *Collector) EndResult() {
	if c.active != nil {
		result := c.active.Result(nil)
		c.results = append(c.results, result)
		c.rows += result.RowCount
		c.truncated = c.truncated || result.Truncated
		encoded, _ := json.Marshal(result)
		c.retainedBytes += len(encoded) + 1
	}
	c.active = nil
	c.columns = 0
	c.inResult = false
}

func (c *Collector) Command(tag string, rows int64) {
	if len(c.commands) >= 128 {
		c.truncated = true
		return
	}
	bounded, cut := sqlresult.BoundValue(tag, 256, "...[truncated]")
	command := Command{Tag: bounded.(string), ReportedRows: strconv.FormatInt(rows, 10)}
	if !c.reserveMetadata(command) {
		return
	}
	c.commands = append(c.commands, command)
	c.truncated = c.truncated || cut
}

func (c *Collector) Notice(message string) {
	if len(c.notices) >= 32 {
		c.truncated = true
		return
	}
	bounded, cut := sqlresult.BoundValue(message, 512, "...[truncated]")
	if !c.reserveMetadata(bounded) {
		return
	}
	c.notices = append(c.notices, bounded.(string))
	c.truncated = c.truncated || cut
}

func (c *Collector) reserveMetadata(value any) bool {
	encoded, _ := json.Marshal(value)
	if c.metadataBytes+len(encoded)+1 > 60000 {
		c.truncated = true
		return false
	}
	c.metadataBytes += len(encoded) + 1
	return true
}

func (c *Collector) Output(completedBatches int) map[string]any {
	return map[string]any{
		"result_sets": c.results, "commands": c.commands, "notices": c.notices,
		"row_count": c.rows, "result_set_count": c.resultCount, "completed_batches": completedBatches,
		"truncated": c.truncated, "max_rows": c.rowLimit, "max_bytes": MaxOutputBytes,
		"retry_safe": false, "transaction_control": "script_defined",
	}
}

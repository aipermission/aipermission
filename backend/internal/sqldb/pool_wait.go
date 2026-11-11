package sqldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// PoolWaitReporter captures bounded code-location evidence after delivery guard
// deadlines. Program counters contain no SQL, context values or function arguments.
type PoolWaitReporter struct {
	mu   sync.Mutex
	last time.Time
	emit func(poolWaitSnapshot)
}

type poolWaitSnapshot struct {
	Open         int           `json:"open"`
	InUse        int           `json:"in_use"`
	Idle         int           `json:"idle"`
	WaitCount    int64         `json:"wait_count"`
	WaitMS       int64         `json:"wait_ms"`
	CodeScope    string        `json:"code_scope"`
	Truncated    bool          `json:"truncated"`
	DatabaseCode []poolCodeRun `json:"database_code"`
}

type poolCodeRun struct {
	Kind      string   `json:"kind"`
	Count     int      `json:"count"`
	Functions []string `json:"functions"`
}

func (reporter *PoolWaitReporter) Report(database *sql.DB, err error) {
	if reporter == nil || database == nil || !errors.Is(err, context.DeadlineExceeded) || !reporter.admit(time.Now()) {
		return
	}
	stats := database.Stats()
	code, truncated := databaseCodeRuns()
	snapshot := poolWaitSnapshot{
		Open: stats.OpenConnections, InUse: stats.InUse, Idle: stats.Idle,
		WaitCount: stats.WaitCount, WaitMS: stats.WaitDuration.Milliseconds(),
		DatabaseCode: code, Truncated: truncated, CodeScope: "process",
	}
	if reporter.emit != nil {
		reporter.emit(snapshot)
		return
	}
	encoded, _ := json.Marshal(snapshot)
	log.Printf("delivery database deadline diagnostics=%s", encoded)
}

func (reporter *PoolWaitReporter) admit(now time.Time) bool {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if !reporter.last.IsZero() && now.Sub(reporter.last) < 30*time.Second {
		return false
	}
	reporter.last = now
	return true
}

func databaseCodeRuns() ([]poolCodeRun, bool) {
	const maxRecords = 4096
	count, _ := runtime.GoroutineProfile(nil)
	if count > maxRecords {
		return nil, true
	}
	records := make([]runtime.StackRecord, min(count+64, maxRecords))
	count, complete := runtime.GoroutineProfile(records)
	if !complete {
		return nil, true
	}
	runs := make(map[string]poolCodeRun)
	for _, record := range records[:count] {
		kind, functions := databaseCodeFrames(record.Stack())
		if kind == "" {
			continue
		}
		key := kind + ":" + strings.Join(functions, ";")
		run := runs[key]
		run.Kind, run.Functions, run.Count = kind, functions, run.Count+1
		runs[key] = run
	}
	result := make([]poolCodeRun, 0, len(runs))
	for _, run := range runs {
		result = append(result, run)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Kind+strings.Join(result[i].Functions, ";") < result[j].Kind+strings.Join(result[j].Functions, ";")
	})
	return result[:min(len(result), 8)], len(result) > 8
}

func databaseCodeFrames(pcs []uintptr) (string, []string) {
	frames := runtime.CallersFrames(pcs)
	kind := ""
	functions := []string{}
	for {
		frame, more := frames.Next()
		name := frame.Function
		if strings.HasPrefix(name, "github.com/SE-I-T-Digital/go-sqlcipher.") {
			kind = "driver"
		} else if kind == "" && name == "database/sql.(*DB).conn" {
			kind = "pool_wait"
		}
		if len(functions) < 16 && len(name) <= 256 && (strings.HasPrefix(name, "database/sql.") ||
			strings.HasPrefix(name, "github.com/aipermission/aipermission/backend/") ||
			strings.HasPrefix(name, "github.com/SE-I-T-Digital/go-sqlcipher.")) {
			functions = append(functions, name)
		}
		if !more {
			return kind, functions
		}
	}
}

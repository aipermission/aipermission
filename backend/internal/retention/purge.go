package retention

import (
	"context"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type configuredTarget struct {
	name string
	days int
}

func applySettings(ctx context.Context, executor sqldb.Executor, store repository, settings Settings) (map[string]int64, error) {
	deleted := map[string]int64{}
	for _, target := range []configuredTarget{
		{name: "history", days: settings.HistoryDays},
		{name: "audit", days: settings.AuditDays},
		{name: "console", days: settings.ConsoleDays},
		{name: "messages", days: settings.MessageDays},
	} {
		if target.days == 0 {
			continue
		}
		count, err := purgeTarget(ctx, executor, store, target.name, target.days)
		if err != nil {
			return nil, err
		}
		deleted[target.name] = count
	}
	idempotency, err := store.PurgeExpiredIdempotency(ctx, executor)
	if err != nil {
		return nil, err
	}
	if idempotency > 0 {
		deleted["idempotency"] = idempotency
	}
	return deleted, nil
}

func purgeTarget(ctx context.Context, executor sqldb.Executor, store repository, target string, days int) (int64, error) {
	if days < 1 {
		return 0, ValidationError("days must be at least 1")
	}
	cutoff := "-" + strconv.Itoa(days) + " days"
	switch target {
	case "history":
		return store.PurgeHistory(ctx, executor, cutoff)
	case "audit":
		return store.PurgeAudit(ctx, executor, cutoff)
	case "console":
		return store.PurgeConsole(ctx, executor, cutoff)
	case "messages":
		return store.PurgeMessages(ctx, executor, cutoff)
	default:
		return 0, ValidationError("invalid retention target")
	}
}

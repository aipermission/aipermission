package restcontract

func capacityReportSchema() map[string]any {
	item := objectSchema(map[string]any{
		"token_id": nonBlankStringSchema(), "name": stringSchema(), "rows": integerSchema(),
		"stored_bytes": integerSchema(), "reserved_bytes": integerSchema(),
		"running": integerSchema(), "pending": integerSchema(),
		"level": enumSchema("ok", "warning", "critical", "exhausted"),
	}, []string{"token_id", "name", "rows", "stored_bytes", "reserved_bytes", "running", "pending", "level"})
	return objectSchema(map[string]any{
		"items": arraySchema(item), "row_limit": integerSchema(), "byte_limit": integerSchema(),
		"running_limit": integerSchema(), "next_request_reservation_bytes": integerSchema(), "history_days": integerSchema(),
	}, []string{"items", "row_limit", "byte_limit", "running_limit", "next_request_reservation_bytes", "history_days"})
}

package restcontract

func consoleCapacitySchemas() map[string]any {
	session := objectSchema(map[string]any{
		"id": integerSchema(), "runtime_id": integerSchema(), "generation": integerSchema(),
		"target_name": stringSchema(), "connector_kind": stringSchema(), "name": stringSchema(),
		"owner": stringSchema(), "state": stringSchema(), "clients": integerSchema(),
		"last_activity": dateTimeSchema(), "can_close": map[string]any{"type": "boolean"},
	}, []string{"id", "runtime_id", "generation", "target_name", "connector_kind", "name", "owner", "state", "clients", "last_activity", "can_close"})
	selection := objectSchema(map[string]any{"id": integerSchema(), "generation": integerSchema(), "last_activity": dateTimeSchema()}, []string{"id", "generation", "last_activity"})
	return map[string]any{
		"ConsoleCleanupPolicy":   objectSchema(map[string]any{"enabled": map[string]any{"type": "boolean"}, "idle_minutes": map[string]any{"type": "integer", "enum": []int{10, 20, 30, 45, 60, 120, 180, 360}}}, []string{"enabled", "idle_minutes"}),
		"ConsoleCapacitySession": session,
		"ConsoleCapacityReport": objectSchema(map[string]any{
			"limit": integerSchema(), "used": integerSchema(), "available": integerSchema(), "revision": integerSchema(), "observed_at": dateTimeSchema(), "items": arraySchema(refSchema("ConsoleCapacitySession")),
		}, []string{"limit", "used", "available", "revision", "observed_at", "items"}),
		"ConsoleCapacityCloseRequest": objectSchema(map[string]any{"idle_minutes": map[string]any{"type": "integer", "enum": []int{0, 10, 20, 30, 45, 60, 120, 180, 360}}, "sessions": arraySchema(selection)}, []string{"idle_minutes", "sessions"}),
		"ConsoleCapacityCloseResult":  objectSchema(map[string]any{"closed": arraySchema(integerSchema()), "skipped": arraySchema(integerSchema()), "uncertain": arraySchema(integerSchema())}, []string{"closed", "skipped", "uncertain"}),
	}
}

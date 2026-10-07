package actioncapacity_test

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
)

func BenchmarkEncryptedCapacityMeasurement(b *testing.B) {
	database, tokenID, _, ids := capacityFixture(b)
	// About 128 MiB of retained output in a native encrypted database.
	_, err := database.ExecContext(b.Context(), `WITH RECURSIVE sequence(n) AS (
		SELECT 1 UNION ALL SELECT n+1 FROM sequence WHERE n < 32
	) INSERT INTO connector_action_requests (
		token_id, target_id, profile_id, connector_kind, action_name, status,
		output_json, created_at
	) SELECT token_id, target_id, profile_id, connector_kind, action_name, status,
		printf('%.*c', 4*1024*1024, 'x'), created_at
	FROM sequence CROSS JOIN connector_action_requests WHERE id = ?`, ids[connectors.ResultCompleted])
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := actioncapacity.Measure(b.Context(), database, tokenID, 0); err != nil {
			b.Fatal(err)
		}
	}
}

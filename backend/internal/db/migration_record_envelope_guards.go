package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

const recordEnvelopeMarkerKey = "encrypted_record_envelope_version"

var recordEnvelopeWriteGuardMigration = migration{
	version:     21,
	description: "enforce record-bound envelope writes",
	preflight:   validateStoredRecordEnvelopeShapes,
	statements:  recordEnvelopeGuardStatements(),
}

func validateStoredRecordEnvelopeShapes(tx *sql.Tx) error {
	var marker string
	err := tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, recordEnvelopeMarkerKey).Scan(&marker)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read encrypted record marker: %w", err)
	}
	if marker != recordcrypto.EnvelopeVersion {
		return fmt.Errorf("unsupported encrypted record migration version %q", marker)
	}
	for _, recordType := range recordcrypto.PersistentRecordTypes() {
		query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s <> '' AND %s`,
			recordType.Table,
			recordType.Column,
			invalidRecordEnvelopeSQL(recordType.Column),
		)
		var invalid int
		if err := tx.QueryRow(query).Scan(&invalid); err != nil {
			return fmt.Errorf("validate %s envelopes: %w", recordType.Domain, err)
		}
		if invalid > 0 {
			return fmt.Errorf("%s contains %d invalid record envelope(s)", recordType.Domain, invalid)
		}
	}
	return nil
}

func recordEnvelopeGuardStatements() []string {
	statements := make([]string, 0, len(recordcrypto.PersistentRecordTypes())*2)
	for _, recordType := range recordcrypto.PersistentRecordTypes() {
		name := strings.ReplaceAll(recordType.Domain, "-", "_")
		condition := fmt.Sprintf(`NEW.%s <> ''
			AND EXISTS (SELECT 1 FROM settings WHERE key = '%s' AND value = '%s')
			AND %s`, recordType.Column, recordEnvelopeMarkerKey, recordcrypto.EnvelopeVersion, invalidRecordEnvelopeSQL("NEW."+recordType.Column))
		for _, operation := range []string{"INSERT", "UPDATE OF " + recordType.Column} {
			suffix := strings.ToLower(strings.Fields(operation)[0])
			statements = append(statements, fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS guard_%s_envelope_%s
				BEFORE %s ON %s
				WHEN %s
				BEGIN
					SELECT RAISE(ABORT, 'record-bound encrypted envelope is required');
				END;`, name, suffix, operation, recordType.Table, condition))
		}
	}
	return statements
}

func invalidRecordEnvelopeSQL(expression string) string {
	return fmt.Sprintf(`CASE WHEN json_valid(%[1]s) THEN
		COALESCE(json_extract(%[1]s, '$.version') <> 1, 1)
		OR COALESCE(json_extract(%[1]s, '$.algorithm') <> 'AES-256-GCM', 1)
		OR COALESCE(json_type(%[1]s, '$.nonce') <> 'text', 1)
		OR COALESCE(json_type(%[1]s, '$.ciphertext') <> 'text', 1)
	ELSE 1 END`, expression)
}

package timestampmigration

import (
	"database/sql"
	"testing"

	_ "github.com/SE-I-T-Digital/go-sqlcipher"
)

func TestNormalizeCanonicalizesEveryOwnedTimestampColumn(t *testing.T) {
	database := openFixture(t)
	defer database.Close()

	for _, spec := range chronologicalColumns {
		columns := "id INTEGER PRIMARY KEY"
		values := "1"
		for _, column := range spec.columns {
			columns += ", " + column + " TEXT"
			values += ", '2026-01-02T03:04:05.1Z'"
		}
		if _, err := database.Exec("CREATE TABLE " + spec.table + " (" + columns + ")"); err != nil {
			t.Fatalf("create %s: %v", spec.table, err)
		}
		if _, err := database.Exec("INSERT INTO " + spec.table + " VALUES (" + values + ")"); err != nil {
			t.Fatalf("insert %s: %v", spec.table, err)
		}
	}

	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := Normalize(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	for _, spec := range chronologicalColumns {
		for _, column := range spec.columns {
			var value string
			if err := database.QueryRow("SELECT " + column + " FROM " + spec.table + " WHERE id = 1").Scan(&value); err != nil {
				t.Fatalf("read %s.%s: %v", spec.table, column, err)
			}
			if value != "2026-01-02T03:04:05.100000000Z" {
				t.Fatalf("%s.%s = %q", spec.table, column, value)
			}
		}
	}
}

func TestNormalizeProcessesMoreThanOneBatchWithoutLosingPrecision(t *testing.T) {
	database := openFixture(t)
	defer database.Close()
	createEmptyOwnedTables(t, database)
	for id := 1; id <= migrationBatchSize+1; id++ {
		value := "2026-01-02T03:04:05.100000001Z"
		if id == migrationBatchSize+1 {
			value = "2026-01-02T03:04:05.100999999Z"
		}
		if _, err := database.Exec(`INSERT INTO history_entries (id, created_at) VALUES (?, ?)`, id, value); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := Normalize(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var first, last string
	if err := database.QueryRow(`SELECT created_at FROM history_entries WHERE id = 1`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT created_at FROM history_entries WHERE id = ?`, migrationBatchSize+1).Scan(&last); err != nil {
		t.Fatal(err)
	}
	if first != "2026-01-02T03:04:05.100000001Z" || last != "2026-01-02T03:04:05.100999999Z" || first >= last {
		t.Fatalf("normalized timestamps = %q, %q", first, last)
	}
}

func TestNormalizeRejectsMalformedTimestamp(t *testing.T) {
	database := openFixture(t)
	defer database.Close()
	createEmptyOwnedTables(t, database)
	if _, err := database.Exec(`INSERT INTO history_entries (id, created_at, started_at) VALUES (1, 'invalid', NULL)`); err != nil {
		t.Fatal(err)
	}

	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := Normalize(tx); err == nil {
		t.Fatal("expected malformed timestamp to fail normalization")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestParseStoredAcceptsSQLiteAndRFC3339Representations(t *testing.T) {
	for _, value := range []string{"2026-01-02T03:04:05.123456789Z", "2026-01-02 03:04:05.123456789"} {
		parsed, err := parseStored(value)
		if err != nil {
			t.Fatalf("parse %q: %v", value, err)
		}
		if parsed.Year() != 2026 || parsed.Nanosecond() != 123456789 {
			t.Fatalf("parsed %q as %v", value, parsed)
		}
	}
}

func openFixture(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	return database
}

func createEmptyOwnedTables(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, spec := range chronologicalColumns {
		columns := "id INTEGER PRIMARY KEY"
		for _, column := range spec.columns {
			columns += ", " + column + " TEXT"
		}
		if _, err := database.Exec("CREATE TABLE " + spec.table + " (" + columns + ")"); err != nil {
			t.Fatalf("create %s: %v", spec.table, err)
		}
	}
}

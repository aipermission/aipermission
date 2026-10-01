package projectvault

import (
	"database/sql"
	"reflect"
	"testing"
)

func populateQuotaRelations(t *testing.T, store *Store, item Item, shared int64) Item {
	t.Helper()
	input := quotaMetadataInput(item, item.OwnerProjectID)
	input.SharedProjectIDs = []int64{shared}
	input.Tags = []string{"original-tag"}
	input.UsageNotes = []UsageNote{{Location: "original-location", Notes: "original-notes"}}
	updated, err := store.UpdateMetadata(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func quotaRelationSnapshot(t *testing.T, database *sql.DB, itemID int64) [][5]string {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), `
		SELECT 'assignment', CAST(project_id AS TEXT), CAST(assignment_revision AS TEXT), created_at, updated_at
		FROM vault_item_projects WHERE vault_item_id = ?
		UNION ALL
		SELECT 'tag', tag, '', created_at, '' FROM vault_item_tags WHERE vault_item_id = ?
		ORDER BY 1, 2`, itemID, itemID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result [][5]string
	for rows.Next() {
		var row [5]string
		if err := rows.Scan(&row[0], &row[1], &row[2], &row[3], &row[4]); err != nil {
			t.Fatal(err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertQuotaRelations(t *testing.T, database *sql.DB, itemID int64, expected [][5]string) {
	t.Helper()
	if got := quotaRelationSnapshot(t, database, itemID); !reflect.DeepEqual(got, expected) {
		t.Fatalf("stored relation revisions/timestamps changed: %#v, want %#v", got, expected)
	}
}

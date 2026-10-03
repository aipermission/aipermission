package projects

import (
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestNewProjectNeverWidensExistingTokenVisibility(t *testing.T) {
	database := openProjectTestDB(t)
	store := NewStore(database)
	ctx := t.Context()
	first, err := store.Create(ctx, "Existing Project")
	if err != nil {
		t.Fatal(err)
	}
	restricted, err := tokens.NewStore(database).Create(ctx, tokens.CreateRequest{Name: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	unrestricted, err := tokens.NewStore(database).Create(ctx, tokens.CreateRequest{Name: "all-current-projects"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceTokenScopes(ctx, restricted.ID, []int64{first.ID}); err != nil {
		t.Fatal(err)
	}
	before := map[int64][]TokenScope{}
	for _, id := range []int64{restricted.ID, unrestricted.ID} {
		before[id], err = store.ListTokenScopes(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
	}
	newProject, err := store.Create(ctx, "Future Project")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{restricted.ID, unrestricted.ID} {
		allowed, err := store.TokenCanAccessProject(ctx, id, newProject.ID)
		if err != nil || allowed {
			t.Fatalf("new project silently widened token %d: %t %v", id, allowed, err)
		}
		for _, previous := range before[id] {
			if current := scopeForProject(t, store, id, previous.ProjectID); !reflect.DeepEqual(current, previous) {
				t.Fatalf("existing scope changed: %#v -> %#v", previous, current)
			}
		}
	}
	if _, err := store.ReplaceTokenScopes(ctx, restricted.ID, []int64{first.ID, newProject.ID}); err != nil {
		t.Fatal(err)
	}
	allowed, err := store.TokenCanAccessProject(ctx, restricted.ID, newProject.ID)
	if err != nil || !allowed {
		t.Fatalf("explicit project opt-in = %t %v", allowed, err)
	}
	createdAfter, err := tokens.NewStore(database).Create(ctx, tokens.CreateRequest{Name: "new-token-current-projects"})
	if err != nil {
		t.Fatal(err)
	}
	allowed, err = store.TokenCanAccessProject(ctx, createdAfter.ID, newProject.ID)
	if err != nil || !allowed {
		t.Fatalf("new token policy changed = %t %v", allowed, err)
	}
}

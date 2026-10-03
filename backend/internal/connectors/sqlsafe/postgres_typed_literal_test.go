package sqlsafe

import "testing"

func TestPostgresTypedLiteralIdentifierForms(t *testing.T) {
	for _, query := range []string{
		`SELECT public.dömain 'x'`,
		`SELECT püb.domain 'x'`,
		"SELECT public.ty\u0308pe 'x'",
		`SELECT public.🗝 'x'`,
		`SELECT U&"public".domain 'x'`,
		`SELECT public.U&"d\00F6main" 'x'`,
		`SELECT public.U&"d!00F6main" UESCAPE '!' 'x'`,
		`SELECT U&"p!00FCb" UESCAPE '!'.domain E'x'`,
		`SELECT public.dömain U&'x'`,
		`SELECT public.dömain /* outer /* inner */ end */ $tag$x$tag$`,
	} {
		t.Run(query, func(t *testing.T) {
			if err := ValidatePostgreSQLResolutionSyntax(query); err == nil {
				t.Fatal("schema-qualified typed literal admitted")
			}
		})
	}
}

func TestPostgresTypedLiteralChecksPreserveOrdinaryUnicodeQueries(t *testing.T) {
	for _, query := range []string{
		`SELECT dömain FROM püb.records`,
		`SELECT U&"d\00F6main" FROM public.records`,
		`SELECT public.U&"d!00F6main" UESCAPE '!' FROM public.records`,
		`SELECT DATE '2026-10-03', U&'d\00F6main'`,
		`SELECT 'public.dömain ''x'''`,
		`SELECT 1 /* public.dömain 'x' */`,
	} {
		if err := ValidatePostgreSQLResolutionSyntax(query); err != nil {
			t.Fatalf("ordinary query rejected: %s: %v", query, err)
		}
	}
}

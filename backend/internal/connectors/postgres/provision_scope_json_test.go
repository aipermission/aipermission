package postgresconnector

import (
	"strings"
	"testing"
)

func TestProvisionScopeRejectsLossyJSONUnicode(t *testing.T) {
	for _, escape := range []string{`\ud800`, `\udfff`, `\ud800x`, `\ud800\u0041`, `\ud800\ud800`, string([]byte{0xff})} {
		for _, field := range []string{"schema", "table", "column"} {
			text := `{"schemas":[{"schema":"public","tables":[{"table":"users","columns":["id"]}]}]}`
			switch field {
			case "schema":
				text = strings.Replace(text, `"public"`, `"`+escape+`"`, 1)
			case "table":
				text = strings.Replace(text, `"users"`, `"`+escape+`"`, 1)
			case "column":
				text = strings.Replace(text, `"id"`, `"`+escape+`"`, 1)
			}
			if _, err := provisionScopeInput(map[string]any{"scope": text}); err == nil {
				t.Fatalf("lossy %s identity accepted: %q", field, text)
			}
		}
	}
}

func TestProvisionScopeJSONPreservesValidUnicodeAndEscapedBackslashes(t *testing.T) {
	for _, sample := range []struct{ encoded, decoded string }{
		{`\ud83d\ude80`, "\U0001f680"}, {`\ufffd`, "\ufffd"}, {"\ufffd", "\ufffd"}, {`\\ud800`, `\ud800`},
	} {
		text := `{"schemas":[{"schema":"` + sample.encoded + `","tables":[{"table":"` + sample.encoded + `","columns":["` + sample.encoded + `"]}]}]}`
		scope, err := provisionScopeInput(map[string]any{"scope": text})
		if err != nil {
			t.Fatal(err)
		}
		schema := scope.Schemas[0]
		if schema.Schema != sample.decoded || schema.Tables[0].Table != sample.decoded || schema.Tables[0].Columns[0] != sample.decoded {
			t.Fatalf("valid Unicode changed: %#v", scope)
		}
	}
}

func TestProvisionScopeJSONRejectsInvalidSyntax(t *testing.T) {
	for _, text := range []string{`{"schema":"\`, `{"schema":"\u00"}`, `{"schema":"\uzzzz"}`, `[]`, `"text"`} {
		var decoded map[string]any
		if err := decodeProvisionScopeJSON(text, &decoded); err == nil {
			t.Fatalf("invalid scope JSON accepted: %q", text)
		}
	}
}

func TestProvisionScopeLiteralIsIndependentOfStringSettings(t *testing.T) {
	for _, sample := range []struct{ value, literal string }{
		{"public", `E'public'`}, {`x\' OR owned.sequence_name IS NOT NULL --`, `E'x\\'' OR owned.sequence_name IS NOT NULL --'`},
		{`\`, `E'\\'`}, {"'", `E''''`},
	} {
		if got := quoteLiteral(sample.value); got != sample.literal {
			t.Fatalf("unsafe SQL literal: %q want %q", got, sample.literal)
		}
	}
}

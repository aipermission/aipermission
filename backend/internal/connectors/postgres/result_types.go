package postgresconnector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgtype"
)

// Retain nested numbers before the shared public JSON projection decides
// whether a value can safely be represented as a JavaScript number.
func configurePostgresTypes(types *pgtype.Map) {
	for _, item := range []struct {
		name          string
		oid, arrayOID uint32
		codec         pgtype.Codec
	}{
		{"json", pgtype.JSONOID, pgtype.JSONArrayOID, &pgtype.JSONCodec{Marshal: json.Marshal, Unmarshal: decodePostgresJSON}},
		{"jsonb", pgtype.JSONBOID, pgtype.JSONBArrayOID, &pgtype.JSONBCodec{Marshal: json.Marshal, Unmarshal: decodePostgresJSON}},
	} {
		element := &pgtype.Type{Name: item.name, OID: item.oid, Codec: item.codec}
		types.RegisterType(element)
		types.RegisterType(&pgtype.Type{Name: "_" + item.name, OID: item.arrayOID, Codec: &pgtype.ArrayCodec{ElementType: element}})
	}
}

func decodePostgresJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("postgres JSON contains trailing data")
	}
	return nil
}

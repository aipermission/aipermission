package postgresconnector

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/sqlresult"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPostgresBinaryValuesRoundTrip(t *testing.T) {
	allBytes := make([]byte, 256)
	for index := range allBytes {
		allBytes[index] = byte(index)
	}
	for _, input := range [][]byte{
		{}, {0xff, 0xfe, 0x00, 0x41}, []byte("\ufffd"), []byte("hello"),
		[]byte(`\xdeadbeef`), allBytes, bytes.Repeat([]byte{0xff}, (maxCellBytes-2)/2),
	} {
		text, ok := normalizePostgresValue(input).(string)
		if !ok || !strings.HasPrefix(text, `\x`) {
			t.Fatalf("binary output lacks hex identity: %T", normalizePostgresValue(input))
		}
		encoded, err := json.Marshal(text)
		if err != nil {
			t.Fatal(err)
		}
		var transported string
		if err := json.Unmarshal(encoded, &transported); err != nil {
			t.Fatal(err)
		}
		decoded, err := hex.DecodeString(strings.TrimPrefix(transported, `\x`))
		if err != nil || !bytes.Equal(decoded, input) {
			t.Fatalf("binary identity changed: err=%v bytes=%d", err, len(input))
		}
	}
	if normalizePostgresValue([]byte{0xff}) == normalizePostgresValue([]byte("\ufffd")) {
		t.Fatal("invalid UTF-8 and a real replacement character share an identity")
	}
}

func TestPostgresValuesPreserveNullTextAndTime(t *testing.T) {
	stamp := time.Date(2026, 10, 1, 12, 30, 45, 123, time.FixedZone("fixture", 3600))
	for _, item := range []struct{ input, want any }{
		{nil, nil}, {[]byte(nil), nil}, {[]byte{}, `\x`},
		{"\ufffd", "\ufffd"}, {`\xff`, `\xff`}, {42, 42}, {true, true},
		{map[string]any{"text": "unchanged"}, map[string]any{"text": "unchanged"}},
		{stamp, "2026-10-01T11:30:45.000000123Z"},
	} {
		if got := normalizePostgresValue(item.input); !reflect.DeepEqual(got, item.want) {
			t.Fatalf("normalize %T = %#v, want %#v", item.input, got, item.want)
		}
	}
}

func TestPostgresBinaryCellLimitsRemainExplicit(t *testing.T) {
	for _, length := range []int{0, (maxCellBytes - 2) / 2, maxCellBytes / 2, maxCellBytes/2 + 1, maxCellBytes * 8} {
		input := bytes.Repeat([]byte{0xff}, length)
		builder := sqlresult.NewBuilder([]string{"binary"}, 1, maxOutputBytes, maxCellBytes, truncatedSuffix)
		if !builder.Add([]any{input}, normalizePostgresValue) {
			t.Fatal("bounded binary row was unexpectedly rejected")
		}
		result := builder.Result(nil)
		text := result.Rows[0]["binary"].(string)
		wantTruncated := length > (maxCellBytes-2)/2
		if result.Truncated != wantTruncated || len(text) > maxCellBytes {
			t.Fatalf("length=%d truncated=%v output=%d", length, result.Truncated, len(text))
		}
		if wantTruncated && !strings.HasSuffix(text, truncatedSuffix) {
			t.Fatal("truncated binary cell lacks its explicit suffix")
		}
		if !wantTruncated {
			decoded, err := hex.DecodeString(text[2:])
			if err != nil || !bytes.Equal(decoded, input) {
				t.Fatal("an untruncated bounded binary cell changed identity")
			}
		}
		encoded, err := json.Marshal(result.ToMap(maxOutputBytes, nil))
		if err != nil || len(encoded) > maxOutputBytes {
			t.Fatalf("serialized binary result exceeded its output bound: %v", err)
		}
		if len(normalizePostgresValue(input).(string)) > maxCellBytes+2 {
			t.Fatal("normalization allocated hex for the entire oversized binary cell")
		}
	}
}

func TestPostgresByteaCodecValuesRemainLossless(t *testing.T) {
	codec := pgtype.ByteaCodec{}
	for _, item := range []struct {
		format int16
		wire   []byte
		want   any
	}{
		{pgtype.BinaryFormatCode, []byte{0xff, 0x00, 0x41}, `\xff0041`},
		{pgtype.TextFormatCode, []byte(`\xff0041`), `\xff0041`},
		{pgtype.BinaryFormatCode, []byte{}, `\x`},
		{pgtype.TextFormatCode, []byte(`\x`), `\x`},
		{pgtype.BinaryFormatCode, nil, nil},
		{pgtype.TextFormatCode, nil, nil},
	} {
		value, err := codec.DecodeValue(pgtype.NewMap(), pgtype.ByteaOID, item.format, item.wire)
		if err != nil {
			t.Fatal(err)
		}
		if got := normalizePostgresValue(value); !reflect.DeepEqual(got, item.want) {
			t.Fatalf("decoded wire normalized to %#v, want %#v", got, item.want)
		}
	}
}

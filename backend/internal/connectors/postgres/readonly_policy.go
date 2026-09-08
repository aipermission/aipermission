package postgresconnector

import (
	"context"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors/sqlsafe"
	"github.com/jackc/pgx/v5"
)

func validateReadonlySQL(sql string) error {
	if err := sqlsafe.ValidateReadOnlyDialect(
		sql,
		ActionQueryReadonly,
		maxSQLBytes,
		[]string{"select", "with", "show", "explain"},
		"SELECT, WITH, SHOW, or EXPLAIN",
		disallowedReadonlyTerms,
		sqlsafe.DialectPostgreSQL,
	); err != nil {
		return err
	}
	if err := sqlsafe.ValidatePostgreSQLResolutionSyntax(sql); err != nil {
		return fmt.Errorf("%s only accepts directly typed read expressions: %w", ActionQueryReadonly, err)
	}
	calls, err := sqlsafe.PostgreSQLFunctionCalls(sql)
	if err != nil {
		return fmt.Errorf("%s sql is malformed: %w", ActionQueryReadonly, err)
	}
	for _, call := range calls {
		if !postgresReadFunctionAllowed(call) {
			name := call.Name
			if call.Schema != "" {
				name = call.Schema + "." + call.Name
			}
			return fmt.Errorf("%s only accepts approved read-only functions; %s is not allowed", ActionQueryReadonly, name)
		}
	}
	return nil
}

func protectReadonlyFunctionResolution(ctx context.Context, tx pgx.Tx) error {
	var exposesCustomImplicitCast bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_cast AS cast_rule
			JOIN pg_catalog.pg_proc AS cast_function ON cast_function.oid = cast_rule.castfunc
			WHERE cast_rule.castcontext = 'i'
				AND cast_rule.castfunc >= 16384
				AND pg_catalog.has_function_privilege(current_user, cast_rule.castfunc, 'EXECUTE')
				AND pg_catalog.has_type_privilege(current_user, cast_rule.castsource, 'USAGE')
				AND pg_catalog.has_type_privilege(current_user, cast_rule.casttarget, 'USAGE')
		)`).Scan(&exposesCustomImplicitCast); err != nil {
		return fmt.Errorf("inspect postgres implicit cast policy: %w", err)
	}
	if exposesCustomImplicitCast {
		return fmt.Errorf("postgres read-only execution is unavailable because this profile can execute a custom implicit cast")
	}
	_, err := tx.Exec(ctx, `SELECT set_config('search_path', 'pg_catalog', true)`)
	if err != nil {
		return fmt.Errorf("protect postgres read-only function resolution: %w", err)
	}
	return nil
}

var postgresReadFunctions = map[string]struct{}{
	"abs": {}, "age": {}, "array": {}, "array_agg": {}, "array_length": {}, "array_position": {},
	"avg": {}, "bit_and": {}, "bit_or": {}, "bit_xor": {}, "bool_and": {}, "bool_or": {},
	"btrim": {}, "cardinality": {}, "ceil": {}, "ceiling": {}, "char_length": {},
	"coalesce": {}, "concat": {}, "concat_ws": {}, "count": {}, "cume_dist": {},
	"current_database": {}, "current_schema": {}, "current_schemas": {},
	"date_part": {}, "date_trunc": {}, "decode": {}, "dense_rank": {}, "encode": {},
	"every": {}, "extract": {}, "first_value": {}, "floor": {}, "format": {},
	"generate_series": {}, "greatest": {}, "json_agg": {}, "json_array_length": {},
	"json_build_array": {}, "json_build_object": {}, "json_each": {}, "json_each_text": {},
	"json_extract_path": {}, "json_extract_path_text": {}, "json_object_agg": {}, "json_typeof": {},
	"jsonb_agg": {}, "jsonb_array_length": {}, "jsonb_build_array": {}, "jsonb_build_object": {},
	"jsonb_each": {}, "jsonb_each_text": {}, "jsonb_extract_path": {}, "jsonb_extract_path_text": {},
	"jsonb_object_agg": {}, "jsonb_typeof": {}, "lag": {}, "last_value": {}, "lead": {},
	"least": {}, "left": {}, "length": {}, "lower": {}, "lpad": {}, "ltrim": {},
	"max": {}, "min": {}, "nth_value": {}, "ntile": {}, "nullif": {}, "octet_length": {},
	"now": {}, "percent_rank": {}, "pg_column_size": {}, "pg_size_pretty": {}, "pg_table_size": {},
	"pg_total_relation_size": {}, "pg_typeof": {}, "quote_ident": {}, "rank": {},
	"regexp_match": {}, "regexp_matches": {}, "regexp_replace": {}, "repeat": {}, "replace": {},
	"reverse": {}, "right": {}, "round": {}, "row": {}, "row_number": {}, "rpad": {}, "rtrim": {},
	"split_part": {}, "string_agg": {}, "string_to_array": {}, "strpos": {}, "substr": {},
	"substring": {}, "sum": {}, "timezone": {}, "to_char": {}, "to_date": {}, "to_json": {}, "to_jsonb": {},
	"to_timestamp": {},
	"translate":    {}, "trim": {}, "trunc": {}, "unnest": {}, "upper": {}, "width_bucket": {},
}

func postgresReadFunctionAllowed(call sqlsafe.FunctionCall) bool {
	if call.Schema != "" && !strings.EqualFold(call.Schema, "pg_catalog") {
		return false
	}
	if call.Name == "quoted_identifier" {
		return false
	}
	_, allowed := postgresReadFunctions[strings.ToLower(call.Name)]
	return allowed
}

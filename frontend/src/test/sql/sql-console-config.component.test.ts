import { describe, expect, it } from "vitest";
import {
  filteredTableBrowserRows,
  mergeMetadataRows,
  normalizeSQLConsoleConfig,
} from "../../connectors/templates/_shared/sql-console-config";

describe("SQL console config", () => {
  it("keeps dotted and case-distinct metadata identities", () => {
    const rows = filteredTableBrowserRows(
      [
        { schema: "public", table: "Users", column: "Admin.Secret", position: 1 },
        { schema: "public", table: "users", column: "public_name", position: 1 },
        { schema: "tenant.one", table: "events.live", column: "id", position: 1 },
      ],
      "public",
    );

    expect(rows.map(({ table, columns }) => ({ table, columns: columns.map((column) => column.name) }))).toEqual([
      { table: "users", columns: ["public_name"] },
      { table: "Users", columns: ["Admin.Secret"] },
    ]);
    expect(mergeMetadataRows(rows, [{ schema: "public", table: "Users", column: "admin.secret", position: 1 }])).toHaveLength(3);
  });

  it("normalizes defaults and preserves custom endpoint behavior", () => {
    const config = normalizeSQLConsoleConfig({
      label: "  Warehouse  ",
      defaultPort: 9000,
      defaultDatabase: "analytics",
      keywords: ["SELECT", "sample"],
    });

    expect(config.label).toBe("Warehouse");
    expect(config.identifierPolicy).toBe("lowercase-unquoted");
    expect(config.keywords).toContain("sample");
    expect(config.targetEndpoint({ config: { host: "db.internal" } })).toBe("db.internal:9000/analytics");
  });

  it("accepts only the exact identifier policy override", () => {
    expect(normalizeSQLConsoleConfig({ identifierPolicy: "exact" }).identifierPolicy).toBe("exact");
    expect(normalizeSQLConsoleConfig({ identifierPolicy: "unknown" }).identifierPolicy).toBe("lowercase-unquoted");
  });

  it("requires explicit execution opt-in without changing read or metadata actions", () => {
    expect(normalizeSQLConsoleConfig().executionAction).toBeUndefined();
    expect(normalizeSQLConsoleConfig({ executionAction: "  " }).executionAction).toBeUndefined();
    const config = normalizeSQLConsoleConfig({ executionAction: "execute_sql" });
    expect(config.executionAction).toBe("execute_sql");
    expect(config.queryAction).toBe("query_readonly");
    expect(config.metadataAction).toBe("query_readonly");
    expect(config.keywords).toEqual(
      expect.arrayContaining(["insert into", "update", "delete from", "create", "alter", "drop", "commit", "rollback", "savepoint"]),
    );
    expect(normalizeSQLConsoleConfig().keywords).not.toContain("update");
    expect(config.keywords).not.toContain("exec");
    expect(config.keywords).not.toContain("call");
  });
});

import assert from "node:assert/strict";
import test from "node:test";
import { buildProvisionScope, buildProvisionSQLPreview, groupMetadataRows, safeBackupFilename } from "./provisioning.js";

test("Postgres provisioning metadata preserves schema, table, and column order", () => {
  assert.deepEqual(
    groupMetadataRows([
      { table_schema: "public", table_name: "users", columns: '["id","email"]' },
      { table_schema: "public", table_name: "events", columns: ["id", "created_at"] },
    ]),
    [
      {
        name: "public",
        tables: [
          { name: "users", columns: ["id", "email"] },
          { name: "events", columns: ["id", "created_at"] },
        ],
      },
    ],
  );
});

test("Postgres provisioning drops incomplete nested scope selections", () => {
  assert.equal(buildProvisionScope({ all_schemas: false, schemas: { public: { selected: true, all_tables: false, tables: {} } } }), null);
  assert.deepEqual(
    buildProvisionScope({
      all_schemas: false,
      schemas: {
        public: {
          selected: true,
          all_tables: false,
          tables: {
            users: { selected: true, all_columns: false, columns: { id: true, email: false } },
            ignored: { selected: false, all_columns: true, columns: {} },
          },
        },
      },
    }),
    {
      all_schemas: false,
      schemas: [{ schema: "public", all_tables: false, tables: [{ table: "users", all_columns: false, columns: ["id"] }] }],
    },
  );
});

test("Postgres SQL previews remain bounded to validated identifiers and scopes", () => {
  const preview = buildProvisionSQLPreview({
    roleName: "invalid role; DROP DATABASE app",
    preset: "read_only",
    database: "app",
    scope: {
      all_schemas: false,
      schemas: [{ schema: "public", all_tables: false, tables: [{ table: "users", all_columns: false, columns: ["id"] }] }],
    },
  });
  assert.match(preview, /CREATE ROLE "role_name"/);
  assert.match(preview, /GRANT SELECT \("id"\) ON TABLE "public"\."users"/);
  assert.doesNotMatch(preview, /DROP DATABASE/);
  assert.equal(safeBackupFilename(" Main DB / Production "), "main-db-production");
});

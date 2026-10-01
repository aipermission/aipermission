import { describe, expect, it } from "vitest";
import { objectRecord } from "../../../../lib/api-types";
import { roleHistoryPage } from "./role-history-contract";
import { journalDecimal, journalHex, journalIdentifier, journalInteger } from "./role-history-identity";
import { roleHistoryPageFixture } from "../../../../test/postgres/role-history-fixtures.test";
import { roleLifecycleStatuses } from "./role-history-types";

describe("Postgres role history contract", () => {
  it("preserves exact large identities and names without trimming", () => {
    const fixture = roleHistoryPageFixture(1, ["9007199254740993", "9223372036854775807"]);
    const parsed = roleHistoryPage(fixture, 1);
    expect(parsed).toEqual(fixture);
    expect(parsed.entries[0].record.intent.anchor.cluster_id).toBe("18446744073709551615");
    expect(parsed).not.toBe(fixture);
    expect(parsed.entries[0].record.intent.anchor).not.toBe(fixture.entries[0].record.intent.anchor);
  });

  it("accepts empty results and exact page continuations", () => {
    expect(roleHistoryPage(roleHistoryPageFixture(1, []), 1).entries).toEqual([]);
    const fixture = roleHistoryPageFixture(
      1,
      Array.from({ length: 64 }, (_, i) => String(i + 1)),
    );
    fixture.has_more = true;
    fixture.next_after_resource_id = "64";
    expect(roleHistoryPage(fixture, 1)).toEqual(fixture);
    expect(roleHistoryPage(roleHistoryPageFixture(1, ["65"]), 1, "64").entries[0].resource_id).toBe("65");
  });

  it.each(roleLifecycleStatuses)("accepts valid %s evidence", (status) => {
    const fixture = roleHistoryPageFixture();
    fixture.entries[0].record.status = status;
    if (status === "provision_intent" || status === "rolled_back") fixture.entries[0].record.role_oid = 0;
    expect(roleHistoryPage(fixture, 1).entries[0].record.status).toBe(status);
  });

  it.each([
    ["target_id", 2],
    ["entries", null],
    ["entries", {}],
    ["entries", Array.from({ length: 65 }, () => roleHistoryPageFixture().entries[0])],
    ["has_more", "true"],
    ["has_more", true],
    ["next_after_resource_id", "1"],
    ["next_after_resource_id", null],
    ["entries.0.resource_id", 1],
    ["entries.0.resource_id", "0"],
    ["entries.0.resource_id", "01"],
    ["entries.0.resource_id", "9223372036854775808"],
    ["entries.0.record", null],
    ["entries.0.record.version", 2],
    ["entries.0.record.generation", "A".repeat(32)],
    ["entries.0.record.generation", "a".repeat(31)],
    ["entries.0.record.status", "attested"],
    ["entries.0.record.role_oid", 0],
    ["entries.0.record.role_oid", 10],
    ["entries.0.record.role_oid", 4294967296],
    ["entries.0.record.intent", null],
    ["entries.0.record.intent.role_name", " Main Admin "],
    ["entries.0.record.intent.role_name", ""],
    ["entries.0.record.intent.role_name", "x".repeat(64)],
    ["entries.0.record.intent.role_name", "name\0suffix"],
    ["entries.0.record.intent.operation_id", "a".repeat(31)],
    ["entries.0.record.intent.anchor", null],
    ["entries.0.record.intent.anchor.target_id", 2],
    ["entries.0.record.intent.anchor.admin_profile_id", 0],
    ["entries.0.record.intent.anchor.context_digest", "a".repeat(63)],
    ["entries.0.record.intent.anchor.target_digest", "c".repeat(63)],
    ["entries.0.record.intent.anchor.target_digest", undefined],
    ["entries.0.record.intent.anchor.cluster_id", "0"],
    ["entries.0.record.intent.anchor.cluster_id", Number("18446744073709551615")],
    ["entries.0.record.intent.anchor.cluster_id", "18446744073709551616"],
    ["entries.0.record.intent.anchor.database_oid", 0],
    ["entries.0.record.intent.anchor.database_name", ""],
    ["entries.0.record.intent.anchor.successor_oid", 0],
    ["entries.0.record.intent.anchor.successor_name", "\0"],
  ])("rejects malformed %s", (path, value) => {
    const fixture = roleHistoryPageFixture();
    replaceField(fixture, path as string, value);
    expect(() => roleHistoryPage(fixture, 1)).toThrow();
  });

  it.each([null, [], "{}", false])("rejects non-object pages", (value) => {
    expect(() => roleHistoryPage(value, 1)).toThrow();
  });

  it.each([0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1])("rejects unsafe target %s", (id) => {
    expect(() => roleHistoryPage(roleHistoryPageFixture(), id)).toThrow();
  });

  it.each(["", "01", "-1", "+1", "1e2", "9223372036854775808"])("rejects invalid requested cursor %s", (after) => {
    expect(() => roleHistoryPage(roleHistoryPageFixture(), 1, after)).toThrow();
  });

  it("rejects duplicate/unordered IDs and operations or a nonadvancing cursor", () => {
    for (const ids of [
      ["1", "1"],
      ["2", "1"],
    ]) {
      expect(() => roleHistoryPage(roleHistoryPageFixture(1, ids), 1)).toThrow();
    }
    const fixture = roleHistoryPageFixture(1, ["1", "2"]);
    fixture.entries[1].record.intent.operation_id = fixture.entries[0].record.intent.operation_id;
    expect(() => roleHistoryPage(fixture, 1)).toThrow();
    expect(() => roleHistoryPage(roleHistoryPageFixture(), 1, "1")).toThrow();
    const page = roleHistoryPageFixture(
      1,
      Array.from({ length: 64 }, (_, i) => String(i + 1)),
    );
    page.has_more = true;
    page.next_after_resource_id = "63";
    expect(() => roleHistoryPage(page, 1)).toThrow();
  });
});

describe("Postgres journal identity primitives", () => {
  it.each([null, "", "00", "01", "+1", "-1", "1.0", "1e2", "1_0", " 1", "1 ", "\u0661", "1".repeat(21)])(
    "rejects noncanonical decimal %s",
    (value) => expect(journalDecimal(value, 18446744073709551615n)).toBe(false),
  );
  it("checks decimal zero and bounds without rounding", () => {
    expect(journalDecimal("0", 1n)).toBe(false);
    expect(journalDecimal("0", 1n, true)).toBe(true);
    expect(journalDecimal("18446744073709551616", 18446744073709551615n)).toBe(false);
    expect(journalDecimal("18446744073709551615", 18446744073709551615n)).toBe(true);
  });
  it.each([null, "1", 0, -1, 1.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1])("rejects invalid integer %s", (value) => {
    expect(journalInteger(value)).toBe(false);
  });
  it("checks numeric bounds and canonical hex", () => {
    expect(journalInteger(0, 1, true)).toBe(true);
    expect(journalInteger(2, 1)).toBe(false);
    expect(journalHex(null, 32)).toBe(false);
    expect(journalHex("a".repeat(32), 32)).toBe(true);
    expect(journalHex("g".repeat(32), 32)).toBe(false);
  });
  it("checks identifier UTF-8 bytes instead of characters", () => {
    expect(journalIdentifier("  ")).toBe(true);
    expect(journalIdentifier("x".repeat(63))).toBe(true);
    expect(journalIdentifier("\u00e9".repeat(31))).toBe(true);
    expect(journalIdentifier("\u00e9".repeat(32))).toBe(false);
    expect(journalIdentifier("\ud800")).toBe(false);
    expect(journalIdentifier("\udc00")).toBe(false);
    expect(journalIdentifier("\ud83d\ude00")).toBe(true);
    expect(journalIdentifier("\ufeffname")).toBe(true);
  });
});

function replaceField(value: unknown, path: string, replacement: unknown) {
  const fields = path.split(".");
  let parent: unknown = value;
  for (const field of fields.slice(0, -1)) parent = Array.isArray(parent) ? parent[Number(field)] : objectRecord(parent)?.[field];
  const record = objectRecord(parent);
  if (!record) throw new Error(`Missing fixture path ${path}`);
  record[fields.at(-1)!] = replacement;
}

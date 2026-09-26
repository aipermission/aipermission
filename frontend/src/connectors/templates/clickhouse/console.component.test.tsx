import { expect, it } from "vitest";
import { clickHouseConsoleConfig } from "./console";

it("preserves exact ClickHouse identifiers in console metadata and prepared queries", () => {
  expect(clickHouseConsoleConfig.identifierPolicy).toBe("exact");
  expect(clickHouseConsoleConfig.describeInput({ schema: "Analytics", table: "Users" })).toEqual({
    database: "Analytics",
    table: "Users",
  });
  expect(clickHouseConsoleConfig.tableQuery({ schema: "Analytics", table: "Users" }, 25)).toBe(
    "SELECT *\nFROM `Analytics`.`Users`\nLIMIT 25;",
  );
});

it("escapes embedded backticks in schema and table identifiers", () => {
  expect(clickHouseConsoleConfig.tableQuery({ schema: "tenant`one", table: "events`live" }, 10)).toBe(
    "SELECT *\nFROM `tenant``one`.`events``live`\nLIMIT 10;",
  );
});

it("does not invent a database for empty metadata references", () => {
  expect(clickHouseConsoleConfig.describeInput({ schema: "", table: "events" })).toEqual({ database: "", table: "events" });
  expect(clickHouseConsoleConfig.tableQuery({ schema: "", table: "" }, 10)).toBe("SELECT *\nFROM ``.``\nLIMIT 10;");
});

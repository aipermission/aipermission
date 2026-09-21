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

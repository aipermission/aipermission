import { expect, it } from "vitest";
import {
  optionalConsoleBoolean,
  optionalConsoleNumber,
  optionalConsolePort,
  optionalConsoleText,
  optionalConsoleTextOrNumber,
  optionalConsoleTextList,
} from "./console-target-config";

it("accepts absent configuration and preserves valid primitive field values", () => {
  expect(optionalConsoleText(undefined, "Example", "host")).toBeUndefined();
  expect(optionalConsoleNumber(undefined, "Example", "database")).toBeUndefined();
  expect(optionalConsolePort(undefined, "Example")).toBeUndefined();
  expect(optionalConsoleBoolean(undefined, "Example", "enabled")).toBeUndefined();
  expect(optionalConsoleText("host.test", "Example", "host")).toBe("host.test");
  expect(optionalConsoleNumber(0, "Example", "database")).toBe(0);
  expect(optionalConsolePort("443", "Example")).toBe("443");
  expect(optionalConsolePort(443, "Example")).toBe(443);
  expect(optionalConsoleBoolean(false, "Example", "enabled")).toBe(false);
  expect(optionalConsoleBoolean(true, "Example", "enabled")).toBe(true);
  expect(optionalConsoleTextOrNumber("0", "Example", "database")).toBe("0");
  expect(optionalConsoleTextOrNumber(0, "Example", "database")).toBe(0);
  expect(optionalConsoleTextOrNumber(undefined, "Example", "database")).toBeUndefined();
  expect(optionalConsoleTextList(undefined, "Example", "folders")).toBeUndefined();
  expect(optionalConsoleTextList("INBOX", "Example", "folders")).toBe("INBOX");
  const folders = ["INBOX", "Sent"];
  expect(optionalConsoleTextList(folders, "Example", "folders")).toEqual(folders);
  expect(optionalConsoleTextList(folders, "Example", "folders")).not.toBe(folders);
});

it("rejects malformed field types without coercing gateway values", () => {
  for (const value of [null, [], {}, 42, true]) {
    expect(() => optionalConsoleText(value, "Example", "host")).toThrow("Invalid Example console target host.");
  }
  for (const value of [null, [], {}, "443", true, Number.NaN, Number.POSITIVE_INFINITY]) {
    expect(() => optionalConsoleNumber(value, "Example", "port")).toThrow("Invalid Example console target port.");
  }
  for (const value of [null, [], {}, true, Number.NaN]) {
    expect(() => optionalConsolePort(value, "Example")).toThrow("Invalid Example console target port.");
    expect(() => optionalConsoleTextOrNumber(value, "Example", "database")).toThrow("Invalid Example console target database.");
  }
  for (const value of [null, [], {}, "false", 0]) {
    expect(() => optionalConsoleBoolean(value, "Example", "enabled")).toThrow("Invalid Example console target enabled.");
  }
  for (const value of [null, {}, ["INBOX", 1], 0, false]) {
    expect(() => optionalConsoleTextList(value, "Example", "folders")).toThrow("Invalid Example console target folders.");
  }
});

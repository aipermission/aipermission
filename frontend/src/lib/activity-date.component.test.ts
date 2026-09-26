import { expect, it } from "vitest";
import { formatDateTime, formatShortTime } from "./activity-date";

it("preserves empty and invalid gateway timestamps without throwing", () => {
  expect(formatShortTime()).toBe("-");
  expect(formatDateTime()).toBe("");
  expect(formatShortTime("invalid")).toBe("invalid");
  expect(formatDateTime("invalid")).toBe("invalid");
});
it("uses the user's locale for both activity views", () => {
  const timestamp = "2026-09-26T12:30:00Z";
  const date = new Date(timestamp);
  expect(formatShortTime(timestamp)).toBe(date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }));
  expect(formatDateTime(timestamp)).toBe(date.toLocaleString());
});

import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { formatLocalTimestamp, formatRelativeAge, formatRelativeDeadline, toLocalDateTime, toRFC3339 } from "./date-time";

const now = Date.parse("2026-09-27T12:00:00Z");

beforeEach(() => vi.spyOn(Date, "now").mockReturnValue(now));
afterEach(() => vi.restoreAllMocks());

it.each([null, undefined, "", "invalid-date"])("does not display or submit an invalid timestamp %j", (value) => {
  expect(formatRelativeAge(value)).toBe("");
  expect(formatRelativeDeadline(value)).toBe("");
  expect(formatLocalTimestamp(value)).toBe("");
  expect(toRFC3339(value)).toBe("");
  expect(toLocalDateTime(value)).toBe("");
});

it.each([
  { seconds: -60, expected: "just now" },
  { seconds: 0, expected: "just now" },
  { seconds: 59, expected: "just now" },
  { seconds: 60, expected: "1 min ago" },
  { seconds: 3599, expected: "59 min ago" },
  { seconds: 3600, expected: "1h ago" },
  { seconds: 86399, expected: "23h ago" },
  { seconds: 86400, expected: "1d ago" },
  { seconds: 172800, expected: "2d ago" },
])("displays age at the $seconds second boundary as $expected", ({ seconds, expected }) => {
  expect(formatRelativeAge(new Date(now - seconds * 1000).toISOString())).toBe(expected);
});

it.each([
  { milliseconds: -1, expected: "expired" },
  { milliseconds: 0, expected: "expired" },
  { milliseconds: 1, expected: "expires in 1s" },
  { milliseconds: 59000, expected: "expires in 59s" },
  { milliseconds: 60000, expected: "expires in 1 min" },
  { milliseconds: 61000, expected: "expires in 2 min" },
  { milliseconds: 3540000, expected: "expires in 59 min" },
  { milliseconds: 3600000, expected: "expires in 1h" },
  { milliseconds: 3600001, expected: "expires in 2h" },
])("rounds a $milliseconds millisecond deadline to $expected", ({ milliseconds, expected }) => {
  expect(formatRelativeDeadline(new Date(now + milliseconds).toISOString())).toBe(expected);
});

it("preserves absolute instants while preparing local editor values", () => {
  const input = "2026-09-27T23:30:00+03:00";
  const date = new Date(input);
  expect(toRFC3339(input)).toBe("2026-09-27T20:30:00.000Z");
  expect(formatLocalTimestamp(input)).toBe(date.toLocaleString());
  const pad = (number: number) => String(number).padStart(2, "0");
  expect(toLocalDateTime(input)).toBe(
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`,
  );
});

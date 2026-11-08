import { describe, expect, it } from "vitest";
import { activityAge, capacityReport, closeResult, idleLabel, idleOptions, sessionGroup } from "./model";
import { report, session } from "../../test/console-capacity-fixtures";
describe("console capacity contract", () => {
  it("validates bounded counts, identity and activity", () => {
    expect(capacityReport(report())).toEqual(report());
    for (const value of [
      null,
      { ...report(), used: 33 },
      { ...report(), items: [session(), session(), session()] },
      { ...report(), revision: -1 },
      { ...report(), items: [session(), session(2), { ...session(3), last_activity: "bad" }] },
    ])
      expect(() => capacityReport(value)).toThrow();
  });
  it("groups by selected threshold, never by age alone for running work", () => {
    const data = report();
    expect(sessionGroup(data.items[0], data, 30)).toBe("idle");
    expect(sessionGroup(data.items[1], data, 30)).toBe("connected");
    expect(sessionGroup(data.items[2], data, 30)).toBe("running");
    expect(sessionGroup({ ...session(), state: "untracked" }, data, 30)).toBe("other");
    expect(sessionGroup(data.items[0], data, 120)).toBe("connected");
    expect(activityAge(data.items[1], data)).toBe("2m ago");
    expect(activityAge(data.items[0], data)).toBe("1h 0m ago");
    expect(activityAge(session(1, 0), data)).toBe("Just now");
    expect(idleOptions.map(idleLabel)).toEqual(["10m", "20m", "30m", "45m", "1h", "2h", "3h", "6h"]);
  });
  it("does not treat an uncertain cleanup as complete", () => {
    expect(closeResult({ closed: [1], skipped: [2], uncertain: [3] }).uncertain).toEqual([3]);
    expect(() => closeResult({ closed: [1] })).toThrow(/Refresh/);
  });
});

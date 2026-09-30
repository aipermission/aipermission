import { describe, expect, it } from "vitest";
import { bulkCommandResponse, bulkCommandDetailResponse } from "./bulk-command-contracts";

const item = { request_id: 41, target_id: 7, target_name: "Example", status: "running" as const };
describe("Bulk command response ownership", () => {
  it("accepts unique positive request identities and verified output", () => {
    expect(bulkCommandResponse({ parallelism: 3, items: [item] })).toEqual({ parallelism: 3, items: [{ ...item, observed: false }] });
    expect(bulkCommandDetailResponse({ id: 41, runtime_id: 7, status: "completed", stdout: "ok", exit_code: 0 }, item)).toMatchObject({
      request_id: 41,
      target_id: 7,
      status: "completed",
      stdout: "ok",
      exit_code: 0,
    });
  });
  it("rejects invalid envelopes, output, and duplicate request identities", () => {
    for (const value of [
      null,
      {},
      { parallelism: 3, items: [] },
      { parallelism: "3", items: [item] },
      { parallelism: 3, items: [{ ...item, request_id: "41" }] },
      { parallelism: 3, items: [{ ...item, stdout: {} }] },
      { parallelism: 3, items: [item, item] },
    ])
      expect(() => bulkCommandResponse(value)).toThrow();
  });
  it("rejects wrong detail identities and malformed output before merging", () => {
    for (const value of [
      { id: 42, status: "completed" },
      { id: "41", status: "completed" },
      { id: 41, status: null },
      { id: 41, status: "completed", exit_code: "0" },
      { id: 41, status: "completed", stderr: [] },
    ])
      expect(() => bulkCommandDetailResponse(value, item)).toThrow();
    expect(bulkCommandDetailResponse({ id: 41, runtime_id: 7, status: "completed", target_id: 999, target_name: 123 }, item)).toMatchObject(
      {
        target_id: 7,
        target_name: "Example",
        observed: true,
      },
    );
    expect(() => bulkCommandDetailResponse({ id: 41, runtime_id: 99, status: "completed" }, item)).toThrow(/runtime mismatch/);
  });
});

import { describe, expect, it } from "vitest";
import { targetEndpoint } from "./model-helpers";

describe("Kafka broker endpoint", () => {
  it.each([undefined, null, {}, { config: {} }, { config: { bootstrap_brokers: "" } }])(
    "keeps the no-brokers fallback intact: %j",
    (target) => {
      expect(targetEndpoint(target)).toBe("no brokers");
    },
  );
  it("normalizes whitespace and comma-separated broker lists", () => {
    expect(targetEndpoint({ config: { bootstrap_brokers: " broker-a:9092,,\n broker-b:9092 " } })).toBe("broker-a:9092, broker-b:9092");
  });
});

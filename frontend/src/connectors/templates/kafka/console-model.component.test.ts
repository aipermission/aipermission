import { describe, expect, it } from "vitest";
import { kafkaConsoleModel } from "./console-model";

describe("Kafka console presentation", () => {
  it("retains broker normalization, transport and target/profile identity without persistence IDs", () => {
    const target = {
      ref: "kafka:3:7",
      connector_kind: "kafka",
      target_name: "Events",
      profile_label: "Reader",
      config: { server_family: "redpanda", bootstrap_brokers: "broker-a:9092, broker-b:9092", connection_mode: "over_ssh" },
    };
    expect(kafkaConsoleModel.targetDisplayName({ target })).toBe("Events");
    expect(kafkaConsoleModel.targetSubtitle({ target })).toBe("broker-a:9092, broker-b:9092 · over ssh");
    expect(kafkaConsoleModel.targetProfileLabel({ target })).toBe("Reader");
    expect(kafkaConsoleModel.usesLiveConsole({ target })).toBe(false);
    expect(kafkaConsoleModel.recoverableRunningActions({ target })).toEqual([]);
  });

  it("retains missing-target and empty-config defaults", () => {
    expect(kafkaConsoleModel.targetDisplayName({})).toBe("Kafka target");
    expect(kafkaConsoleModel.targetProfileLabel({ target: null })).toBe("monitor");
    expect(kafkaConsoleModel.targetSubtitle({ target: { ref: "kafka:3:7", connector_kind: "kafka" } })).toBe("no brokers · direct");
  });

  it.each([{ bootstrap_brokers: 123 }, { server_family: [] }, { connection_mode: false }])("rejects malformed config %j", (config) => {
    expect(() => kafkaConsoleModel.targetSubtitle({ target: { ref: "kafka:3:7", connector_kind: "kafka", config } })).toThrow(
      "Invalid Kafka console target",
    );
  });
});

import { describe, expect, it } from "vitest";
import { rabbitConsoleModel } from "./console-model";

describe("RabbitMQ console presentation", () => {
  it("retains endpoint, vhost, transport and target/profile identity without persistence IDs", () => {
    const target = {
      ref: "rabbitmq:3:7",
      connector_kind: "rabbitmq",
      name: "Queue",
      profile_label: "Reader",
      config: { scheme: "https", host: "queue.local", port: "15671", vhost: "jobs", connection_mode: "over_ssh" },
    };
    expect(rabbitConsoleModel.targetDisplayName({ target })).toBe("Queue");
    expect(rabbitConsoleModel.targetSubtitle({ target })).toBe("https://queue.local:15671 · vhost jobs · over ssh");
    expect(rabbitConsoleModel.targetProfileLabel({ target })).toBe("Reader");
    expect(rabbitConsoleModel.usesLiveConsole({ target })).toBe(false);
    expect(rabbitConsoleModel.recoverableRunningActions({ target })).toEqual([]);
  });

  it("retains missing-target and empty-config defaults", () => {
    expect(rabbitConsoleModel.targetDisplayName({})).toBe("RabbitMQ target");
    expect(rabbitConsoleModel.targetProfileLabel({ target: null })).toBe("monitor");
    expect(rabbitConsoleModel.targetSubtitle({ target: { ref: "rabbitmq:3:7", connector_kind: "rabbitmq" } })).toBe(
      "http://127.0.0.1:15672 · vhost / · direct",
    );
  });

  it.each([{ vhost: 123 }, { scheme: [] }, { host: false }, { port: NaN }, { connection_mode: {} }])(
    "rejects malformed config %j",
    (config) => {
      expect(() => rabbitConsoleModel.targetSubtitle({ target: { ref: "rabbitmq:3:7", connector_kind: "rabbitmq", config } })).toThrow(
        "Invalid RabbitMQ console target",
      );
    },
  );
});

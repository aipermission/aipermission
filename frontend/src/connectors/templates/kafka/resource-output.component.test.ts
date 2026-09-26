import { expect, it } from "vitest";
import { kafkaOutputDetail, kafkaOutputResources } from "./resource-output";
import { actionableOffsetPartitions } from "./console-helpers";

it("preserves Kafka metadata, opaque extensions, and exact 64-bit offset strings", () => {
  const topic = { name: "orders", internal: false, partition_count: 2, replication_factor: 3, extension: { future: true } };
  const group = { name: "consumers", state: "Empty", protocol_type: "consumer", coordinator: 3 };
  expect(kafkaOutputResources({ topics: [topic] }, "topics")[0]).toBe(topic);
  expect(kafkaOutputResources({ consumer_groups: [group] }, "consumer_groups")[0]).toBe(group);
  const partition = {
    partition: 0,
    topic: "orders",
    committed_offset: "9007199254740993",
    end_offset: "9223372036854775807",
    earliest_offset: "0",
    replicas: [1, 2, 3],
    extension: "retained",
  };
  const detail = {
    name: "orders",
    partitions: [partition],
    members: [{ member_id: "one", assignments: [{ topic: "orders", partitions: [0] }] }],
    truncated: true,
  };
  expect(kafkaOutputDetail(detail)).toBe(detail);
  expect(actionableOffsetPartitions(kafkaOutputDetail(detail)?.partitions)).toEqual([partition]);
});

it("rejects malformed rendered metadata without accepting arrays as records", () => {
  expect(kafkaOutputResources([], "topics")).toEqual([]);
  expect(kafkaOutputResources({ topics: [{ name: "good" }, { name: 2 }, { name: "bad", partition_count: "two" }] }, "topics")).toEqual([
    { name: "good" },
  ]);
  for (const value of [
    null,
    [],
    { members: {} },
    { partitions: [{}] },
    { partitions: [{ partition: -1 }] },
    { partitions: [{ partition: 0, end_offset: 42 }] },
  ]) {
    expect(kafkaOutputDetail(value)).toBeNull();
  }
  expect(kafkaOutputDetail({ partitions: [{ partition: 0, topic: "orders", error: "not visible" }] })).not.toBeNull();
});

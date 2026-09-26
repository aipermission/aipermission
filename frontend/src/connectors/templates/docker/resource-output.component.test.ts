import { expect, it } from "vitest";
import { dockerOutputRecord, dockerOutputResources } from "./resource-output";

it("preserves Docker resource projections and backend extension fields", () => {
  const fixtures = {
    containers: [{ id: "one", name: "api", image: "api:v1", state: "running", status: "Up", health: "healthy", ports: "80/tcp", labels: "app=api", compose_project: "app", compose_service: "api", extension: { retained: true } }],
    images: [{ id: "sha256:abc", repository: "api", tag: "v1", digest: "sha256:def", size: "10MB", created_since: "1d", containers: 1 }],
    networks: [{ id: "network", name: "apps", driver: "bridge", scope: "local", ipv6: "false", internal: "true", containers: 2, labels: "app=demo" }],
    volumes: [{ name: "data", driver: "local", scope: "local", mountpoint: "/data", containers: 0, labels: "app=demo" }],
  };
  for (const [kind, rows] of Object.entries(fixtures)) {
    const parsed = dockerOutputResources(fixtures, kind);
    expect(parsed).toEqual(rows);
    expect(parsed[0]).toBe(rows[0]);
  }
});

it("rejects malformed rendered fields without losing valid sibling rows", () => {
  const valid = { name: "api", containers: 0, ipv6: "false" };
  expect(dockerOutputResources({ containers: [null, [], true, { name: {} }, { containers: "2" }, { internal: 1 }, valid] }, "containers")).toEqual([valid]);
  expect(dockerOutputResources({ containers: {} }, "containers")).toEqual([]);
  expect(dockerOutputResources(null, "containers")).toEqual([]);
  expect(dockerOutputRecord("raw text")).toEqual({});
  expect(dockerOutputRecord([])).toEqual({});
});

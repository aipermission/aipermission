import { expect, it } from "vitest";
import { kubernetesOutputField, kubernetesOutputNamespaces, kubernetesOutputResources } from "./resource-output";

it("retains all Kubernetes resource projections and opaque backend metadata", () => {
  const fixtures = {
    workloads: [{ namespace: "apps", kind: "Deployment", name: "api", replicas: 3, available: 2, ready: "2/3", image: "api:v1", age: "1d" }],
    pods: [{ namespace: "apps", name: "api-1", ready: "1/1", restarts: 2, phase: "Running", node: "worker", age: "1d" }],
    services: [{ namespace: "apps", name: "api", type: "ClusterIP", cluster_ip: "10.0.0.1", external_ip: "", ports: "443/TCP", age: "1d" }],
    ingress: [{ namespace: "apps", name: "api", class: "nginx", hosts: "example.test", age: "1d" }],
    nodes: [{ name: "worker", ready: "Ready", roles: "worker", version: "v1.30", age: "1d" }],
    events: [{ namespace: "apps", object: "Pod/api-1", reason: "Scheduled", type: "Normal", count: 2, last_timestamp: "2026-01-01", message: "placed" }],
  };
  for (const [kind, rows] of Object.entries(fixtures)) {
    const parsed = kubernetesOutputResources(fixtures, kind);
    expect(parsed).toEqual(rows);
    expect(parsed[0]).toBe(rows[0]);
  }
});

it("rejects malformed rendered fields without dropping valid sibling resources", () => {
  const valid = { name: "api", count: 0, restarts: 0 };
  expect(kubernetesOutputResources({ pods: [null, [], 1, { name: {} }, { restarts: "2" }, { count: Infinity }, valid] }, "pods")).toEqual([valid]);
  expect(kubernetesOutputResources({ pods: {} }, "pods")).toEqual([]);
  expect(kubernetesOutputResources(null, "pods")).toEqual([]);
  expect(kubernetesOutputResources([], "pods")).toEqual([]);
  expect(kubernetesOutputNamespaces({ namespaces: [{ name: "apps", labels: { owner: "dev" } }, { name: 2 }, null] })).toEqual([{ name: "apps", labels: { owner: "dev" } }]);
  expect(kubernetesOutputNamespaces({ namespaces: null })).toEqual([]);
  expect(kubernetesOutputField("raw text", "logs")).toBeUndefined();
});

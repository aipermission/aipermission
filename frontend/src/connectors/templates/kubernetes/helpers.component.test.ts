import assert from "node:assert/strict";
import { test } from "vitest";
import {
  resourceKey,
  resourceSearchValues,
  resourceStatus,
  resourceSubtitle,
  resourceTabs,
  resourceTertiary,
  resourceTitle,
  resourceTone,
  resourceTypeForWorkload,
} from "./helpers.ts";

test("Kubernetes resource helpers keep pod selection and filtering stable", () => {
  const pod = {
    namespace: "monitoring",
    name: "api-7f9d",
    node: "worker-2",
    phase: "Running",
    image: "example/api:latest",
  };

  assert.equal(resourceKey("pods", pod), "monitoring/pods/api-7f9d");
  assert.equal(resourceTitle("pods", pod), "worker-2");
  assert.equal(resourceTone("pods", pod), "good");
  assert.ok(resourceSearchValues("pods", pod).includes("example/api:latest"));
});

test("Kubernetes resource modes preserve stable identity and compact metadata", () => {
  const item = {
    namespace: "demo",
    name: "api",
    kind: "Deployment",
    ready: "1/2",
    image: "api:latest",
    type: "ClusterIP",
    cluster_ip: "10.0.0.1",
    ports: "80/tcp",
    age: "2d",
    hosts: "app.example.com",
    class: "nginx",
    roles: "worker",
    version: "v1",
    external_ip: "none",
    object: "Pod/api",
    reason: "Scheduled",
    last_timestamp: "now",
    count: 2,
    message: "Ready",
  };
  assert.equal(resourceKey("nodes", item), "api");
  assert.equal(resourceKey("events", item), "demo/Pod/api/Scheduled/now/2/Ready");
  assert.equal(resourceTitle("workloads", item), "demo/Deployment/api");
  assert.equal(resourceTitle("events", item), "ClusterIP Scheduled");
  assert.equal(resourceTitle("nodes", item), "api");
  assert.equal(resourceSubtitle("services", item), "ClusterIP · 10.0.0.1 · 80/tcp");
  assert.equal(resourceSubtitle("ingress", item), "app.example.com · nginx");
  assert.equal(resourceSubtitle("nodes", item), "ready 1/2 · worker · v1");
  assert.equal(resourceTertiary("services", item), "age 2d · external none");
  assert.equal(resourceTertiary("ingress", item), "namespace demo · age 2d");
  assert.equal(resourceTertiary("nodes", item), "age 2d · status 1/2");
  assert.equal(resourceTypeForWorkload(undefined), "deployment");
  for (const { key, output } of resourceTabs) {
    assert.equal(key, output);
    assert.equal(resourceKey(key, null), "");
    assert.equal(resourceTitle(key, null), "");
    assert.equal(resourceSubtitle(key, null), "");
    assert.equal(resourceTertiary(key, null), "");
    assert.equal(resourceStatus(key, null), "");
    assert.equal(resourceTone(key, null), "neutral");
    assert.ok(resourceSearchValues(key, item).includes("api:latest"));
  }
  assert.equal(resourceTone("pods", { phase: "Pending" }), "warn");
  assert.equal(resourceTone("workloads", { ready: "1/0" }), "warn");
});

test("Kubernetes resource helpers classify workload actions and warning events", () => {
  assert.equal(resourceTypeForWorkload({ kind: "StatefulSet" }), "statefulset");
  assert.equal(resourceTypeForWorkload({ kind: "DaemonSet" }), "daemonset");
  assert.equal(resourceTypeForWorkload({ kind: "Deployment" }), "deployment");
  assert.equal(resourceTone("events", { type: "Warning" }), "warn");
  assert.equal(resourceTone("pods", { phase: "CrashLoopBackOff" }), "bad");
});

test("Kubernetes workload tones distinguish ready, degraded, unavailable, and scaled-to-zero states", () => {
  assert.equal(resourceTone("workloads", { ready: "3/3" }), "good");
  assert.equal(resourceTone("workloads", { ready: "1/3" }), "warn");
  assert.equal(resourceTone("workloads", { ready: "0/3" }), "bad");
  assert.equal(resourceTone("workloads", { ready: "0/0" }), "neutral");
  assert.equal(resourceTone("workloads", { ready: "4/3" }), "warn");
  assert.equal(resourceTone("workloads", {}), "neutral");
});

test("resource metadata uses explicit fallbacks for absent optional fields", () => {
  const resource = { name: "api", namespace: "apps" };
  assert.equal(resourceTitle("pods", resource), "api");
  assert.equal(resourceTitle("events", { message: "Scheduled" }), "Event");
  assert.equal(resourceSubtitle("workloads", resource), "ready - · image -");
  assert.equal(resourceSubtitle("services", resource), "- · - · no ports");
  assert.equal(resourceSubtitle("ingress", resource), "no hosts · no class");
  assert.equal(resourceSubtitle("nodes", resource), "ready - · - · -");
  assert.equal(resourceSubtitle("events", resource), "apps · - · ");
  assert.equal(resourceTertiary("pods", resource), "ready - · restarts 0 · -");
  assert.equal(resourceTertiary("workloads", resource), "apps · age -");
  assert.equal(resourceTertiary("services", resource), "age - · external -");
  assert.equal(resourceTertiary("ingress", resource), "namespace apps · age -");
  assert.equal(resourceTertiary("nodes", resource), "age - · status -");
  assert.equal(resourceTertiary("events", resource), "");
  assert.equal(resourceKey("events", { namespace: "apps", object: "Pod/api" }), "apps/Pod/api////");
  assert.equal(resourceKey("pods", { reason: "Evicted" }), "/pods/Evicted");
  assert.equal(resourceKey("pods", { message: "Pending" }), "/pods/Pending");
  assert.equal(resourceKey("pods", {}), "/pods/");
  assert.equal(resourceKey("nodes", {}), "");
  assert.equal(resourceTone("pods", { phase: "Unknown" }), "warn");
  assert.equal(resourceTone("nodes", { ready: "Ready" }), "good");
  assert.equal(resourceTone("pods", { phase: "Failed" }), "bad");
  assert.equal(resourceTone("pods", { phase: "Error" }), "bad");
});

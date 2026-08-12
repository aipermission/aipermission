import assert from "node:assert/strict";
import test from "node:test";
import { resourceKey, resourceSearchValues, resourceTitle, resourceTone, resourceTypeForWorkload } from "./helpers.js";

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

test("Kubernetes resource helpers classify workload actions and warning events", () => {
  assert.equal(resourceTypeForWorkload({ kind: "StatefulSet" }), "statefulset");
  assert.equal(resourceTypeForWorkload({ kind: "DaemonSet" }), "daemonset");
  assert.equal(resourceTypeForWorkload({ kind: "Deployment" }), "deployment");
  assert.equal(resourceTone("events", { type: "Warning" }), "warn");
  assert.equal(resourceTone("pods", { phase: "CrashLoopBackOff" }), "bad");
});

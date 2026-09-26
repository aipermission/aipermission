import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { KubernetesFooter, KubernetesHeaderStatus, KubernetesResourceDetail } from "./resource-detail";
import type { KubernetesResourceKind } from "./resource-types";

it.each<[KubernetesResourceKind, string]>([["workloads", "Resource metadata"], ["services", "Resource metadata"], ["ingress", "Resource metadata"], ["nodes", "Node metadata"], ["events", "Event details"]])("preserves %s metadata and raw data sections", (tab, title) => {
  const resource = { name: "api", namespace: "apps", kind: "Deployment", ready: "1/1", image: "example/api", type: "Normal", reason: "Scheduled", object: "pod/api", count: 2, message: "placed", class: "nginx", hosts: "example.test", age: "2d", roles: "worker", version: "v1", ports: "443/TCP", cluster_ip: "10.0.0.1" };
  render(<KubernetesResourceDetail tab={tab} resource={resource} detail={{ output: { resource: { apiVersion: "v1", metadata: { name: "api" } } } }} logs="" search="" onSearch={() => {}} inputClass="" mutedClass="" />);
  expect(screen.getByText(title)).toBeVisible();
  expect(screen.getByText("Kubernetes raw data")).toBeVisible();
  expect(screen.getByText(/"apiVersion": "v1"/)).toBeVisible();
  if (tab === "nodes") expect(screen.getByText(/do not expose pod-style logs/)).toBeVisible();
  if (tab === "events") expect(screen.getByText("placed")).toBeVisible();
});

it("preserves raw scalar output and pod logs without interpreting their content", () => {
  render(<KubernetesResourceDetail tab="pods" resource={{ name: "api", namespace: "apps" }} detail={{ output: "raw detail" }} logs="<script>untrusted</script>" search="" onSearch={() => {}} inputClass="" mutedClass="" />);
  expect(screen.getByText("Pod logs")).toBeVisible();
  expect(screen.getByText("<script>untrusted</script>")).toBeVisible();
  expect(screen.getByText('"raw detail"')).toBeVisible();
  expect(document.querySelector("script")).toBeNull();
});

it("keeps header status and transport identity in stable slots", () => {
  const view = render(<><KubernetesHeaderStatus state={{ state: "reading" }} mutedClass="" /><KubernetesFooter target={{ config: { transport_target_ref: "transport:opaque" } }} borderClass="" mutedClass="" /></>);
  expect(screen.getByText("reading")).toBeVisible();
  expect(screen.getByText("transport:opaque")).toBeVisible();
  view.rerender(<><KubernetesHeaderStatus state={{ state: "error", error: "connection failed" }} mutedClass="" /><KubernetesFooter target={{}} borderClass="" mutedClass="" /></>);
  expect(screen.getByText("connection failed")).toBeVisible();
  expect(screen.getByText("no transport")).toBeVisible();
});

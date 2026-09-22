import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { KubernetesResourceBrowser } from "./resource-browser";

it("renders an unavailable workload with a bad readiness badge", () => {
  const resource = { namespace: "production", kind: "Deployment", name: "api", ready: "0/3", image: "example/api", age: "2d" };
  render(
    <KubernetesResourceBrowser
      theme="dark"
      styles={{ border: "", subtlePanel: "", muted: "", rowHover: "", activeRow: "", input: "" }}
      browser={{
        filteredResources: [resource],
        activeResources: [resource],
        latestAction: null,
        state: { state: "idle" },
        tab: "workloads",
        activeTab: { label: "Workloads" },
        namespace: "",
        namespaces: [],
        filter: "",
        selectedKey: "",
        refreshResource: vi.fn(),
        switchTab: vi.fn(),
        changeNamespace: vi.fn(),
        setFilter: vi.fn(),
        selectResource: vi.fn(),
      }}
    />,
  );

  expect(screen.getByText("0/3")).toHaveClass("dark-badge-bad");
});

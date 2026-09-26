import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import { KubernetesResourceBrowser } from "./resource-browser";
import { resourceKey } from "./helpers";

it("preserves Kubernetes selection, filter, namespace and refresh contracts", async () => {
  const user = userEvent.setup();
  const pod = { namespace: "apps", name: "api-1", phase: "Running", ready: "1/1" };
  const browser: ComponentProps<typeof KubernetesResourceBrowser>["browser"] = {
    tab: "pods", filteredResources: [pod], activeResources: [pod], selectedKey: resourceKey("pods", pod), latestAction: { status: "completed", action_name: "list_pods" }, state: { state: "idle" }, namespace: "", namespaces: [{ name: "apps" }], filter: "", activeTab: { label: "Pods" }, refreshResource: vi.fn(), switchTab: vi.fn(), changeNamespace: vi.fn(), setFilter: vi.fn(), selectResource: vi.fn(),
  };
  const props = { browser, styles: { border: "", subtlePanel: "", muted: "", rowHover: "", activeRow: "", input: "" }, theme: "light" };
  const view = render(<KubernetesResourceBrowser {...props} />);
  const row = screen.getByRole("button", { name: /api-1/ });
  expect(row).toHaveAttribute("aria-pressed", "true");
  await user.click(row);
  await user.click(screen.getByRole("button", { name: "Refresh resources" }));
  await user.click(screen.getByRole("tab", { name: "Nodes" }));
  await user.selectOptions(screen.getByRole("combobox", { name: "Kubernetes namespace" }), "apps");
  await user.type(screen.getByRole("textbox", { name: "Filter pods" }), "a");
  expect(browser.selectResource).toHaveBeenCalledWith(pod);
  expect(browser.refreshResource).toHaveBeenCalledWith("pods");
  expect(browser.switchTab).toHaveBeenCalledWith("nodes");
  expect(browser.changeNamespace).toHaveBeenCalledWith("apps");
  expect(browser.setFilter).toHaveBeenCalledWith("a");
  view.rerender(<KubernetesResourceBrowser {...props} browser={{ ...browser, filteredResources: [], state: { state: "loading" } }} />);
  expect(screen.getByText("Loading Kubernetes resources...")).toBeVisible();
  expect(screen.getByRole("button", { name: "Refresh resources" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Refresh resources" }));
  expect(browser.refreshResource).toHaveBeenCalledOnce();
  view.rerender(<KubernetesResourceBrowser {...props} browser={{ ...browser, filteredResources: [], state: { state: "error" }, latestAction: { status: "failed", action_name: "list_pods" } }} />);
  expect(screen.getByText("No resources found for this filter.")).toBeVisible();
  expect(screen.getByRole("button", { name: "Refresh resources" })).toBeEnabled();
});

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

it("keeps an unclassified resource selectable while an action is running", async () => {
  const user = userEvent.setup();
  const resource = { namespace: "apps", name: "public-api" };
  const selectResource = vi.fn();
  render(
    <KubernetesResourceBrowser
      theme="dark"
      styles={{ border: "", subtlePanel: "", muted: "", rowHover: "", activeRow: "", input: "" }}
      browser={{
        filteredResources: [resource],
        activeResources: [resource],
        latestAction: { status: "running", action_name: "list_services" },
        state: { state: "running" },
        tab: "services",
        activeTab: { label: "Services" },
        namespace: "",
        namespaces: [],
        filter: "",
        selectedKey: "",
        refreshResource: vi.fn(),
        switchTab: vi.fn(),
        changeNamespace: vi.fn(),
        setFilter: vi.fn(),
        selectResource,
      }}
    />,
  );
  const row = screen.getByRole("button", { name: /apps\/public-api/ });
  expect(row).toHaveAttribute("aria-pressed", "false");
  expect(screen.queryByText("ClusterIP")).not.toBeInTheDocument();
  expect(screen.getByText("list_services")).toHaveClass("dark-badge-warn");
  expect(screen.getByRole("button", { name: "Refresh resources" })).toBeDisabled();
  await user.click(row);
  expect(selectResource).toHaveBeenCalledWith(resource);
});

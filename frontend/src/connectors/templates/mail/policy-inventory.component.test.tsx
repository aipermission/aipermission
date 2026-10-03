import { render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { captureConnectorFamily } from "../../editor/capture-connector-family";
import { ConnectorTargetsTable } from "../../editor/connector-targets-table";
import type { ConnectorTargetsTableProps } from "../../editor/connector-target-table-types";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../../test/connector-inventory-fixtures";
import { mailConnectorFamily } from "./connector-family";

it.each([true, 42, { allowed: "example.test" }, ["example.test", null], [" "]])(
  "contains malformed Mail policy %j at its own inventory row without silently repairing it",
  (policy) => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const family = mailConnectorFamily.create(captureConnectorFamily);
    const valid = inventoryTargetFixture({
      connector_kind: "mail",
      name: "Healthy mailbox",
      config: { imap_host: "imap.example.test", allowed_recipient_domains: ["example.test"] },
      profiles: [inventoryProfileFixture({ connector_kind: "mail", public: { allowed_read_folders: ["INBOX"] } })],
    });
    const invalid = inventoryTargetFixture({
      ...valid,
      id: 44,
      name: "Malformed mailbox",
      config: { allowed_recipient_domains: policy },
    });
    const props: ConnectorTargetsTableProps = {
      targets: { state: "ready", data: [invalid, valid], error: null },
      projects: [{ id: 7, name: "My Project", slug: "my-project", target_count: 2 }],
      search: "",
      collapsedProjects: {},
      onSearch: vi.fn(),
      onToggleProject: vi.fn(),
      catalog: { state: "ready", data: [{ kind: "mail", label: "Mail", version: "0.2" }], details: {}, detailFailures: [], error: null },
      unifiedTargets: [],
      credentials: [],
      profileSelections: {},
      tests: {},
      onSelectProfile: vi.fn(),
      onTestConnector: vi.fn(),
      onUnderConstruction: vi.fn(),
      onEdit: vi.fn(),
      onDelete: vi.fn(),
      resolveTemplate: () => family.tableTemplate,
    };
    try {
      const view = render(<ConnectorTargetsTable {...props} />);
      expect(screen.getByText(/Connector unavailable: Malformed mailbox/)).toBeVisible();
      const row = screen.getByText(/Connector unavailable: Malformed mailbox/).closest("tr");
      if (!row) throw new Error("Missing policy error row");
      expect(within(row).queryByRole("button")).not.toBeInTheDocument();
      expect(screen.getByText("Healthy mailbox")).toBeVisible();
      expect(props.onTestConnector).not.toHaveBeenCalled();
      expect(props.onEdit).not.toHaveBeenCalled();
      props.targets = { ...props.targets, data: [inventoryTargetFixture({ ...invalid, config: valid.config }), valid] };
      view.rerender(<ConnectorTargetsTable {...props} />);
      expect(screen.queryByText(/Connector unavailable:/)).not.toBeInTheDocument();
      expect(screen.getByText("Malformed mailbox")).toBeVisible();
      expect(screen.getAllByRole("button", { name: "Edit connector" })).toHaveLength(2);
    } finally {
      error.mockRestore();
    }
  },
);

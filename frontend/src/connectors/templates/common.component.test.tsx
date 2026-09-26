import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { ConnectorIcon, ConnectorKindCell, ProfilesCell, StatusCell, TargetCell, catalogLabel } from "./common";

it("uses the catalog label or a generic metadata fallback", () => {
  expect(catalogLabel({ data: [{ kind: "custom", label: "Custom connector" }] }, "custom")).toBe("Custom connector");
  expect(catalogLabel({ data: [] }, "custom-service")).toBe("Custom Service");
  render(<table><tbody><tr><ConnectorKindCell target={{ connector_kind: "custom", id: 7 }} catalog={{ data: [{ kind: "custom", label: "Custom connector" }] }} /><TargetCell target={{ name: "My connector" }} endpoint="host:1234" /></tr></tbody></table>);
  expect(screen.getByText("custom:7")).toBeInTheDocument();
  expect(screen.getByText("My connector")).toBeInTheDocument();
  expect(screen.getByText("host:1234")).toBeInTheDocument();
});

it("renders profiles, empty fallback, and lifecycle status", () => {
  const { rerender } = render(<><ProfilesCell target={{ profiles: [{ id: 7, label: "main", ref: "custom:1:7" }] }} /><StatusCell target={{ status: "active" }} /></>);
  expect(screen.getByText("main")).toHaveAttribute("title", "custom:1:7");
  expect(screen.getByText("active")).toBeInTheDocument();
  rerender(<><ProfilesCell target={{}} /><StatusCell target={{ status: "disabled" }} /><ConnectorIcon kind="custom" /></>);
  expect(screen.getByText("No profiles")).toBeInTheDocument();
  expect(screen.getByText("disabled")).toBeInTheDocument();
});

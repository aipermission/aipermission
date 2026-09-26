import { render, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { MCPSetupPage } from "./mcp-setup.tsx";
import { mcpClientCatalog } from "../lib/mcp-client-catalog.ts";
import { mcpPackageSpecifier } from "../lib/mcp-package.ts";

it("renders the supported MCP client catalog without inventing providers", () => {
  render(<MCPSetupPage />);
  const title = screen.getByRole("heading", { name: "Providers" });
  const panel = title.closest("section");
  if (!panel) throw new Error("Provider card missing");
  for (const client of mcpClientCatalog.filter((item) => item.supportsMCP || item.id === "custom")) {
    expect(within(panel).getByText(client.label)).toBeVisible();
  }
});

it("pins manual configuration to the published package and uses only a token placeholder", () => {
  render(<MCPSetupPage />);
  const manual = screen.getByText(/"mcpServers":/);
  expect(manual).toHaveTextContent(mcpPackageSpecifier);
  expect(manual).toHaveTextContent('"AIPERMISSION_API_TOKEN": "TOKEN"');
  expect(screen.getByText(/bearer token/)).toBeVisible();
});

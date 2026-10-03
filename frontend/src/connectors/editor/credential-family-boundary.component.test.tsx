import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { InventoryTarget } from "../../lib/gateway-contracts/connector-inventory-contract";
import type { CredentialResource } from "../../lib/gateway-contracts/core-resource-contracts";
import { InventoryRowBoundary } from "./inventory-row-boundary";

function FailedFamily(): ReactNode {
  throw new Error("Fixture private detail must not be shown");
}
function HealthyFamily() {
  return (
    <tr>
      <td>Healthy family</td>
    </tr>
  );
}

it("isolates failed native credential renderers and retries only with refreshed inputs", () => {
  const error = vi.spyOn(console, "error").mockImplementation(() => {});
  const targets: InventoryTarget[] = [];
  const credentials: CredentialResource[] = [];
  function table(child: ReactNode, targetRows = targets, resources = credentials) {
    return (
      <table>
        <tbody>
          <InventoryRowBoundary label="Connector credentials unavailable: example" inputs={[targetRows, resources]}>
            {child}
          </InventoryRowBoundary>
          <tr>
            <td>Other family</td>
          </tr>
        </tbody>
      </table>
    );
  }
  try {
    const result = render(table(<HealthyFamily />));
    expect(screen.getByText("Healthy family")).toBeVisible();
    result.rerender(table(<FailedFamily />));
    expect(screen.getByText(/Connector credentials unavailable: example/)).toBeVisible();
    expect(screen.getByText("Other family")).toBeVisible();
    expect(screen.queryByText("Fixture private detail must not be shown")).not.toBeInTheDocument();
    result.rerender(table(<HealthyFamily />));
    expect(screen.queryByText("Healthy family")).not.toBeInTheDocument();
    result.rerender(table(<HealthyFamily />, [...targets]));
    expect(screen.getByText("Healthy family")).toBeVisible();
    result.rerender(table(<FailedFamily />, [...targets]));
    expect(screen.getByText(/Connector credentials unavailable: example/)).toBeVisible();
    result.rerender(table(<HealthyFamily />, targets, [...credentials]));
    expect(screen.getByText("Healthy family")).toBeVisible();
    expect(error).toHaveBeenCalled();
  } finally {
    error.mockRestore();
  }
});

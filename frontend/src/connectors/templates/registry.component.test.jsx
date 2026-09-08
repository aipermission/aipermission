import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";

import { ConnectorTemplateNotFound, getConnectorModel, getConnectorTemplate } from "./registry";

it("returns registered templates and renders both missing-slot fallbacks", () => {
  expect(getConnectorTemplate("postgres")?.metadata.kind).toBe("postgres");
  expect(getConnectorModel("postgres")?.emptyForm).toBeTypeOf("function");
  expect(getConnectorTemplate("missing-kind")).toBeNull();

  const { rerender } = render(<ConnectorTemplateNotFound kind="missing-kind" slot="Console" />);
  expect(screen.getByText(/missing-kind\/Console/)).toBeVisible();

  rerender(
    <table>
      <tbody>
        <ConnectorTemplateNotFound kind="missing-kind" slot="RowActions" as="tr" colSpan={3} />
      </tbody>
    </table>,
  );
  expect(screen.getByText(/missing-kind\/RowActions/).closest("td")).toHaveAttribute("colspan", "3");
});

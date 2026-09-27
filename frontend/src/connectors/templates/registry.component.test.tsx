import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";

import { ConnectorTemplateNotFound, getConnectorModel, getConnectorTemplate } from "./registry";
import { sshConsoleRecovery } from "./ssh/console-recovery";

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
it("resolves captured session recovery while retaining native template slots", () => {
  const template = getConnectorTemplate("ssh");
  expect(template?.Operations).toBe(sshConsoleRecovery.Operations);
  expect(template?.Form).toBeTypeOf("function");
  expect(template?.model.emptyForm).toBeTypeOf("function");
});

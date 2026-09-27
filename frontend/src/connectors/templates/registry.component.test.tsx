import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";

import { ConnectorTemplateNotFound, getConnectorTemplate, registerConnectorTemplates } from "./registry";
import { sshConsoleRecovery } from "./ssh/console-recovery";
import nativeSSH from "./ssh";

it("returns registered templates and renders both missing-slot fallbacks", () => {
  expect(getConnectorTemplate("postgres")?.metadata.kind).toBe("postgres");
  expect(getConnectorTemplate("postgres")?.Console).toBeTypeOf("function");
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
it("resolves captured session recovery without exposing native editor models", () => {
  const template = getConnectorTemplate("ssh");
  expect(template?.Operations).toBe(sshConsoleRecovery.Operations);
  expect(template).not.toHaveProperty("Form");
  expect(template).not.toHaveProperty("model");
  expect(Object.isFrozen(template)).toBe(true);
});

it("rejects unregistered templates and incomplete catalogs", () => {
  expect(() => registerConnectorTemplates({ "./ssh/index.ts": {} })).toThrow(/defineConsoleTemplate/);
  expect(() => registerConnectorTemplates({})).toThrow(/catalog\/registry mismatch/);
  expect(() => registerConnectorTemplates({ "./unregistered/index.ts": nativeSSH })).toThrow("Connector unregistered metadata is missing.");
  for (const kind of ["constructor", "toString", "__proto__"]) {
    expect(getConnectorTemplate(kind)).toBeNull();
  }
});

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import {
  ActivityStatusBadge,
  formatConnectorTime,
  ResultViewToggle,
  SQLConnectorToolbarActions,
  SQLEndpointFooter,
  SQLNoSessionPlaceholder,
} from "./sql-console-chrome";

const config = { label: "SQL", targetEndpoint: (target) => `db://${target.name}` };

it("renders and operates the SQL session chrome", async () => {
  const user = userEvent.setup();
  const onNewSession = vi.fn();
  const onEndSession = vi.fn();
  const onToggle = vi.fn();
  const { rerender } = render(
    <>
      <SQLNoSessionPlaceholder config={config} target={{ name: "main" }} theme="dark" onNewSession={onNewSession} />
      <SQLEndpointFooter config={config} target={{ name: "main" }} borderClass="border" mutedClass="muted" />
      <SQLConnectorToolbarActions
        label="SQL"
        theme="dark"
        structuredSession={{ active: false }}
        onNewStructuredSession={onNewSession}
        onEndStructuredSession={onEndSession}
      />
      <ResultViewToggle checked={false} onChange={onToggle} theme="light" />
      <ActivityStatusBadge status="running" />
    </>,
  );
  expect(screen.getByText("db://main")).toBeVisible();
  await user.click(screen.getAllByRole("button", { name: "New Session" })[0]);
  await user.click(screen.getByRole("switch"));
  expect(onNewSession).toHaveBeenCalledOnce();
  expect(onToggle).toHaveBeenCalledWith(true);
  expect(screen.getByRole("button", { name: "End Session" })).toBeDisabled();

  rerender(
    <SQLConnectorToolbarActions
      label="SQL"
      theme="light"
      structuredSession={{ active: true }}
      onNewStructuredSession={onNewSession}
      onEndStructuredSession={onEndSession}
    />,
  );
  await user.click(screen.getByRole("button", { name: "End Session" }));
  expect(onEndSession).toHaveBeenCalledOnce();
});

it("maps activity tones and formats optional times", () => {
  const { rerender } = render(<ActivityStatusBadge status="completed" />);
  rerender(<ActivityStatusBadge status="failed" />);
  rerender(<ActivityStatusBadge status="idle" />);
  expect(formatConnectorTime("")).toBe("-");
  expect(formatConnectorTime("2026-01-01T10:00:00Z")).not.toBe("-");
});

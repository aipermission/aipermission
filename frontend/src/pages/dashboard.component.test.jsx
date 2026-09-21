import { act, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { DashboardPage } from "./dashboard";

const tokens = [
  { id: 1, expires_at: "2999-01-01T00:00:00Z" },
  { id: 2, expires_at: "2000-01-01T00:00:00Z" },
  { id: 3, revoked_at: "2000-01-01T00:00:00Z" },
];

vi.mock("../lib/gateway-context", () => ({
  useGateway: () => ({
    targets: { data: [] },
    credentials: { data: [] },
    tokens: {
      data: tokens,
    },
    gatewayState: "running",
  }),
}));

afterEach(() => vi.useRealTimers());
beforeEach(() => {
  tokens.splice(
    0,
    tokens.length,
    { id: 1, expires_at: "2999-01-01T00:00:00Z" },
    { id: 2, expires_at: "2000-01-01T00:00:00Z" },
    { id: 3, revoked_at: "2000-01-01T00:00:00Z" },
  );
});

it("counts only unrevoked, unexpired tokens as active", () => {
  render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  );

  const tokenMetric = screen.getByRole("link", { name: /Tokens/ });
  expect(within(tokenMetric).getByText("1")).toBeVisible();
  expect(within(tokenMetric).getByText("active MCP/API tokens")).toBeVisible();
});

it("updates the active token metric when a token expires", async () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-09-21T12:00:00.000Z"));
  tokens.splice(0, tokens.length, { id: 4, expires_at: "2026-09-21T12:00:01.000Z" });
  render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  );

  const tokenMetric = screen.getByRole("link", { name: /Tokens/ });
  expect(within(tokenMetric).getByText("1")).toBeVisible();
  await act(async () => vi.advanceTimersByTimeAsync(1001));
  expect(within(tokenMetric).getByText("0")).toBeVisible();
});

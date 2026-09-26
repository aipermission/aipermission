import { useState } from "react";
import { MemoryRouter } from "react-router";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { BackupFreshnessNotices } from "./backup-freshness-notices.tsx";
import type { useGatewayActivityResources } from "./use-gateway-activity-resources.ts";

type Freshness = ReturnType<typeof useGatewayActivityResources>["backupFreshness"];
function Harness({ initial }: { initial: Freshness }) {
  const [value, setValue] = useState(initial);
  return (
    <MemoryRouter>
      <BackupFreshnessNotices value={value} onChange={setValue} />
    </MemoryRouter>
  );
}

it("dismisses newer-backup warnings separately from provider check failures", async () => {
  const user = userEvent.setup();
  render(
    <Harness
      initial={{
        state: "ready",
        data: [{ provider_id: 1, latest_remote_at: "2026-09-26T00:00:00Z" }],
        checkErrors: [{ provider_id: 2 }],
        error: null,
      }}
    />,
  );
  expect(screen.getByText(/newer encrypted backup is available/)).toBeVisible();
  expect(screen.getByText(/freshness could not be checked/)).toBeVisible();
  expect(screen.getAllByRole("link", { name: "Review backups" })[0]).toHaveAttribute("href", "/settings");
  await user.click(screen.getAllByRole("button", { name: "Dismiss" })[0]);
  expect(screen.queryByText(/newer encrypted backup/)).not.toBeInTheDocument();
  expect(screen.getByText(/freshness could not be checked/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Dismiss" }));
  expect(screen.queryByText(/freshness could not be checked/)).not.toBeInTheDocument();
});

it("shows provider counts and a failed overall check without discarding either warning", () => {
  render(<Harness initial={{ state: "error", data: [{ provider_id: 1 }, { provider_id: 2 }], checkErrors: [], error: "offline" }} />);
  expect(screen.getByText(/in 2 providers/)).toBeVisible();
  expect(screen.getByText(/freshness could not be checked/)).toBeVisible();
});

it("does not display a warning when checks completed without newer backups", () => {
  render(<Harness initial={{ state: "ready", data: [], checkErrors: [], error: null }} />);
  expect(screen.queryByRole("button", { name: "Dismiss" })).not.toBeInTheDocument();
});

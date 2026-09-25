import { expect, it, vi } from "vitest";
import { apiGet } from "./api";
import { loadProjectOptions } from "./load-project-options";
import { createRequestGuard } from "./request-guard";

vi.mock("./api", () => ({ apiGet: vi.fn() }));

it("does not publish project options from an invalidated request", async () => {
  const guard = createRequestGuard("first");
  let complete: (_result: { items: { id: number; slug: string }[] }) => void = () => {};
  vi.mocked(apiGet).mockReturnValue(
    new Promise((resolve) => {
      complete = resolve;
    }),
  );
  const setProjects = vi.fn();
  const loading = loadProjectOptions(guard, setProjects);
  guard.setScope("second");
  complete({ items: [{ id: 1, slug: "example" }] });
  await loading;
  expect(setProjects).toHaveBeenCalledTimes(1);
  expect(setProjects).not.toHaveBeenCalledWith(expect.objectContaining({ state: "ready" }));
  guard.dispose();
});

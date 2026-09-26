import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";
import { ProjectsPage } from "./projects";

vi.mock("../lib/api", () => ({ apiDelete: vi.fn(), apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn() }));

function deferred() {
  let resolve: (_value: unknown) => void = () => {
    throw new Error("Deferred request is not initialized");
  };
  let reject: (_reason: unknown) => void = () => {
    throw new Error("Deferred request is not initialized");
  };
  const promise = new Promise<unknown>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  vi.mocked(apiGet)
    .mockReset()
    .mockResolvedValue({
      items: [
        { id: 1, name: "First", slug: "first", target_count: 0 },
        { id: 2, name: "Second", slug: "second", target_count: 0 },
      ],
    });
  vi.mocked(apiPost).mockReset();
  vi.mocked(apiPut).mockReset();
  vi.mocked(apiDelete).mockReset();
});

it("does not close a newer project draft when an earlier save finishes", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  vi.mocked(apiPost).mockReturnValueOnce(pending.promise);
  render(<ProjectsPage />);
  await screen.findByText("First");

  await user.click(screen.getByRole("button", { name: "Add project" }));
  await user.type(screen.getByRole("textbox", { name: "Project name" }), "Old draft");
  await user.click(screen.getByRole("button", { name: "Save project" }));
  await waitFor(() => expect(apiPost).toHaveBeenCalledOnce());
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  await user.click(screen.getByRole("button", { name: "Add project" }));
  await user.type(screen.getByRole("textbox", { name: "Project name" }), "New draft");
  pending.resolve({ id: 3 });

  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
  expect(screen.getByRole("textbox", { name: "Project name" })).toHaveValue("New draft");
  expect(screen.queryByText("Project created.")).not.toBeInTheDocument();
});

it("does not close a newer archive dialog when the old archive finishes", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  vi.mocked(apiDelete).mockReturnValueOnce(pending.promise);
  render(<ProjectsPage />);
  await screen.findByText("First");

  const archiveButtons = screen.getAllByTitle("Archive project");
  await user.click(archiveButtons[0]);
  await user.click(screen.getByRole("button", { name: "Archive project" }));
  await waitFor(() => expect(apiDelete).toHaveBeenCalledOnce());
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  await user.click(archiveButtons[1]);
  pending.resolve(null);

  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
  expect(screen.getByText(/Archive Second\?/)).toBeVisible();
  expect(screen.queryByText("Project archived.")).not.toBeInTheDocument();
});

it("creates and renames a project using the selected identity", async () => {
  const user = userEvent.setup();
  vi.mocked(apiPost).mockResolvedValue({});
  vi.mocked(apiPut).mockResolvedValue({});
  render(<ProjectsPage />);
  await screen.findByText("First");
  await user.click(screen.getByRole("button", { name: "Add project" }));
  await user.type(screen.getByRole("textbox", { name: "Project name" }), "My Project");
  await user.click(screen.getByRole("button", { name: "Save project" }));
  expect(apiPost).toHaveBeenCalledWith(
    "/api/projects",
    { name: "My Project" },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  expect(await screen.findByText("Project created.")).toBeVisible();
  await user.click(screen.getAllByTitle("Rename project")[1]);
  const name = screen.getByRole("textbox", { name: "Project name" });
  await user.clear(name);
  await user.type(name, "Renamed Project");
  await user.click(screen.getByRole("button", { name: "Save project" }));
  expect(apiPut).toHaveBeenCalledWith(
    "/api/projects/2",
    { name: "Renamed Project" },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  expect(await screen.findByText("Project renamed.")).toBeVisible();
});

it("does not render malformed gateway project records", async () => {
  vi.mocked(apiGet).mockResolvedValue({ items: [{ id: 1, name: { unsafe: "not a label" }, slug: "project", target_count: 1 }] });
  render(<ProjectsPage />);
  expect(await screen.findByText("Invalid project list response.")).toBeVisible();
});

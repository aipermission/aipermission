import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";
import { ProjectsPage } from "./projects";

vi.mock("../lib/api", () => ({ apiDelete: vi.fn(), apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn() }));

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  apiGet.mockReset().mockResolvedValue({
    items: [
      { id: 1, name: "First", slug: "first", target_count: 0 },
      { id: 2, name: "Second", slug: "second", target_count: 0 },
    ],
  });
  apiPost.mockReset();
  apiPut.mockReset();
  apiDelete.mockReset();
});

it("does not close a newer project draft when an earlier save finishes", async () => {
  const user = userEvent.setup();
  const pending = deferred();
  apiPost.mockReturnValueOnce(pending.promise);
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
  apiDelete.mockReturnValueOnce(pending.promise);
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

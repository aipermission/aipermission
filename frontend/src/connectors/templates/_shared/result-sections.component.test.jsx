import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { RawDataSection } from "./result-sections";

it("keeps raw-data search, highlighting, and copy controls in one frame", async () => {
  const user = userEvent.setup();
  const onSearch = vi.fn();
  render(<RawDataSection title="Example raw data" value={'{"status":"running"}'} search="run" onSearch={onSearch} />);
  expect(screen.getByRole("mark")).toHaveTextContent("run");
  await user.type(screen.getByPlaceholderText("Search raw data"), "x");
  expect(onSearch).toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument();
});

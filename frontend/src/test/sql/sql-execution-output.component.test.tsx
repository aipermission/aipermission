import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { downloadBlob, downloadJSON } from "../../lib/api";
import { SQLExecutionOutput } from "../../connectors/templates/_shared/sql-execution-output";
import { SQLConnectorConsole } from "../../connectors/templates/_shared/sql-console";
import { executionApproval, executionProps, setupSQLExecutionTransport } from "./execution-test-state";
import { readExportBlob } from "./export-blob";

vi.mock("../../lib/api", async (original) => ({
  ...(await original<typeof import("../../lib/api")>()),
  downloadBlob: vi.fn(),
  downloadJSON: vi.fn(),
}));
vi.mock("../../connectors/templates/_shared/sql-editor", () => ({ SQLEditor: () => <div /> }));
setupSQLExecutionTransport();
const value = {
  result_sets: [
    { columns: ["id"], rows: [{ id: 1 }], row_count: 1, max_rows: 10, truncated: false },
    { columns: ["label"], rows: [{ label: "=1+1" }, { label: "second" }], row_count: 2, max_rows: 10, truncated: true },
  ],
  commands: [
    { tag: "SELECT 1", reported_rows: "1" },
    { tag: "UPDATE", reported_rows: "9007199254740993" },
  ],
  notices: ["Notice from server"],
  row_count: 3,
  result_set_count: 2,
  completed_batches: 1,
  truncated: true,
  max_rows: 10,
  max_bytes: 1048576,
  retry_safe: false,
  transaction_control: "script_defined",
};

it("selects independent result sets and exports the selected rows through existing CSV/TSV/JSON controls", async () => {
  const user = userEvent.setup();
  const clipboard = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  render(<SQLExecutionOutput value={value} theme="light" filenamePrefix="execution" />);
  expect(screen.getByRole("columnheader", { name: "id" })).toBeVisible();
  expect(screen.getByRole("combobox", { name: "Result" })).toHaveValue("0");
  await user.selectOptions(screen.getByRole("combobox", { name: "Result" }), "1");
  expect(screen.getByRole("columnheader", { name: "label" })).toBeVisible();
  expect(screen.queryByRole("columnheader", { name: "id" })).not.toBeInTheDocument();
  await user.click(screen.getByTitle("Copy rows as TSV"));
  expect(clipboard).toHaveBeenLastCalledWith("label\n'=1+1\nsecond");
  await user.click(screen.getByTitle("Copy result JSON"));
  expect(clipboard).toHaveBeenLastCalledWith(JSON.stringify(value.result_sets[1], null, 2));
  await user.click(screen.getByTitle("Download rows as CSV"));
  const [blob, filename] = vi.mocked(downloadBlob).mock.calls.at(-1)!;
  expect(filename).toBe("execution-result-2.csv");
  expect(await readExportBlob(blob)).toBe("label\n'=1+1\nsecond");
  await user.click(screen.getByTitle("Download result JSON"));
  expect(downloadJSON).toHaveBeenLastCalledWith(value.result_sets[1], "execution-result-2.json");
});

it("shows returned counts separately from verbatim driver-reported command counts and exports full execution JSON", async () => {
  const user = userEvent.setup();
  const clipboard = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  render(<SQLExecutionOutput value={value} theme="dark" filenamePrefix="execution" />);
  expect(screen.getByText("3 rows returned")).toBeVisible();
  expect(screen.getByText("2 result sets")).toBeVisible();
  expect(screen.getByText("1 completed batch")).toBeVisible();
  expect(screen.getByText("Returned data truncated")).toBeVisible();
  expect(screen.getByText("Transactions: script_defined")).toBeVisible();
  expect(screen.getByText("Not retry-safe")).toBeVisible();
  await user.click(screen.getByText("Commands (2) / Notices (1)"));
  expect(screen.getByText("SELECT 1 / Reported rows: 1")).toBeVisible();
  expect(screen.getByText("UPDATE / Reported rows: 9007199254740993")).toBeVisible();
  expect(screen.getByText("Notice from server")).toBeVisible();
  expect(screen.queryByText(/affected/i)).not.toBeInTheDocument();
  await user.selectOptions(screen.getByRole("combobox", { name: "Result" }), "summary");
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  await user.click(screen.getByTitle("Copy JSON"));
  expect(clipboard).toHaveBeenLastCalledWith(JSON.stringify(value, null, 2));
  await user.click(screen.getByTitle("Download JSON"));
  expect(downloadJSON).toHaveBeenLastCalledWith(value, "execution.json");
});

it("keeps command-only and empty result sets inspectable without inventing affected rows", async () => {
  const { rerender } = render(
    <SQLExecutionOutput value={{ ...value, result_sets: [], result_set_count: 0, row_count: 0 }} theme="dark" filenamePrefix="commands" />,
  );
  expect(screen.getByText("UPDATE / Reported rows: 9007199254740993")).toBeVisible();
  expect(screen.getByText("0 rows returned")).toBeVisible();
  expect(screen.getByRole("combobox", { name: "Result" })).toHaveValue("summary");
  rerender(
    <SQLExecutionOutput
      key="empty-set"
      value={{ ...value, result_sets: [{ columns: ["id"], rows: [], row_count: 0 }] }}
      theme="light"
      filenamePrefix="empty"
    />,
  );
  expect(screen.getByText("No rows returned.")).toBeVisible();
});

it("integrates execution output in request detail and resets the result selection when a different request is selected", async () => {
  const user = userEvent.setup();
  const first = executionApproval({ status: "completed", output: value, input: { sql: "SELECT 1; SELECT 2" } });
  const second = executionApproval({
    id: 82,
    status: "completed",
    reason: "Other execution",
    output: { ...value, result_sets: [value.result_sets[0]] },
  });
  const props = { ...executionProps, approvals: { state: "ready", data: [first, second] } };
  const { rerender } = render(<SQLConnectorConsole {...props} theme="dark" />);
  await user.click(screen.getByRole("switch", { name: "Result View" }));
  await user.selectOptions(screen.getByRole("combobox", { name: "Result" }), "1");
  expect(within(screen.getByRole("table")).getByRole("columnheader", { name: "label" })).toBeVisible();
  rerender(<SQLConnectorConsole {...props} approvals={{ state: "ready", data: [second] }} theme="dark" />);
  expect(screen.getByText("Request #82")).toBeVisible();
  expect(screen.getByRole("combobox", { name: "Result" })).toHaveValue("0");
  expect(screen.getByRole("columnheader", { name: "id" })).toBeVisible();
});

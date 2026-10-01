import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { downloadBlob, downloadJSON } from "../../lib/api";
import { ActivityBlock, SQLOutputBlock } from "../../connectors/templates/_shared/sql-result-output";

vi.mock("../../lib/api", () => ({ downloadBlob: vi.fn(), downloadJSON: vi.fn() }));

async function exportedCSV(): Promise<string> {
  const [blob] = vi.mocked(downloadBlob).mock.calls.at(-1) ?? [];
  if (!(blob instanceof Blob)) throw new Error("CSV download was not created");
  const reader = new FileReader();
  return new Promise<string>((resolve, reject) => {
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error);
    reader.readAsText(blob);
  });
}

it("renders ordered columns and exports escaped CSV with the connector filename", async () => {
  const user = userEvent.setup();
  const value = { columns: ["name", "count", "extra"], rows: [{ name: 'A,"B"', count: null, extra: { enabled: true } }] };
  render(<SQLOutputBlock title="Rows" value={value} theme="dark" filenamePrefix="query-result" />);

  const table = screen.getByRole("table");
  expect(
    within(table)
      .getAllByRole("columnheader")
      .map((header) => header.textContent),
  ).toEqual(["name", "count", "extra"]);
  expect(within(table).getByText("NULL")).toBeVisible();
  expect(within(table).getByText('{"enabled":true}')).toBeVisible();

  await user.click(screen.getByTitle("Download rows as CSV"));
  expect(downloadBlob).toHaveBeenCalledWith(expect.any(Blob), "query-result.csv");
  const text = await exportedCSV();
  expect(text).toBe('name,count,extra\n"A,""B""",NULL,"{""enabled"":true}"');

  await user.click(screen.getByTitle("Download result JSON"));
  expect(downloadJSON).toHaveBeenCalledWith(value, "query-result.json");
});

it("copies rows as TSV without allowing embedded tabs to add columns", async () => {
  const user = userEvent.setup();
  const clipboard = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  render(<SQLOutputBlock title="Rows" value={{ columns: ["label"], rows: [{ label: "a\tb" }] }} theme="light" filenamePrefix="rows" />);

  await user.click(screen.getByTitle("Copy rows as TSV"));
  expect(clipboard).toHaveBeenCalledWith("label\na b");
});

it("shows empty results and tolerates malformed row values", () => {
  const { rerender } = render(<SQLOutputBlock title="Rows" value={{ columns: ["id"], rows: [] }} theme="light" filenamePrefix="rows" />);
  expect(screen.getByText("No rows returned.")).toBeVisible();

  rerender(<SQLOutputBlock title="Rows" value={{ columns: ["id"], rows: [null, 9] }} theme="dark" filenamePrefix="rows" />);
  expect(screen.getAllByRole("cell")).toHaveLength(2);
  expect(screen.getAllByRole("cell").every((cell) => cell.textContent === "NULL")).toBe(true);
});

it("falls back to raw JSON when no tabular columns are present", async () => {
  const user = userEvent.setup();
  const value = { message: "No table payload" };
  render(<SQLOutputBlock title="Output" value={value} theme="dark" filenamePrefix="raw" />);

  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  expect(screen.getByText(/No table payload/)).toBeVisible();
  await user.click(screen.getByTitle("Download JSON"));
  expect(downloadJSON).toHaveBeenCalledWith(value, "raw.json");
});

it("renders raw strings and bounds non-serializable input", () => {
  const { rerender } = render(<ActivityBlock title="Input" value="SELECT 1" />);
  expect(screen.getByText("SELECT 1")).toBeVisible();

  const cycle: { self?: unknown } = {};
  cycle.self = cycle;
  rerender(<ActivityBlock title="Input" value={cycle} />);
  expect(screen.getByText("[object Object]")).toBeVisible();
});

it("normalizes encoded rows for table exports while preserving the raw JSON payload", async () => {
  const user = userEvent.setup();
  const clipboard = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const value = { columns: ["id"], rows: ['{"id":42}', "invalid JSON"] };
  render(<SQLOutputBlock title="Rows" value={value} theme="dark" filenamePrefix="encoded" />);

  expect(screen.getAllByRole("cell").map((cell) => cell.textContent)).toEqual(["42", "NULL"]);
  await user.click(screen.getByTitle("Copy rows as TSV"));
  expect(clipboard).toHaveBeenCalledWith("id\n42\nNULL");
  await user.click(screen.getByTitle("Copy result JSON"));
  expect(clipboard).toHaveBeenCalledWith(JSON.stringify(value, null, 2));
  await user.click(screen.getByTitle("Download rows as CSV"));
  expect(downloadBlob).toHaveBeenCalledWith(expect.any(Blob), "encoded.csv");
  expect(await exportedCSV()).toBe("id\n42\nNULL");
  await user.click(screen.getByTitle("Download result JSON"));
  expect(downloadJSON).toHaveBeenCalledWith(value, "encoded.json");
});

it("keeps circular object cells bounded when rendering and exporting rows", async () => {
  const user = userEvent.setup();
  const clipboard = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const cycle: { self?: unknown } = {};
  cycle.self = cycle;
  render(<SQLOutputBlock title="Rows" value={{ columns: ["data"], rows: [{ data: cycle }] }} theme="dark" filenamePrefix="bounded" />);

  expect(screen.getByRole("cell")).toHaveTextContent("[object Object]");
  await user.click(screen.getByTitle("Copy rows as TSV"));
  expect(clipboard).toHaveBeenCalledWith("data\n[object Object]");
  await user.click(screen.getByTitle("Download rows as CSV"));
  expect(downloadBlob).toHaveBeenCalledWith(expect.any(Blob), "bounded.csv");
  expect(await exportedCSV()).toBe("data\n[object Object]");
});

it("keeps binary hex cells unchanged in rendering, clipboard, and downloads", async () => {
  const user = userEvent.setup();
  const clipboard = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const value = { columns: ["binary", "empty", "missing"], rows: [{ binary: "\\xff0041", empty: "\\x", missing: null }] };
  render(<SQLOutputBlock title="Rows" value={value} theme="dark" filenamePrefix="binary" />);

  expect(screen.getAllByRole("cell").map((cell) => cell.textContent)).toEqual(["\\xff0041", "\\x", "NULL"]);
  await user.click(screen.getByTitle("Copy rows as TSV"));
  expect(clipboard).toHaveBeenCalledWith("binary\tempty\tmissing\n\\xff0041\t\\x\tNULL");
  await user.click(screen.getByTitle("Copy result JSON"));
  expect(clipboard).toHaveBeenCalledWith(JSON.stringify(value, null, 2));
  await user.click(screen.getByTitle("Download rows as CSV"));
  expect(await exportedCSV()).toBe("binary,empty,missing\n\\xff0041,\\x,NULL");
  await user.click(screen.getByTitle("Download result JSON"));
  expect(downloadJSON).toHaveBeenCalledWith(value, "binary.json");
});

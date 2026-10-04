import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { SQLOutputBlock } from "../../connectors/templates/_shared/sql-result-output";
import { readExportBlob } from "./export-blob";

it.each(["object", "encoded"])("preserves exact SQL numeric strings through real %s row consumers", async (representation) => {
  const user = userEvent.setup();
  const clipboard = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const downloads: Blob[] = [];
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL(blob: Blob) {
        downloads.push(blob);
        return `blob:sql-export-${downloads.length}`;
      }
      static revokeObjectURL() {}
    },
  );
  const clickedDownloads: { filename: string; href: string }[] = [];
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click");
  click.mockImplementation(() => {
    const anchor = click.mock.contexts.at(-1);
    if (!(anchor instanceof HTMLAnchorElement)) throw new Error("Download was not an anchor click");
    clickedDownloads.push({ filename: anchor.download, href: anchor.href });
  });
  const row = {
    unsafe_integer: "9007199254740993",
    maximum_int64: "9223372036854775807",
    minimum_int64: "-9223372036854775808",
    exact_decimal: "123456789012345.678901234567890123",
  };
  const value = { columns: Object.keys(row), rows: [representation === "encoded" ? JSON.stringify(row) : row] };
  const expectedCells = Object.values(row);
  const expectedJSON = JSON.stringify(value, null, 2);
  render(<SQLOutputBlock title="Rows" value={value} theme="dark" filenamePrefix="exact-numbers" />);

  expect(screen.getAllByRole("cell").map((cell) => cell.textContent)).toEqual(expectedCells);
  await user.click(screen.getByTitle("Copy rows as TSV"));
  // Negative strings remain text in spreadsheets; JSON retains the original value.
  expect(clipboard).toHaveBeenLastCalledWith(
    "unsafe_integer\tmaximum_int64\tminimum_int64\texact_decimal\n9007199254740993\t9223372036854775807\t'-9223372036854775808\t123456789012345.678901234567890123",
  );
  await user.click(screen.getByTitle("Copy result JSON"));
  expect(clipboard).toHaveBeenLastCalledWith(expectedJSON);

  await user.click(screen.getByTitle("Download rows as CSV"));
  expect(downloads).toHaveLength(1);
  expect(downloads[0].type).toBe("text/csv");
  expect(await readExportBlob(downloads[0])).toBe(
    "unsafe_integer,maximum_int64,minimum_int64,exact_decimal\n9007199254740993,9223372036854775807,'-9223372036854775808,123456789012345.678901234567890123",
  );
  expect(clickedDownloads).toEqual([{ filename: "exact-numbers.csv", href: "blob:sql-export-1" }]);

  await user.click(screen.getByTitle("Download result JSON"));
  expect(downloads).toHaveLength(2);
  expect(downloads[1].type).toBe("application/json");
  expect(await readExportBlob(downloads[1])).toBe(expectedJSON);
  expect(clickedDownloads).toEqual([
    { filename: "exact-numbers.csv", href: "blob:sql-export-1" },
    { filename: "exact-numbers.json", href: "blob:sql-export-2" },
  ]);
});

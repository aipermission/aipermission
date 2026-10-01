import { expect, it } from "vitest";
import { connectorActionsResponse } from "./connector-catalog-contract";
import { connectorActionMaximumInputBytes } from "./generated-connector-contract";

it.each([1, 1024, connectorActionMaximumInputBytes])("preserves valid action input limit %s", (max_input_bytes) => {
  const action = { name: "inspect", max_input_bytes };
  expect(connectorActionsResponse({ items: [action] })).toEqual([action]);
});

it.each([0, -1, 1.5, "1024", null, NaN, Infinity, connectorActionMaximumInputBytes + 1])(
  "rejects malformed action input limit %s",
  (max_input_bytes) => {
    expect(() => connectorActionsResponse({ items: [{ name: "inspect", max_input_bytes }] })).toThrow("Invalid connector action");
  },
);

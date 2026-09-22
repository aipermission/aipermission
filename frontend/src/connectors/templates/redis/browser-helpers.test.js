import assert from "node:assert/strict";
import test from "node:test";
import { valueToEditableText } from "./browser-helpers.js";

test("Redis string drafts preserve exact JSON number and whitespace text", () => {
  const values = [
    '{"id":9007199254740993,"enabled":true}',
    '{"id":-9007199254740993,"amount":1.0000000000000001}',
    '{ "exponent": 1e+30, "quoted": "9007199254740993" }',
    "plain text",
  ];

  for (const value of values) assert.equal(valueToEditableText({ type: "string", value }), value);
});

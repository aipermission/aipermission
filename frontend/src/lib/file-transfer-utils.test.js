import assert from "node:assert/strict";
import test from "node:test";

import { fileTransferFailureText, transferProgress } from "./file-transfer-utils.js";

test("transferProgress treats canceled terminal queue items as processed", () => {
  const progress = transferProgress({
    status: "canceled",
    size_bytes: 100,
    transferred_bytes: 40,
  });

  assert.equal(progress.percent, 100);
  assert.equal(progress.label, "40 B / 100 B");
});

test("transferProgress keeps running queues byte-based", () => {
  const progress = transferProgress({
    status: "running",
    size_bytes: 100,
    transferred_bytes: 40,
  });

  assert.equal(progress.percent, 40);
});

test("file transfer outcome uncertainty warns before retry", () => {
  assert.match(
    fileTransferFailureText({ failure_kind: "outcome_unknown", error: "internal detail" }),
    /Inspect the destination before retrying/,
  );
  assert.equal(fileTransferFailureText({ failure_kind: "timeout", error: "timed out" }), "timed out");
});

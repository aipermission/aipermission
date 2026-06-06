import assert from "node:assert/strict";
import test from "node:test";

import { transferProgress } from "./file-transfer-utils.js";

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

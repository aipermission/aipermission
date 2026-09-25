import assert from "node:assert/strict";
import test from "node:test";
import { fileSHA256 } from "../file-digest.ts";

test("fileSHA256 hashes file bytes without exposing the content", async () => {
  const digest = await fileSHA256(new Blob(["abc"]));
  assert.equal(digest, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
});

test("fileSHA256 rejects an already canceled read", async () => {
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(fileSHA256(new Blob(["abc"]), controller.signal), { name: "AbortError" });
});

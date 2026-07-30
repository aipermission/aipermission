import assert from "node:assert/strict";
import test from "node:test";

import { scopedUICookieName } from "./ui-cookie.js";

test("scopes UI cookie names by frontend port", () => {
  assert.equal(scopedUICookieName("aipermission_csrf", { protocol: "http:", port: "3210" }), "aipermission_csrf_3210");
  assert.equal(scopedUICookieName("aipermission_csrf", { protocol: "http:", port: "3212" }), "aipermission_csrf_3212");
});

test("uses the standard protocol port when location omits it", () => {
  assert.equal(scopedUICookieName("aipermission_csrf", { protocol: "http:", port: "" }), "aipermission_csrf_80");
  assert.equal(scopedUICookieName("aipermission_csrf", { protocol: "https:", port: "" }), "aipermission_csrf_443");
});

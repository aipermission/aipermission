#!/usr/bin/env node

const assert = require("node:assert/strict");

const {
  compareSemver,
  renderChangelog,
  renderFrontend,
  validateSource,
  wrapBullet,
} = require("../release-notes.js");

const source = {
  inAppReleaseLimit: 1,
  unreleased: [],
  releases: [
    {
      version: "1.2.3",
      date: "2026-08-26",
      label: "Stable notes",
      sections: [{ title: "Fixed", items: ["One canonical release note."] }],
    },
  ],
};

const validate = (releases = source.releases, expected = "1.2.3") =>
  validateSource({ ...source, releases }, expected);

validate();
assert.match(renderChangelog(source), /## \[1\.2\.3\] - 2026-08-26/);
assert.match(renderFrontend(source, "1.2.3"), /"Stable notes"/);
assert.equal(wrapBullet("short item"), "- short item");
assert.ok(compareSemver("1.2.3", "1.2.3-rc.1") > 0);
assert.ok(compareSemver("1.10.0", "1.9.0") > 0);
assert.throws(
  () => validate([...source.releases, source.releases[0]]),
  /duplicate release version/,
);
assert.throws(
  () =>
    validate([source.releases[0], { ...source.releases[0], version: "1.2.4" }]),
  /newest to oldest/,
);
assert.throws(
  () => validate(undefined, "1.2.4"),
  /does not match release manifest/,
);

console.log("Release notes generator tests passed.");

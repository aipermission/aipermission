#!/usr/bin/env node

const { execFileSync } = require("node:child_process");
const { createNativeRuntimeSuite } = require("./native-runtime-tests");

const suite = createNativeRuntimeSuite({
  cgoEnabled: "1",
  label: "macOS",
  nodePlatform: "darwin",
  policyPlatform: "darwin",
  requiredTestsKey: "darwinRuntimeTests",
  resolveEnvironment: () => {
    const prefix = execFileSync("brew", ["--prefix", "openssl@3"], {
      encoding: "utf8",
    }).trim();
    return {
      CGO_CFLAGS: `-I${prefix}/include`,
      CGO_LDFLAGS: `-L${prefix}/lib`,
    };
  },
});

if (require.main === module) suite.run();

module.exports = suite;

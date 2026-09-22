#!/usr/bin/env node

const { createNativeRuntimeSuite } = require("./native-runtime-tests");

const suite = createNativeRuntimeSuite({
  cgoEnabled: "0",
  label: "Windows",
  nodePlatform: "win32",
  policyPlatform: "windows",
  requiredTestsKey: "windowsRuntimeTests",
});

if (require.main === module) suite.run();

module.exports = {
  ...suite,
  assertWindowsPlatform: suite.assertNativePlatform,
};

const path = require("node:path");
const { check } = require("../dependency-license-check");
const { checkPatchedDriver } = require("./patched-driver");

// The source verifier and reviewed inventory jointly bind the actual go.mod
// requirements/replacement, source snapshot, original checksums and licenses.
const root = path.resolve(__dirname, "../..");
check(root);
process.stdout.write(JSON.stringify(checkPatchedDriver(root)) + "\n");

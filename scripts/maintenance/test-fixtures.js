const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const policy = require("../../maintenance-policy.json");

const copyPolicy = () => structuredClone(policy);

function writeFixtureFiles(root, files) {
  for (const [name, contents] of Object.entries(files)) {
    const destination = path.join(root, name);
    fs.mkdirSync(path.dirname(destination), { recursive: true });
    if (typeof contents === "string") fs.writeFileSync(destination, contents);
    else fs.copyFileSync(contents.copyFrom, destination);
  }
}

function temporaryRoot(t, files = {}) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "aipermission-maintenance-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  writeFixtureFiles(root, files);
  return root;
}

module.exports = { copyPolicy, temporaryRoot, writeFixtureFiles };

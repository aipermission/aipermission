const fs = require("node:fs");
const path = require("node:path");

function workflowFiles(root) {
  const roots = [
    path.join(root, ".github", "workflows"),
    path.join(root, ".github", "actions"),
  ];
  const files = [];
  const visit = (directory) => {
    if (!fs.existsSync(directory)) return;
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const absolute = path.join(directory, entry.name);
      if (entry.isDirectory()) visit(absolute);
      else if (/\.ya?ml$/i.test(entry.name)) files.push(absolute);
    }
  };
  roots.forEach(visit);
  return files.sort();
}

function workflowManifestFiles(root) {
  return fs
    .readdirSync(path.join(root, ".github", "workflows"), {
      withFileTypes: true,
    })
    .filter((entry) => entry.isFile() && /\.ya?ml$/i.test(entry.name))
    .map((entry) => path.join(root, ".github", "workflows", entry.name))
    .sort();
}

module.exports = { workflowFiles, workflowManifestFiles };

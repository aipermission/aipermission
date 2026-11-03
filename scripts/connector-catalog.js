#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const templatesDir = path.join(root, "frontend/src/connectors/templates");
const outputPath = path.join(root, "docs/connectors.md");
const readmePath = path.join(root, "README.md");
const overviewStart = "<!-- connector-overview:start -->";
const overviewEnd = "<!-- connector-overview:end -->";

function readConnectorMetadata() {
  return fs
    .readdirSync(templatesDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && !entry.name.startsWith("_"))
    .map((entry) => {
      const metadataPath = path.join(templatesDir, entry.name, "metadata.json");
      if (!fs.existsSync(metadataPath)) {
        throw new Error(`${entry.name} is missing metadata.json`);
      }
      const metadata = JSON.parse(fs.readFileSync(metadataPath, "utf8"));
      for (const field of ["kind", "label", "summary", "version"]) {
        if (typeof metadata[field] !== "string" || !metadata[field].trim()) {
          throw new Error(`${entry.name} metadata requires ${field}`);
        }
      }
      if (metadata.kind !== entry.name) {
        throw new Error(
          `${entry.name} metadata kind must match its template directory`,
        );
      }
      return metadata;
    })
    .sort((left, right) => left.label.localeCompare(right.label, "en"));
}

function tableCell(value) {
  return String(value).replaceAll("|", "\\|").replaceAll("\n", " ").trim();
}

function renderCatalog(connectors) {
  const lines = [
    "# Built-In Connectors",
    "",
    "<!-- Generated from frontend connector metadata. Do not edit directly. -->",
    "",
    "All built-in connectors use the same project, target, credential profile,",
    "token action permission, approval, history, and audit pipeline. Their",
    "connector packages own protocol-specific validation and execution; frontend",
    "templates own connector-specific forms and activity surfaces.",
    "",
    "| Connector | Kind | Contract | Summary |",
    "| --- | --- | --- | --- |",
  ];
  for (const connector of connectors) {
    lines.push(
      `| ${tableCell(connector.label)} | \`${tableCell(connector.kind)}\` | ${tableCell(connector.version)} | ${tableCell(connector.summary)} |`,
    );
  }
  lines.push(
    "",
    "Connector contract versions describe the in-project connector surface; they",
    "are not package or AIPermission release versions.",
    "",
    "For implementation rules, see [Add A Connector](development/add-a-connector.md).",
    "For setup-specific guidance, use the [documentation index](index.md).",
    "",
  );
  return lines.join("\n");
}

function renderReadmeOverview(connectors) {
  const names = connectors.map(({ label }) => {
    const escaped = label
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;");
    return `<strong>${escaped}</strong>`;
  });
  const rows = [];
  for (let index = 0; index < names.length; index += 4) {
    rows.push(names.slice(index, index + 4).join(" &middot; "));
  }
  return rows.join(" <br />\n");
}

function updateReadme(source, connectors) {
  const start = source.indexOf(overviewStart);
  const end = source.indexOf(overviewEnd);
  if (
    source.split(overviewStart).length !== 2 ||
    source.split(overviewEnd).length !== 2 ||
    start >= end
  ) {
    throw new Error(
      "README.md requires one ordered connector-overview marker pair",
    );
  }
  return (
    source.slice(0, start + overviewStart.length) +
    "\n" +
    renderReadmeOverview(connectors) +
    "\n" +
    source.slice(end)
  );
}

function expectedDocuments() {
  const connectors = readConnectorMetadata();
  return [
    [outputPath, renderCatalog(connectors)],
    [readmePath, updateReadme(fs.readFileSync(readmePath, "utf8"), connectors)],
  ];
}

function writeCatalog() {
  for (const [file, expected] of expectedDocuments()) {
    fs.writeFileSync(file, expected);
    console.log(`Updated ${path.relative(root, file)}.`);
  }
}

function checkCatalog() {
  for (const [file, expected] of expectedDocuments()) {
    const current = fs.existsSync(file) ? fs.readFileSync(file, "utf8") : "";
    if (current !== expected) {
      throw new Error(
        `${path.relative(root, file)} is stale; run npm run connector-catalog`,
      );
    }
  }
  console.log("Generated connector catalog and README overview are current.");
}

try {
  if (process.argv[2] === "--check") {
    checkCatalog();
  } else {
    writeCatalog();
  }
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}

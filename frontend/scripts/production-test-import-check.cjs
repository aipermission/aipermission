#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { parse } = require("espree");
const ts = require("typescript");

const { isTestSource } = require("../../scripts/maintenance-source-kind");

function walk(directory) {
  if (!fs.existsSync(directory)) return [];
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    if (entry.isDirectory() && entry.name === "node_modules") return [];
    const entryPath = path.join(directory, entry.name);
    return entry.isDirectory() ? walk(entryPath) : [entryPath];
  });
}

function visit(node, callback) {
  if (!node || typeof node !== "object") return;
  callback(node);
  for (const value of Object.values(node)) {
    if (Array.isArray(value)) value.forEach((item) => visit(item, callback));
    else if (value && typeof value === "object") visit(value, callback);
  }
}

function staticString(node) {
  if (typeof node?.value === "string") return node.value;
  if (node?.type === "TemplateLiteral" && node.expressions.length === 0) {
    return node.quasis[0]?.value?.cooked ?? node.quasis[0]?.value?.raw ?? "";
  }
  return null;
}

function dynamicNonLocalSpecifier(node) {
  if (node?.type !== "TemplateLiteral" || node.expressions.length === 0) {
    return false;
  }
  const prefix = node.quasis[0]?.value?.cooked ?? node.quasis[0]?.value?.raw ?? "";
  return /^(?:data:|https?:|node:)/.test(prefix);
}

function staticModuleSpecifiers(source, filename = "source.js") {
  const jsSource = /\.(?:ts|tsx|mts|cts)$/.test(filename)
    ? ts.transpileModule(source, {
        fileName: filename,
        compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.Preserve },
      }).outputText
    : source;
  const program = parse(jsSource, {
    ecmaVersion: "latest",
    sourceType: "module",
    ecmaFeatures: { jsx: true },
  });
  const specifiers = [];
  visit(program, (node) => {
    if (
      (node.type === "ImportDeclaration" || node.type === "ExportAllDeclaration" || node.type === "ExportNamedDeclaration") &&
      staticString(node.source) !== null
    ) {
      specifiers.push(staticString(node.source));
    }
    if (node.type === "ImportExpression") {
      const specifier = staticString(node.source);
      if (specifier !== null) specifiers.push(specifier);
      else if (!dynamicNonLocalSpecifier(node.source)) {
        throw new Error("dynamic import specifier cannot be verified");
      }
    }
    if (
      node.type === "CallExpression" &&
      node.callee?.type === "Identifier" &&
      node.callee.name === "require" &&
      node.arguments.length === 1
    ) {
      const specifier = staticString(node.arguments[0]);
      if (specifier === null) {
        throw new Error("dynamic require specifier cannot be verified");
      }
      specifiers.push(specifier);
    }
  });
  return specifiers;
}

function resolveLocalImport(importer, specifier, files, extensions) {
  if (!specifier.startsWith(".")) return null;
  const base = path.resolve(path.dirname(importer), specifier);
  const candidates = [base];
  for (const extension of extensions) candidates.push(base + extension);
  for (const extension of extensions) candidates.push(path.join(base, "index" + extension));
  return candidates.find((candidate) => files.has(candidate)) || null;
}

function analyzeProductionTestImports(root, policy) {
  const markers = policy.frontendArchitecture.testModuleMarkers;
  const entries = new Map();
  const extensions = new Set();
  for (const budget of policy.sourceBudgets) {
    if (budget.classifier === "go") continue;
    for (const extension of budget.extensions) extensions.add(extension);
    const directory = path.join(root, budget.directory);
    for (const file of walk(directory)) {
      if (!budget.extensions.includes(path.extname(file))) continue;
      entries.set(path.resolve(file), {
        test: isTestSource(budget.classifier, file, markers),
      });
    }
  }

  const failures = [];
  const files = new Set(entries.keys());
  for (const [file, metadata] of entries) {
    if (metadata.test) continue;
    let specifiers;
    try {
      specifiers = staticModuleSpecifiers(fs.readFileSync(file, "utf8"), file);
    } catch (error) {
      failures.push(`${path.relative(root, file)} cannot be parsed for import ownership: ${error.message}`);
      continue;
    }
    for (const specifier of specifiers) {
      const dependency = resolveLocalImport(file, specifier, files, extensions);
      if (dependency && entries.get(dependency)?.test) {
        failures.push(`${path.relative(root, file)} imports test support ${path.relative(root, dependency)}`);
      }
    }
  }
  return failures.sort();
}

function main() {
  const root = path.resolve(__dirname, "../..");
  const policy = require("../../maintenance-policy.json");
  const failures = analyzeProductionTestImports(root, policy);
  if (failures.length > 0) {
    console.error("Production/test import ownership check failed:");
    failures.forEach((failure) => console.error(`- ${failure}`));
    process.exit(1);
  }
  console.log("Production modules do not import test-only source files.");
}

if (require.main === module) main();

module.exports = { analyzeProductionTestImports, staticModuleSpecifiers };

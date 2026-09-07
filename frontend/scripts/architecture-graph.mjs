import { globSync, readFileSync, readdirSync } from "node:fs";
import { dirname, extname, join, relative, resolve, sep } from "node:path";

import { parse } from "espree";

const architecturePolicy = JSON.parse(readFileSync(new URL("../architecture-policy.json", import.meta.url), "utf8"));

const sourceExtensions = Object.freeze([".js", ".jsx"]);
const genericRoots = Object.freeze(["components", "lib", "pages"]);

export function analyzeSourceTree(sourceRoot, options = {}) {
  const importBudget = options.importBudget ?? architecturePolicy.maxDependencyFanout;
  const lineBudget = options.lineBudget ?? architecturePolicy.maxProductionModuleLines;
  const files = sourceFiles(sourceRoot);
  const fileSet = new Set(files);
  const graph = new Map();
  const failures = [];
  const connectorKinds = connectorTemplateKinds(sourceRoot);

  for (const file of files) {
    const source = readFileSync(file, "utf8");
    const lineCount = source.endsWith("\n") ? source.split("\n").length - 1 : source.split("\n").length;
    if (lineCount > lineBudget) {
      failures.push(`${displayPath(sourceRoot, file)} has ${lineCount} lines; budget is ${lineBudget}`);
    }
    let parsed;
    try {
      parsed = parseModule(source);
    } catch (error) {
      failures.push(`${displayPath(sourceRoot, file)} cannot be parsed: ${error.message}`);
      continue;
    }
    const specifiers = moduleSpecifiers(parsed);
    const globSpecifiers = moduleGlobSpecifiers(parsed);
    const dependencies = [
      ...specifiers.map((specifier) => resolveSourceImport(file, specifier, fileSet)).filter(Boolean),
      ...globSpecifiers.flatMap((specifier) => resolveSourceGlob(file, specifier, fileSet)),
    ];
    const importFanout = new Set([...specifiers.filter((specifier) => !specifier.startsWith(".")), ...dependencies]);
    if (importFanout.size > importBudget) {
      failures.push(`${displayPath(sourceRoot, file)} imports ${importFanout.size} modules; budget is ${importBudget}`);
    }
    graph.set(file, [...new Set(dependencies)]);
    failures.push(...boundaryFailures({ sourceRoot, file, dependencies, connectorKinds }));
    if (isConnectorAgnosticModule(sourceRoot, file, connectorKinds)) {
      for (const kind of hardCodedConnectorKinds(parsed, connectorKinds)) {
        failures.push(`${displayPath(sourceRoot, file)} hard-codes connector kind ${kind}; use the template registry`);
      }
    }
  }

  for (const cycle of dependencyCycles(graph)) {
    failures.push(`dependency cycle: ${cycle.map((file) => displayPath(sourceRoot, file)).join(" -> ")}`);
  }

  return { failures: [...new Set(failures)].sort(), files, graph, importBudget, lineBudget };
}

export function parseModule(source) {
  return parse(source, {
    ecmaVersion: "latest",
    sourceType: "module",
    ecmaFeatures: { jsx: true },
  });
}

export function moduleSpecifiers(sourceOrProgram) {
  const program = typeof sourceOrProgram === "string" ? parseModule(sourceOrProgram) : sourceOrProgram;
  const specifiers = [];
  walk(program, (node) => {
    if (node.type === "ImportDeclaration" || node.type === "ExportAllDeclaration" || node.type === "ExportNamedDeclaration") {
      if (typeof node.source?.value === "string") specifiers.push(node.source.value);
    }
    if (node.type === "ImportExpression" && typeof node.source?.value === "string") specifiers.push(node.source.value);
  });
  return specifiers;
}

export function moduleGlobSpecifiers(sourceOrProgram) {
  const program = typeof sourceOrProgram === "string" ? parseModule(sourceOrProgram) : sourceOrProgram;
  const specifiers = [];
  walk(program, (node) => {
    if (node.type !== "CallExpression" || !isImportMetaGlob(node.callee)) return;
    const candidate = node.arguments[0];
    if (candidate?.type === "Literal" && typeof candidate.value === "string") {
      specifiers.push(candidate.value);
    } else if (candidate?.type === "ArrayExpression") {
      for (const element of candidate.elements) {
        if (element?.type === "Literal" && typeof element.value === "string") specifiers.push(element.value);
      }
    }
  });
  return specifiers;
}

function isImportMetaGlob(node) {
  if (node?.type !== "MemberExpression" || node.computed) return false;
  return (
    node.property?.type === "Identifier" &&
    node.property.name === "glob" &&
    node.object?.type === "MetaProperty" &&
    node.object.meta?.name === "import" &&
    node.object.property?.name === "meta"
  );
}

export function hardCodedConnectorKinds(sourceOrProgram, connectorKinds) {
  const program = typeof sourceOrProgram === "string" ? parseModule(sourceOrProgram) : sourceOrProgram;
  const kinds = new Set(connectorKinds);
  const found = new Set();
  walk(program, (node) => {
    if (node.type === "BinaryExpression" && ["===", "!==", "==", "!="].includes(node.operator)) {
      collectComparedKind(node.left, node.right, kinds, found);
      collectComparedKind(node.right, node.left, kinds, found);
    }
    if (node.type === "SwitchStatement" && isConnectorKindReference(node.discriminant)) {
      for (const switchCase of node.cases) collectKindLiteral(switchCase.test, kinds, found);
    }
    if (
      node.type === "VariableDeclarator" &&
      node.id.type === "Identifier" &&
      /connector|kind|template/i.test(node.id.name) &&
      node.init?.type === "ObjectExpression"
    ) {
      for (const property of node.init.properties) {
        if (property.type !== "Property" || property.computed) continue;
        if (property.key.type === "Identifier" && kinds.has(property.key.name)) found.add(property.key.name);
        collectKindLiteral(property.key, kinds, found);
      }
    }
  });
  return [...found].sort();
}

function collectComparedKind(reference, candidate, kinds, found) {
  if (isConnectorKindReference(reference)) collectKindLiteral(candidate, kinds, found);
}

function collectKindLiteral(node, kinds, found) {
  if (node?.type === "Literal" && typeof node.value === "string" && kinds.has(node.value)) found.add(node.value);
}

function isConnectorKindReference(node) {
  if (node?.type === "Identifier") return ["connectorKind", "connector_kind", "kind"].includes(node.name);
  if (node?.type !== "MemberExpression") return false;
  const property = node.computed ? node.property?.value : node.property?.name;
  return property === "connectorKind" || property === "connector_kind";
}

export function dependencyCycles(graph) {
  const state = new Map();
  const stack = [];
  const cycles = [];
  const signatures = new Set();

  function visit(file) {
    state.set(file, "visiting");
    stack.push(file);
    for (const dependency of graph.get(file) || []) {
      if (!graph.has(dependency)) continue;
      if (state.get(dependency) === "visiting") {
        const cycle = [...stack.slice(stack.indexOf(dependency)), dependency];
        const signature = canonicalCycle(cycle);
        if (!signatures.has(signature)) {
          signatures.add(signature);
          cycles.push(cycle);
        }
      } else if (!state.has(dependency)) {
        visit(dependency);
      }
    }
    stack.pop();
    state.set(file, "visited");
  }

  for (const file of [...graph.keys()].sort()) {
    if (!state.has(file)) visit(file);
  }
  return cycles;
}

function boundaryFailures({ sourceRoot, file, dependencies, connectorKinds }) {
  const failures = [];
  const sourceLayer = moduleLayer(sourceRoot, file, connectorKinds);
  for (const dependency of dependencies) {
    const targetLayer = moduleLayer(sourceRoot, dependency, connectorKinds);
    const reason = forbiddenDependency(sourceLayer, targetLayer);
    if (reason) {
      failures.push(`${displayPath(sourceRoot, file)} imports ${displayPath(sourceRoot, dependency)} across forbidden boundary: ${reason}`);
    }
  }
  return failures;
}

function forbiddenDependency(sourceLayer, targetLayer) {
  const targetsConnectorTemplate = targetLayer.startsWith("connector-template:");
  if (sourceLayer === "lib" && (["components", "pages", "connector-editor"].includes(targetLayer) || targetsConnectorTemplate)) {
    return "lib must remain below UI and connector implementations";
  }
  if (sourceLayer === "components" && (["pages", "connector-editor"].includes(targetLayer) || targetsConnectorTemplate)) {
    return "shared components may use connector registry surfaces, not connector implementations or pages";
  }
  if (sourceLayer === "connector-editor" && targetsConnectorTemplate) {
    return "connector editor must use registry surfaces instead of concrete templates";
  }
  if (sourceLayer === "connector-shared" && targetsConnectorTemplate) {
    return "shared connector code must not depend on a concrete template";
  }
  if (sourceLayer === "pages" && targetsConnectorTemplate) {
    return "pages must use connector registry surfaces instead of concrete templates";
  }
  if (sourceLayer.startsWith("connector-template:") && ["pages", "connector-editor", "connector-registry"].includes(targetLayer)) {
    return "connector templates must not depend on pages, editor orchestration, or their registry";
  }
  if (sourceLayer.startsWith("connector-template:") && targetLayer.startsWith("connector-template:") && sourceLayer !== targetLayer) {
    return "connector templates must not import sibling connector internals";
  }
  return "";
}

function moduleLayer(sourceRoot, file, connectorKinds) {
  const path = displayPath(sourceRoot, file);
  const first = path.split("/")[0];
  if (genericRoots.includes(first)) return first;
  if (path.startsWith("connectors/editor/")) return "connector-editor";
  if (path.startsWith("connectors/templates/_shared/")) return "connector-shared";
  for (const kind of connectorKinds) {
    if (path.startsWith(`connectors/templates/${kind}/`)) return `connector-template:${kind}`;
  }
  if (["connectors/templates/catalog.js", "connectors/templates/registry.jsx"].includes(path)) return "connector-registry";
  if (path.startsWith("connectors/templates/")) return "connector-shared";
  return "other";
}

function isConnectorAgnosticModule(sourceRoot, file, connectorKinds) {
  return !moduleLayer(sourceRoot, file, connectorKinds).startsWith("connector-template:");
}

function connectorTemplateKinds(sourceRoot) {
  const root = join(sourceRoot, "connectors", "templates");
  return readdirSync(root, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && !entry.name.startsWith("_"))
    .map((entry) => entry.name)
    .sort();
}

function resolveSourceImport(importer, specifier, fileSet) {
  if (!specifier.startsWith(".")) return null;
  const base = resolve(dirname(importer), specifier);
  for (const candidate of [
    base,
    ...sourceExtensions.map((extension) => `${base}${extension}`),
    ...sourceExtensions.map((extension) => join(base, `index${extension}`)),
  ]) {
    if (fileSet.has(candidate)) return candidate;
  }
  return null;
}

function resolveSourceGlob(importer, specifier, fileSet) {
  if (!specifier.startsWith(".")) return [];
  return globSync(specifier, { cwd: dirname(importer) })
    .map((match) => resolve(dirname(importer), match))
    .filter((match) => fileSet.has(match));
}

function sourceFiles(directory) {
  return readdirSync(directory, { withFileTypes: true })
    .flatMap((entry) => {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) return sourceFiles(path);
      if (!sourceExtensions.includes(extname(entry.name)) || entry.name.includes(".test.")) return [];
      return [resolve(path)];
    })
    .sort();
}

function displayPath(sourceRoot, file) {
  return relative(sourceRoot, file).split(sep).join("/");
}

function canonicalCycle(cycle) {
  const nodes = cycle.slice(0, -1);
  return nodes.map((_, index) => [...nodes.slice(index), ...nodes.slice(0, index)].join("\0")).sort()[0];
}

function walk(value, visitor, parent = null) {
  if (!value || typeof value !== "object") return;
  if (typeof value.type === "string") visitor(value, parent);
  for (const [key, child] of Object.entries(value)) {
    if (["loc", "range", "tokens", "comments", "parent"].includes(key)) continue;
    if (Array.isArray(child)) child.forEach((item) => walk(item, visitor, value));
    else if (child && typeof child === "object") walk(child, visitor, value);
  }
}

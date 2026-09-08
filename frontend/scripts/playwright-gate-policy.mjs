import { parseModule } from "./architecture-graph.mjs";

export function forbiddenPlaywrightAnnotations(source) {
  const program = parseModule(source);
  const violations = [];
  walk(program, (node) => {
    if (node.type !== "CallExpression") return;
    const path = calleePath(node.callee);
    const segments = path.split(".");
    const kind = segments.at(-1);
    if (segments.length >= 2 && segments[0] === "test" && ["skip", "fixme", "fail"].includes(kind)) {
      violations.push({ kind, line: node.loc?.start?.line || 0 });
    }
  });
  return violations;
}

export function assertPlaywrightListing(report, requiredTitles, label) {
  const specs = collectSpecs(report?.suites || []);
  const titles = specs.map((spec) => spec.title).sort();
  const required = [...requiredTitles].sort();
  if (JSON.stringify(titles) !== JSON.stringify(required)) {
    const missing = required.filter((title) => !titles.includes(title));
    const unexpected = titles.filter((title) => !required.includes(title));
    throw new Error(
      `${label} test manifest mismatch (missing: ${missing.join(" | ") || "none"}; unexpected: ${unexpected.join(" | ") || "none"})`,
    );
  }
  for (const spec of specs) {
    if (!spec.ok) throw new Error(`${label} test is not runnable: ${spec.title}`);
    for (const item of spec.tests || []) {
      if ((item.annotations || []).length > 0) throw new Error(`${label} test has annotations: ${spec.title}`);
      if (item.expectedStatus !== "passed") {
        throw new Error(`${label} test has expected status ${item.expectedStatus}: ${spec.title}`);
      }
    }
  }
}

export function assertPlaywrightManifestRatchet(baseManifest, currentManifest) {
  const failures = [];
  for (const [suite, baseTitles] of Object.entries(baseManifest)) {
    const currentTitles = new Set(currentManifest[suite] || []);
    for (const title of baseTitles) {
      if (!currentTitles.has(title)) failures.push(`${suite}: ${title}`);
    }
  }
  if (failures.length > 0) throw new Error(`Playwright manifest ratchet removed required scenarios: ${failures.join(" | ")}`);
}

function collectSpecs(suites) {
  return suites.flatMap((suite) => [...(suite.specs || []), ...collectSpecs(suite.suites || [])]);
}

function calleePath(node) {
  const segments = [];
  let current = node;
  while (current?.type === "MemberExpression") {
    const property = current.computed && current.property?.type === "Literal" ? current.property.value : current.property?.name;
    if (typeof property !== "string") return "";
    segments.push(property);
    current = current.object;
  }
  if (current?.type !== "Identifier") return "";
  segments.push(current.name);
  return segments.reverse().join(".");
}

function walk(value, visitor) {
  const pending = [value];
  while (pending.length > 0) {
    const current = pending.pop();
    if (!current || typeof current !== "object") continue;
    if (typeof current.type === "string") visitor(current);
    for (const [key, child] of Object.entries(current)) {
      if (["loc", "range", "tokens", "comments", "parent"].includes(key)) continue;
      if (Array.isArray(child)) pending.push(...child);
      else if (child && typeof child === "object") pending.push(child);
    }
  }
}

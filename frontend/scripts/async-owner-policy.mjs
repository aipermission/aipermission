import { parseModule } from "./architecture-graph.mjs";

const asyncCalls = new Set(["createPollGenerationGuard", "createRequestGuard", "setInterval", "useAsyncAction", "useRequestGuard"]);
const asyncConstructors = new Set(["AbortController", "WebSocket"]);

export function isAsyncStateOwner(sourceOrProgram) {
  const program = typeof sourceOrProgram === "string" ? parseModule(sourceOrProgram) : sourceOrProgram;
  let found = false;
  const asyncAliases = importedAsyncAliases(program);
  walk(program, (node) => {
    if (found) return;
    if (node.type === "CallExpression") {
      const name = calledName(node.callee);
      if (
        asyncCalls.has(name) ||
        asyncAliases.has(name) ||
        (name === "setTimeout" && schedulesAsyncWork(node.arguments[0])) ||
        (["begin", "invalidate"].includes(name) && requestGuardReceiver(node.callee))
      )
        found = true;
    }
    if (node.type === "NewExpression" && asyncConstructors.has(calledName(node.callee))) found = true;
    if (node.type === "UpdateExpression" && isAsyncGenerationReference(node.argument)) found = true;
    if (node.type === "AssignmentExpression" && isAsyncGenerationReference(node.left)) found = true;
  });
  return found;
}

function importedAsyncAliases(program) {
  const aliases = new Set();
  for (const node of program.body || []) {
    if (node.type !== "ImportDeclaration") continue;
    for (const specifier of node.specifiers || []) {
      const imported = specifier.imported?.name;
      if (asyncCalls.has(imported)) aliases.add(specifier.local.name);
    }
  }
  return aliases;
}

function schedulesAsyncWork(callback) {
  if (callback?.type === "Identifier") return /^(?:fetch|load|poll|refresh|request)/i.test(callback.name);
  if (callback?.async) return true;
  let found = false;
  walk(callback, (node) => {
    if (
      node.type === "AwaitExpression" ||
      (node.type === "CallExpression" && /^(?:api|fetch|load|poll|refresh|request)/i.test(calledName(node.callee)))
    )
      found = true;
  });
  return found;
}

function calledName(callee) {
  if (callee?.type === "Identifier") return callee.name;
  if (callee?.type !== "MemberExpression") return "";
  return callee.computed ? String(callee.property?.value || "") : callee.property?.name || "";
}

function requestGuardReceiver(callee) {
  if (callee?.type !== "MemberExpression") return false;
  return /request|guard/i.test(referenceName(callee.object));
}

function isAsyncGenerationReference(node) {
  const name = referenceName(node);
  return /(?:request|generation)/i.test(name) && /\.current$/i.test(name);
}

function referenceName(node) {
  if (node?.type === "Identifier") return node.name;
  if (node?.type !== "MemberExpression") return "";
  const property = node.computed ? node.property?.value : node.property?.name;
  return `${referenceName(node.object)}.${String(property || "")}`;
}

function walk(value, visitor) {
  if (!value || typeof value !== "object") return;
  if (typeof value.type === "string") visitor(value);
  for (const [key, child] of Object.entries(value)) {
    if (["loc", "range", "tokens", "comments", "parent"].includes(key)) continue;
    if (Array.isArray(child)) child.forEach((item) => walk(item, visitor));
    else if (child && typeof child === "object") walk(child, visitor);
  }
}

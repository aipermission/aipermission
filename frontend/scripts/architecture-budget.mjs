import { readFileSync, readdirSync } from "node:fs";
import { extname, join, relative } from "node:path";

const sourceRoot = join(process.cwd(), "src");
const importBudget = 20;
const failures = [];

for (const file of sourceFiles(sourceRoot)) {
  const source = readFileSync(file, "utf8");
  const imports = new Set(
    Array.from(source.matchAll(/(?:import\s+(?:[^"']+?\s+from\s+)?|import\s*\()["']([^"']+)["']/g), (match) => match[1]),
  );
  if (imports.size > importBudget) {
    failures.push(`${relative(process.cwd(), file)} imports ${imports.size} modules; budget is ${importBudget}`);
  }
}

if (failures.length > 0) {
  console.error("Frontend architecture budget failed:");
  failures.forEach((failure) => console.error(`- ${failure}`));
  process.exit(1);
}

console.log(`Frontend architecture budget passed: dependency fan-out is at most ${importBudget}.`);

function sourceFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    if (![".js", ".jsx"].includes(extname(entry.name)) || entry.name.includes(".test.")) return [];
    return [path];
  });
}

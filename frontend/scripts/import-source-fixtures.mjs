import fixtures from "./import-fixtures.cjs";
import { analyzeSourceTree } from "./architecture-graph.mjs";

export const { data, createFixture } = fixtures;

export function createSourceFixture(context, name, values) {
  const fixture = createFixture(context, name, values);
  const result = analyzeSourceTree(fixture.sourceRoot || fixture.root, data.trees[name].options);
  return { ...fixture, result };
}

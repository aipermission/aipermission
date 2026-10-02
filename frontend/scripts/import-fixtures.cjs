const { mkdirSync, mkdtempSync, rmSync, writeFileSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { dirname, join } = require("node:path");
const data = require("./import-fixtures.json");

function createFixture(context, name, values = {}) {
  const definition = data.trees[name];
  const base = data.trees[definition.extends] || {};
  const fixture = { ...base, ...definition, files: { ...base.files, ...definition.files } };
  const root = mkdtempSync(join(tmpdir(), `aipermission-import-${name}-`));
  context.after(() => rmSync(root, { recursive: true, force: true }));
  for (const directory of fixture.directories || []) mkdirSync(join(root, directory), { recursive: true });
  const substitute = (source) =>
    source.replace(/\{\{(\w+)\}\}/g, (_, key) => {
      if (!Object.hasOwn(values, key)) throw new Error(`Missing fixture parameter: ${name}.${key}`);
      return values[key];
    });
  for (const [filename, content] of Object.entries(fixture.files)) {
    const path = join(root, substitute(filename));
    const template = typeof content === "string" ? content : content.source.repeat(content.repeat);
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, substitute(template));
  }
  const paths = Object.fromEntries(Object.entries(fixture.paths || {}).map(([key, path]) => [key, join(root, path)]));
  return { root, ...paths };
}

module.exports = { data, createFixture };

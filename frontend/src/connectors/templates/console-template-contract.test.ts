import assert from "node:assert/strict";
import test from "node:test";
import { resolve } from "node:path";
import ts from "typescript";

test("requires both native console slots to accept the real shared props without erasing their inferred types", () => {
  const filename = resolve("src/test/virtual-console-template-contract.tsx");
  const source = `
    import type { ConsoleTemplateContract } from "../connectors/templates/console-template-contract";
    import type { ConsoleWorkspaceSlotProps, ConsoleToolbarSlotProps } from "../components/console/console-workspace-types";
    const console = (_props: ConsoleWorkspaceSlotProps) => null;
    const toolbar = (_props: ConsoleToolbarSlotProps) => null;
    const valid = { Console: console, ToolbarActions: toolbar, model: { owned: true } } satisfies ConsoleTemplateContract;
    const preservesModelInference: boolean = valid.model.owned;
    const invalidConsole = { Console: (_props: { unavailableConsoleField: string }) => null } satisfies ConsoleTemplateContract;
    const invalidToolbar = { Console: console, ToolbarActions: (_props: { unavailableToolbarField: string }) => null } satisfies ConsoleTemplateContract;
  `;
  const config = ts.readConfigFile(resolve("tsconfig.json"), ts.sys.readFile);
  assert.equal(config.error, undefined);
  const options = ts.parseJsonConfigFileContent(config.config, ts.sys, process.cwd()).options;
  const host = ts.createCompilerHost(options);
  const originalSource = host.getSourceFile.bind(host);
  host.getSourceFile = (path, languageVersion, onError, shouldCreateNewSourceFile) =>
    path === filename
      ? ts.createSourceFile(filename, source, languageVersion, true, ts.ScriptKind.TSX)
      : originalSource(path, languageVersion, onError, shouldCreateNewSourceFile);
  const program = ts.createProgram([filename], options, host);
  const diagnostics = ts.getPreEmitDiagnostics(program).filter((item) => item.file?.fileName === filename);
  assert.deepEqual(
    diagnostics.map((item) => item.code),
    [2322, 2322],
  );
  assert.match(ts.flattenDiagnosticMessageText(diagnostics[0].messageText, "\n"), /unavailableConsoleField/);
  assert.match(ts.flattenDiagnosticMessageText(diagnostics[1].messageText, "\n"), /unavailableToolbarField/);
});

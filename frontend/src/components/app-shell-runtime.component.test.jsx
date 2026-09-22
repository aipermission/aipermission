import { describe, expect, it, vi } from "vitest";

vi.mock("../lib/api.js", () => ({
  apiUrl: "https://localhost:3210",
  currentWorkspaceBinding: vi.fn(() => "workspace/a"),
}));

import { consoleSessionAttachUrl, liveConsoleRuntimeTargets } from "./app-shell-runtime.js";

describe("app shell runtime", () => {
  it("binds secure console sockets to the encoded workspace identity", () => {
    expect(consoleSessionAttachUrl(7)).toBe("wss://localhost:3210/api/console/sessions/7/attach?workspace=workspace%2Fa");
  });

  it("keeps only connector targets with a live runtime projection", () => {
    const models = {
      live: {
        usesLiveConsole: ({ target }) => target.enabled,
        liveConsoleRuntimeTarget: ({ target }) => ({ id: target.runtime_id }),
      },
      static: { usesLiveConsole: () => false },
    };

    expect(
      liveConsoleRuntimeTargets(
        [
          { connector_kind: "live", runtime_id: 11, enabled: true },
          { connector_kind: "live", runtime_id: 12, enabled: false },
          { connector_kind: "static", runtime_id: 13, enabled: true },
          { connector_kind: "live", enabled: true },
        ],
        (kind) => models[kind],
      ),
    ).toEqual([{ id: 11 }]);
    expect(liveConsoleRuntimeTargets(null, () => null)).toEqual([]);
  });
});

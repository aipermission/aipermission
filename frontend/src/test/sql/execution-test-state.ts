import { beforeEach, vi } from "vitest";
import { connectorActionFixture, connectorApprovalFixture } from "../connector-action-fixtures";
import { mutationTestWorkspace, setupMutationRetryStorage } from "../connector-mutation-test-state";
import type { SQLConsoleProps } from "../../connectors/templates/_shared/use-sql-console";

export const executionProps: SQLConsoleProps = {
  config: { label: "Test SQL", executionAction: "execute_sql", metadataAction: "get_metadata", metadataInput: {} },
  target: { ref: "sql-test:1:1", name: "Test database" },
  approvals: { state: "ready", data: [] },
  session: { active: true, startedAt: "2026-01-01T12:00:00Z" },
};

export function executionResponse(overrides: Parameters<typeof connectorActionFixture>[0] = {}) {
  return connectorActionFixture({
    request_id: 81,
    target_ref: executionProps.target.ref,
    action_name: "execute_sql",
    ...overrides,
  });
}

export function executionApproval(overrides: Parameters<typeof connectorApprovalFixture>[0] = {}) {
  return connectorApprovalFixture({
    id: 81,
    target_ref: executionProps.target.ref,
    action_name: "execute_sql",
    created_at: "2026-01-01T12:01:00Z",
    ...overrides,
  });
}

export function sqlResponse(value: unknown) {
  return new Response(JSON.stringify(value), {
    headers: { "Content-Type": "application/json", "X-AIPermission-Workspace": mutationTestWorkspace },
  });
}

export function setupSQLExecutionTransport() {
  setupMutationRetryStorage();
  const execute = vi.fn<(_body: Record<string, unknown>) => Promise<unknown>>();
  const discover = vi.fn<(_url: string) => Promise<unknown>>();
  const posts = vi.fn();
  beforeEach(() => {
    posts.mockReset();
    execute.mockReset().mockResolvedValue(executionResponse({ output: { result_sets: [], commands: [], notices: [] } }));
    discover.mockReset().mockResolvedValue([]);
    vi.stubGlobal("fetch", async (url: string, options?: RequestInit) => {
      if (options?.method !== "POST") return sqlResponse(await discover(url));
      const body = JSON.parse(String(options.body));
      posts(body, options);
      if (body.action_name === "execute_sql") return sqlResponse(await execute(body));
      return sqlResponse(
        executionResponse({
          target_ref: body.target_ref,
          action_name: body.action_name,
          output: { columns: [], rows: [] },
        }),
      );
    });
  });
  return { execute, discover, posts };
}

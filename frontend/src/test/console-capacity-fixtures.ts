import type { Report, Session } from "../components/capacity/model";

export function session(id = 1, age = 60, state = "connected"): Session {
  return {
    id,
    runtime_id: id,
    generation: 1,
    target_name: `Target ${id}`,
    connector_kind: "docker",
    name: `Shell ${id}`,
    owner: "mcp_token:1",
    state,
    clients: 1,
    last_activity: new Date(Date.parse("2026-11-08T12:00:00Z") - age * 60_000).toISOString(),
    can_close: state === "connected",
  };
}

export function report(): Report {
  return {
    limit: 32,
    used: 3,
    available: 29,
    revision: 1,
    observed_at: "2026-11-08T12:00:00Z",
    items: [session(), session(2, 2), session(3, 40, "running")],
  };
}

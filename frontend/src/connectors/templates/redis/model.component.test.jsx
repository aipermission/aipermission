import { describe, expect, it } from "vitest";
import { emptyForm, serverProductLabel, submitLabel, syncForm, targetEndpoint } from "./model";

describe("Redis connector model", () => {
  it("keeps direct and SSH-backed forms on the same normalized contract", () => {
    const direct = emptyForm();
    expect(syncForm({ form: { ...direct, transport_target_ref: "ssh:1:1" } })).toMatchObject({
      connection_mode: "direct",
      transport_target_ref: "",
    });

    const tunneled = syncForm({
      form: { ...direct, connection_mode: "over_ssh", transport_target_ref: "ssh:1:1" },
    });
    expect(tunneled.transport_target_ref).toBe("ssh:1:1");
    expect(targetEndpoint({ target: { config: tunneled } })).toBe("127.0.0.1:6379/0 · over ssh");
    expect(serverProductLabel({ server_family: "valkey" })).toBe("Valkey");
  });

  it("reports stable submit labels for connector editor state", () => {
    expect(submitLabel({ state: { state: "saving" }, mode: "create" })).toBe("Saving...");
    expect(submitLabel({ state: { state: "ready" }, mode: "edit" })).toBe("Save changes");
    expect(submitLabel({ state: { state: "ready" }, mode: "create" })).toBe("Create connector");
  });
});

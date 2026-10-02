export function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_error: unknown) => void;
  const promise = new Promise<unknown>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

export function scopeSnapshot(revision = "scope-1", enabled = true) {
  return { items: [{ project_id: 3, project_name: "My Project", project_slug: "my-project", enabled }], revision };
}

export function capabilitySnapshot(revision = "capability-1", rule = "", expiresAt: string | null = null) {
  return {
    definitions: [
      {
        name: "vault.inject",
        label: "Inject secrets",
        description: "Inject selected Vault values into a connector session.",
        allowed_rules: ["approval_required", "always_run"],
      },
    ],
    items: rule ? [{ project_id: 3, capability_name: "vault.inject", execution_rule: rule, expires_at: expiresAt }] : [],
    revision,
  };
}

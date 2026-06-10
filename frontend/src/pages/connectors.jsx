import { Database, Plus, RefreshCcw, ShieldCheck } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { apiGet, apiPost } from "../lib/api";
import { useAsyncAction } from "../lib/use-async-action";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Drawer } from "../components/ui/drawer";
import { Field, Input, Select } from "../components/ui/form";
import { Notice } from "../components/ui/notice";

const emptyPostgresForm = {
  name: "main-db",
  host: "127.0.0.1",
  port: 5432,
  database: "postgres",
  ssl_mode: "prefer",
  profile_label: "readonly",
  username: "",
  password: "",
  risk_label: "read-only",
};

export function ConnectorsPage() {
  const [connector, setConnector] = useState({ state: "loading", data: null, error: null });
  const [targets, setTargets] = useState({ state: "loading", data: [], error: null });
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [form, setForm] = useState(emptyPostgresForm);
  const { actionState, runAction } = useAsyncAction();

  const actions = useMemo(() => connector.data?.actions || [], [connector.data]);

  async function loadPostgresConnector() {
    try {
      const data = await apiGet("/api/connectors/postgres");
      setConnector({ state: "ready", data, error: null });
    } catch (error) {
      setConnector({ state: "error", data: null, error: error.message });
    }
  }

  async function loadTargets() {
    setTargets((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/connector-targets?kind=postgres");
      const items = await Promise.all((data.items || []).map((target) => apiGet(`/api/connector-targets/${target.id}`)));
      setTargets({ state: "ready", data: items, error: null });
    } catch (error) {
      setTargets({ state: "error", data: [], error: error.message });
    }
  }

  useEffect(() => {
    void loadPostgresConnector();
    void loadTargets();
  }, []);

  async function createPostgresTarget(event) {
    event.preventDefault();
    await runAction({
      pending: "saving",
      successMessage: "Postgres connector target created.",
      action: async () => {
        const target = await apiPost("/api/connector-targets", {
          connector_kind: "postgres",
          name: form.name,
          config: {
            connection_mode: "direct",
            host: form.host,
            port: Number(form.port) || 5432,
            database: form.database,
            ssl_mode: form.ssl_mode,
          },
        });
        await apiPost(`/api/connector-targets/${target.id}/profiles`, {
          kind: "username_password",
          label: form.profile_label,
          public: {
            username: form.username,
          },
          secret: {
            password: form.password,
          },
          risk_label: form.risk_label || "read-only",
        });
        setDrawerOpen(false);
        setForm(emptyPostgresForm);
        await loadTargets();
      },
    });
  }

  function updateForm(field, value) {
    setForm((current) => ({ ...current, [field]: value }));
  }

  return (
    <section className="mx-auto grid w-full max-w-7xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">Connectors</h3>
          <p className="text-sm text-stone-500">Configure local-first connector targets that AI tokens can use through AIPermission approval rules.</p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={loadTargets} disabled={targets.state === "loading"}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
          <Button type="button" onClick={() => setDrawerOpen(true)}>
            <Plus className="h-4 w-4" />
            Add Postgres target
          </Button>
        </div>
      </div>

      <Notice tone="warn">
        Postgres connector MVP supports direct connections only. Use a dedicated read-only database role for AI access, then grant actions per token from the Tokens page.
      </Notice>
      {connector.state === "error" ? <Notice tone="bad">{connector.error}</Notice> : null}
      {targets.state === "error" ? <Notice tone="bad">{targets.error}</Notice> : null}
      {actionState.message ? <Notice tone="good">{actionState.message}</Notice> : null}
      {actionState.state === "error" ? <Notice tone="bad">{actionState.error}</Notice> : null}

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
          <table className="w-full table-fixed border-collapse text-left text-sm">
            <thead className="bg-stone-50 text-xs uppercase text-stone-500">
              <tr>
                <th className="w-[24%] px-4 py-3 font-semibold">Target</th>
                <th className="w-[28%] px-4 py-3 font-semibold">Database</th>
                <th className="w-[28%] px-4 py-3 font-semibold">Credential profiles</th>
                <th className="w-[20%] px-4 py-3 font-semibold">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-stone-200">
              {targets.data.map((target) => (
                <tr key={target.id} className="hover:bg-stone-50">
                  <td className="px-4 py-4">
                    <div className="grid min-w-0 gap-1">
                      <span className="inline-flex min-w-0 items-center gap-2 font-semibold">
                        <Database className="h-4 w-4 shrink-0 text-stone-500" />
                        <span className="truncate">{target.name}</span>
                      </span>
                      <span className="font-mono text-xs text-stone-500">postgres:{target.id}</span>
                    </div>
                  </td>
                  <td className="px-4 py-4">
                    <div className="grid gap-1 text-xs text-stone-500">
                      <span className="truncate font-mono text-stone-800">
                        {target.config?.host}:{target.config?.port || 5432}
                      </span>
                      <span className="truncate">{target.config?.database || "-"}</span>
                      <span>SSL {target.config?.ssl_mode || "prefer"}</span>
                    </div>
                  </td>
                  <td className="px-4 py-4">
                    <div className="flex flex-wrap gap-1.5">
                      {(target.profiles || []).map((profile) => (
                        <Badge key={profile.id} tone="neutral" title={profile.ref}>
                          {profile.label}
                        </Badge>
                      ))}
                      {(target.profiles || []).length === 0 ? <span className="text-xs text-stone-500">No profiles</span> : null}
                    </div>
                  </td>
                  <td className="px-4 py-4">
                    <Badge tone={target.status === "active" ? "good" : "warn"}>{target.status}</Badge>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {targets.state === "ready" && targets.data.length === 0 ? (
            <div className="p-4">
              <Notice>Create a Postgres connector target to expose read-only database actions to AI clients.</Notice>
            </div>
          ) : null}
        </div>

        <aside className="grid content-start gap-4">
          <div className="rounded-lg border border-stone-200 bg-white p-4">
            <div className="flex items-center gap-2">
              <ShieldCheck className="h-4 w-4 text-emerald-700" />
              <h4 className="text-sm font-semibold text-stone-950">Postgres actions</h4>
            </div>
            <div className="mt-3 grid gap-2">
              {actions.map((action) => (
                <div key={action.name} className="rounded-md border border-stone-200 bg-stone-50 px-3 py-2 dark-panel-subtle">
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-mono text-xs font-semibold text-stone-900">{action.name}</span>
                    <Badge tone={action.risk === "read" ? "good" : "warn"}>{action.risk}</Badge>
                  </div>
                  <p className="mt-1 text-xs text-stone-500">{action.description}</p>
                </div>
              ))}
              {connector.state === "loading" ? <p className="text-sm text-stone-500">Loading actions...</p> : null}
            </div>
          </div>
        </aside>
      </div>

      <Drawer
        open={drawerOpen}
        title="Add Postgres target"
        description="Create a direct Postgres target and one credential profile."
        onClose={() => setDrawerOpen(false)}
      >
        <form className="grid gap-4" onSubmit={createPostgresTarget}>
          <Notice tone="good">Use the database role you want AI to have. AIPermission stores only the encrypted profile secret and never returns it from the API.</Notice>
          <Field>
            Target name
            <Input value={form.name} onChange={(event) => updateForm("name", event.target.value)} required />
          </Field>
          <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_120px]">
            <Field>
              Host
              <Input value={form.host} onChange={(event) => updateForm("host", event.target.value)} required />
            </Field>
            <Field>
              Port
              <Input type="number" min="1" max="65535" value={form.port} onChange={(event) => updateForm("port", event.target.value)} required />
            </Field>
          </div>
          <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_160px]">
            <Field>
              Database
              <Input value={form.database} onChange={(event) => updateForm("database", event.target.value)} required />
            </Field>
            <Field>
              SSL mode
              <Select value={form.ssl_mode} onChange={(event) => updateForm("ssl_mode", event.target.value)}>
                <option value="prefer">Prefer</option>
                <option value="require">Require</option>
                <option value="disable">Disable</option>
                <option value="verify_full">Verify full</option>
              </Select>
            </Field>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field>
              Profile label
              <Input value={form.profile_label} onChange={(event) => updateForm("profile_label", event.target.value)} required />
            </Field>
            <Field>
              Risk label
              <Input value={form.risk_label} onChange={(event) => updateForm("risk_label", event.target.value)} />
            </Field>
          </div>
          <Field>
            Username
            <Input value={form.username} onChange={(event) => updateForm("username", event.target.value)} autoComplete="off" required />
          </Field>
          <Field>
            Password
            <Input type="password" value={form.password} onChange={(event) => updateForm("password", event.target.value)} autoComplete="new-password" required />
          </Field>
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={() => setDrawerOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={actionState.state === "saving"}>
              {actionState.state === "saving" ? "Creating..." : "Create target"}
            </Button>
          </div>
        </form>
      </Drawer>
    </section>
  );
}

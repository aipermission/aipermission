import { CircleCheck, CircleX, Copy, Edit3, PlugZap, Plus, RefreshCcw, Server, ShieldCheck, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useGateway } from "../lib/gateway-context";
import { apiDelete, apiPost, apiPut } from "../lib/api";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { CopyButton } from "../components/ui/copy-button";
import { Dialog } from "../components/ui/dialog";
import { Drawer } from "../components/ui/drawer";
import { Checkbox, Field, Input, Select, Textarea } from "../components/ui/form";
import { Notice } from "../components/ui/notice";

const emptyForm = { name: "", host: "", port: 22, username: "root", ssh_key_id: "", description: "", setup_later: false };

export function ServersPage() {
  const { servers, sshKeys, loadServers } = useGateway();
  const [drawer, setDrawer] = useState({ open: false, mode: "create", server: null });
  const [deleteDialog, setDeleteDialog] = useState({ open: false, server: null });
  const [installDialog, setInstallDialog] = useState({ open: false, server: null });
  const [hostKeyDialog, setHostKeyDialog] = useState({ open: false, hostKey: null, action: null, state: "idle", error: null });
  const [form, setForm] = useState(emptyForm);
  const [state, setState] = useState({ state: "idle", error: null });
  const [tests, setTests] = useState({});

  const firstKeyID = useMemo(() => (sshKeys.data[0] ? String(sshKeys.data[0].id) : ""), [sshKeys.data]);
  const activeKey = useMemo(() => selectedSSHKey(sshKeys.data, form.ssh_key_id), [sshKeys.data, form.ssh_key_id]);

  useEffect(() => {
    if (!form.ssh_key_id && firstKeyID) {
      setForm((current) => ({ ...current, ssh_key_id: firstKeyID }));
    }
  }, [firstKeyID, form.ssh_key_id]);

  function openCreateDrawer() {
    setState({ state: "idle", error: null });
    setForm({ ...emptyForm, ssh_key_id: firstKeyID });
    setDrawer({ open: true, mode: "create", server: null });
  }

  function openEditDrawer(server) {
    setState({ state: "idle", error: null });
    setForm({
      name: server.name,
      host: server.host,
      port: server.port,
      username: server.username,
      ssh_key_id: String(server.ssh_key_id),
      description: server.description || "",
      setup_later: false,
    });
    setDrawer({ open: true, mode: "edit", server });
  }

  async function saveServer(event) {
    event.preventDefault();
    setState({ state: "saving", error: null });
    const payload = serverPayloadFromForm(form);
    try {
      if (!form.setup_later) {
        const test = await apiPost("/api/servers/test-connection", payload);
        if (!test.ok) {
          throw new Error(test.stderr || test.stdout || "SSH connection test failed. Paste the install command on the server first, or choose setup later.");
        }
      }
      if (drawer.mode === "edit") {
        await apiPut(`/api/servers/${drawer.server.id}`, payload);
      } else {
        await apiPost("/api/servers", payload);
      }
      await loadServers();
      setDrawer({ open: false, mode: "create", server: null });
      setForm({ ...emptyForm, ssh_key_id: form.ssh_key_id });
      setState({ state: "idle", error: null });
    } catch (error) {
      if (showHostKeyApproval(error, { type: "save", payload })) {
        setState({ state: "idle", error: null });
        return;
      }
      setState({ state: "error", error: error.message });
    }
  }

  async function deleteServer(removeKey) {
    if (!deleteDialog.server) return;
    setState({ state: "deleting", error: null });
    try {
      await apiDelete(`/api/servers/${deleteDialog.server.id}${removeKey ? "?remove_key=true" : ""}`);
      await loadServers();
      setDeleteDialog({ open: false, server: null });
      setState({ state: "idle", error: null });
    } catch (error) {
      setState({ state: "error", error: error.message });
    }
  }

  async function testServer(id) {
    setTests((current) => ({ ...current, [id]: { state: "testing", error: null, data: null } }));
    try {
      const data = await apiPost(`/api/servers/${id}/test`, {});
      setTests((current) => ({ ...current, [id]: { state: data.ok ? "ok" : "error", error: data.stderr || null, data } }));
    } catch (error) {
      if (showHostKeyApproval(error, { type: "test", serverID: id })) {
        setTests((current) => ({ ...current, [id]: { state: "idle", error: null, data: null } }));
        return;
      }
      setTests((current) => ({ ...current, [id]: { state: "error", error: error.message, data: null } }));
    }
  }

  function showHostKeyApproval(error, action) {
    if (error.status !== 409 || !["unknown_ssh_host_key", "changed_ssh_host_key"].includes(error.data?.code) || !error.data?.host_key) {
      return false;
    }
    setHostKeyDialog({ open: true, hostKey: error.data.host_key, action, state: "idle", error: null });
    return true;
  }

  async function approveHostKey() {
    const { hostKey, action } = hostKeyDialog;
    if (!hostKey || !action) return;
    setHostKeyDialog((current) => ({ ...current, state: "approving", error: null }));
    try {
      await apiPost("/api/ssh-host-keys/approve", {
        host: hostKey.host,
        port: hostKey.port,
        public_key: hostKey.public_key,
        replace: Boolean(hostKey.changed),
      });
      setHostKeyDialog({ open: false, hostKey: null, action: null, state: "idle", error: null });
      if (action.type === "test") {
        await testServer(action.serverID);
      } else if (action.type === "save") {
        setState({ state: "saving", error: null });
        const test = await apiPost("/api/servers/test-connection", action.payload);
        if (!test.ok) {
          throw new Error(test.stderr || test.stdout || "SSH connection test failed.");
        }
        if (drawer.mode === "edit") {
          await apiPut(`/api/servers/${drawer.server.id}`, action.payload);
        } else {
          await apiPost("/api/servers", action.payload);
        }
        await loadServers();
        setDrawer({ open: false, mode: "create", server: null });
        setForm({ ...emptyForm, ssh_key_id: form.ssh_key_id });
        setState({ state: "idle", error: null });
      }
    } catch (error) {
      setHostKeyDialog((current) => ({ ...current, state: "error", error: error.message }));
      if (action?.type === "save") {
        setState({ state: "error", error: error.message });
      }
    }
  }

  return (
    <section className="mx-auto grid w-full max-w-6xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">Servers</h3>
          <p className="text-sm text-stone-500">Remote SSH targets linked to named gateway keys.</p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={loadServers}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
          <Button type="button" onClick={openCreateDrawer} disabled={sshKeys.data.length === 0}>
            <Plus className="h-4 w-4" />
            Add server
          </Button>
        </div>
      </div>

      {sshKeys.data.length === 0 ? <Notice>Create an SSH key before adding servers.</Notice> : null}
      {servers.state === "error" ? <Notice tone="bad">{servers.error}</Notice> : null}

      <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
        <table className="w-full table-fixed border-collapse text-left text-sm">
          <thead className="bg-stone-50 text-xs uppercase text-stone-500">
            <tr>
              <th className="w-[18%] px-4 py-3 font-semibold">Name</th>
              <th className="w-[27%] px-4 py-3 font-semibold">Endpoint</th>
              <th className="w-[18%] px-4 py-3 font-semibold">SSH key</th>
              <th className="w-[17%] px-4 py-3 font-semibold">Status</th>
              <th className="w-[20%] px-4 py-3 text-right font-semibold">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-stone-200">
            {servers.data.map((server) => {
              const test = tests[server.id];
              return (
                <tr className="align-top" key={server.id}>
                  <td className="px-4 py-4">
                    <div className="flex min-w-0 items-center gap-2">
                        <Server className="h-4 w-4 shrink-0 text-stone-500" />
                      <span className="truncate font-semibold">{server.name}</span>
                    </div>
                    {server.description ? <p className="mt-1 truncate text-xs text-stone-500">{server.description}</p> : null}
                  </td>
                  <td className="px-4 py-4">
                    <span className="block truncate font-mono text-xs text-stone-600">
                      {server.username}@{server.host}:{server.port}
                    </span>
                  </td>
                  <td className="px-4 py-4">
                    <Badge>{server.ssh_key_name || `#${server.ssh_key_id}`}</Badge>
                  </td>
                  <td className="px-4 py-4">
                    {test?.state === "ok" ? (
                      <span className="flex items-center gap-2 text-sm text-emerald-800 dark-status-good">
                        <CircleCheck className="h-4 w-4" />
                        {test.data.duration_ms}ms
                      </span>
                    ) : null}
                    {test?.state === "testing" ? <span className="text-sm text-stone-500">Testing...</span> : null}
                    {test?.state === "error" ? (
                      <span className="flex items-center gap-2 text-sm text-red-800 dark-status-bad" title={test.error}>
                        <CircleX className="h-4 w-4 shrink-0" />
                        Failed
                      </span>
                    ) : null}
                    {!test ? <span className="text-sm text-stone-400">Not tested</span> : null}
                  </td>
                  <td className="px-4 py-4">
                    <div className="flex justify-end gap-2">
                      <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Test SSH" onClick={() => testServer(server.id)}>
                        <PlugZap className="h-4 w-4" />
                      </Button>
                      <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Install key" onClick={() => setInstallDialog({ open: true, server })}>
                        <Copy className="h-4 w-4" />
                      </Button>
                      <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Edit server" onClick={() => openEditDrawer(server)}>
                        <Edit3 className="h-4 w-4" />
                      </Button>
                      <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Delete server" onClick={() => setDeleteDialog({ open: true, server })}>
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        {servers.state === "ready" && servers.data.length === 0 ? (
          <div className="p-4">
            <Notice>No servers yet.</Notice>
          </div>
        ) : null}
      </div>

      <Drawer
        open={drawer.open}
        title={drawer.mode === "edit" ? `Edit ${drawer.server?.name}` : "Add server"}
        description="No SSH password. Select one of your named gateway keys."
        onClose={() => setDrawer({ open: false, mode: "create", server: null })}
      >
        <form className="grid min-w-0 gap-4" onSubmit={saveServer}>
          <Field>
            Name
            <Input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder="core-1" required />
          </Field>
          <div className="grid gap-4 md:grid-cols-[1fr_120px]">
            <Field>
              Host
              <Input value={form.host} onChange={(event) => setForm({ ...form, host: event.target.value })} placeholder="203.0.113.10" required />
            </Field>
            <Field>
              Port
              <Input type="number" min="1" max="65535" value={form.port} onChange={(event) => setForm({ ...form, port: event.target.value })} required />
            </Field>
          </div>
          <Field>
            Username
            <Input value={form.username} onChange={(event) => setForm({ ...form, username: event.target.value })} required />
          </Field>
          <Field>
            SSH key
            <Select value={form.ssh_key_id} onChange={(event) => setForm({ ...form, ssh_key_id: event.target.value })} required>
              <option value="" disabled>
                Select key
              </option>
              {sshKeys.data.map((key) => (
                <option value={key.id} key={key.id}>
                  {key.name} · {key.key_type}
                </option>
              ))}
            </Select>
          </Field>
          {activeKey ? (
            <InstallCommandPanel
              command={activeKey.install_command}
              title="Install this SSH key on the server"
              description="Copy this command, SSH into the server yourself, and paste it once. Save will test the connection before storing the server."
            />
          ) : null}
          <label className="flex items-start gap-3 rounded-md border border-stone-200 bg-stone-50 p-3 text-sm text-stone-700">
            <Checkbox
              checked={form.setup_later}
              onChange={(event) => setForm({ ...form, setup_later: event.target.checked })}
            />
            <span>
              <span className="block font-semibold text-stone-900">I will install the key later</span>
              <span className="mt-1 block text-xs text-stone-500">Save without testing the SSH connection.</span>
            </span>
          </label>
          <Field>
            Description
            <Textarea value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} rows={3} />
          </Field>
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
          <Button type="submit" disabled={state.state === "saving" || sshKeys.data.length === 0}>
            {state.state === "saving" ? (form.setup_later ? "Saving..." : "Testing...") : drawer.mode === "edit" ? "Save changes" : "Save server"}
          </Button>
        </form>
      </Drawer>

      <ServerInstallDialog
        value={installDialog}
        sshKeys={sshKeys.data}
        onClose={() => setInstallDialog({ open: false, server: null })}
      />

      <HostKeyApprovalDialog
        value={hostKeyDialog}
        onApprove={approveHostKey}
        onClose={() => setHostKeyDialog({ open: false, hostKey: null, action: null, state: "idle", error: null })}
      />

      <Dialog
        open={deleteDialog.open}
        title={deleteDialog.server ? `Uninstall ${deleteDialog.server.name}` : "Uninstall server"}
        description="Remove this server from aipermission. You can also remove the selected gateway public key from remote authorized_keys first."
        onClose={() => setDeleteDialog({ open: false, server: null })}
        size="md"
      >
        <div className="grid gap-4">
          {deleteDialog.server ? (
            <div className="rounded-md border border-stone-200 bg-stone-50 p-3 text-sm text-stone-700">
              <p className="font-semibold">{deleteDialog.server.username}@{deleteDialog.server.host}:{deleteDialog.server.port}</p>
              <p className="mt-1">SSH key: {deleteDialog.server.ssh_key_name || `#${deleteDialog.server.ssh_key_id}`}</p>
            </div>
          ) : null}
          <Notice tone="warn">
            Remote key cleanup connects to the server, removes entries containing the selected gateway public key blob from <span className="font-mono">~/.ssh/authorized_keys</span>, then deletes the local server record.
          </Notice>
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={() => deleteServer(false)} disabled={state.state === "deleting"}>
              Delete local only
            </Button>
            <Button type="button" onClick={() => deleteServer(true)} disabled={state.state === "deleting"}>
              {state.state === "deleting" ? "Uninstalling..." : "Remove key and delete"}
            </Button>
          </div>
        </div>
      </Dialog>
    </section>
  );
}

function HostKeyApprovalDialog({ value, onApprove, onClose }) {
  const hostKey = value.hostKey;
  const changed = Boolean(hostKey?.changed);
  return (
    <Dialog
      open={value.open}
      title={changed ? "SSH host fingerprint changed" : "Approve SSH host fingerprint"}
      description={changed ? "The server is sending a different identity than the one previously trusted." : "First connection requires you to trust the server identity."}
      onClose={onClose}
      size={changed ? "lg" : "md"}
    >
      {hostKey ? (
        <div className="grid gap-4">
          {changed ? (
            <Notice tone="bad">
              This can happen after a rebuild or IP reuse, but it can also indicate a man-in-the-middle attack. Verify the new fingerprint from your provider console before replacing the trusted key.
            </Notice>
          ) : (
            <Notice tone="warn">
              Verify this fingerprint from your provider console or from a trusted terminal before approving. AIPermission will reject future changes for this host.
            </Notice>
          )}
          <div className="grid gap-2 rounded-md border border-stone-200 bg-stone-50 p-3 text-sm">
            <div className="flex items-center gap-2 font-semibold text-stone-900">
              <ShieldCheck className="h-4 w-4 text-stone-500" />
              {hostKey.hostname}
            </div>
            <p className="text-xs text-stone-500">Type: {hostKey.key_type}</p>
            {changed && hostKey.existing_fingerprints?.length ? (
              <div className="grid gap-1">
                <p className="text-xs font-semibold uppercase text-stone-500">Previously trusted</p>
                {hostKey.existing_fingerprints.map((fingerprint) => (
                  <code className="break-all rounded bg-white p-2 text-xs text-stone-800" key={fingerprint}>{fingerprint}</code>
                ))}
              </div>
            ) : null}
            <p className="text-xs font-semibold uppercase text-stone-500">{changed ? "New fingerprint" : "Fingerprint"}</p>
            <code className="break-all rounded bg-white p-2 text-xs text-stone-800">{hostKey.fingerprint_sha256}</code>
          </div>
          {value.state === "error" ? <Notice tone="bad">{value.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={value.state === "approving"}>
              Cancel
            </Button>
            <Button type="button" className="whitespace-nowrap" onClick={onApprove} disabled={value.state === "approving"}>
              {value.state === "approving" ? "Approving..." : changed ? "Replace trusted fingerprint" : "Approve fingerprint"}
            </Button>
          </div>
        </div>
      ) : null}
    </Dialog>
  );
}

function ServerInstallDialog({ value, sshKeys, onClose }) {
  const server = value.server;
  const key = server ? selectedSSHKey(sshKeys, server.ssh_key_id) : null;
  return (
    <Dialog
      open={value.open}
      title={server ? `Install key for ${server.name}` : "Install SSH key"}
      description="Paste this command on the remote server before connecting with aipermission."
      onClose={onClose}
      size="md"
    >
      {server && key ? (
        <InstallCommandPanel
          command={key.install_command}
          title={`${server.username}@${server.host}:${server.port}`}
          description="Copy the command, connect to the server with your own terminal, paste it, then test the server from aipermission."
        />
      ) : (
        <Notice tone="bad">SSH key details are not loaded.</Notice>
      )}
    </Dialog>
  );
}

function InstallCommandPanel({ command, title, description }) {
  return (
    <div className="grid min-w-0 gap-3 overflow-hidden rounded-lg border border-stone-200 bg-white p-3">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-stone-900">{title}</p>
          <p className="mt-1 text-xs text-stone-500">{description}</p>
        </div>
        <CopyButton value={command} variant="outline" className="h-9 shrink-0 px-3" />
      </div>
      <pre className="max-h-44 max-w-full overflow-x-auto overflow-y-auto rounded-md bg-stone-950 p-3 text-xs leading-5 text-stone-50">
        <code>{command}</code>
      </pre>
    </div>
  );
}

function selectedSSHKey(keys, id) {
  return keys.find((key) => Number(key.id) === Number(id)) || null;
}

function serverPayloadFromForm(form) {
  return {
    name: form.name,
    host: form.host,
    port: Number(form.port),
    username: form.username,
    ssh_key_id: Number(form.ssh_key_id),
    description: form.description,
  };
}

import { CircleCheck, CircleX, Container, Copy, Edit3, FileText, Info, PlugZap, Plus, RefreshCcw, Server, ShieldCheck, Trash2, Upload } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useGateway } from "../lib/gateway-context";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { CopyButton } from "../components/ui/copy-button";
import { Dialog } from "../components/ui/dialog";
import { Drawer } from "../components/ui/drawer";
import { Checkbox, Field, Input, Select, Textarea } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { TerminalBlock } from "../components/ui/terminal-block";

const emptyForm = { name: "", host: "", port: 22, username: "root", ssh_key_id: "", description: "", setup_later: false };

export function ServersPage() {
  const { servers, sshKeys, loadServers } = useGateway();
  const [drawer, setDrawer] = useState({ open: false, mode: "create", server: null });
  const [deleteDialog, setDeleteDialog] = useState({ open: false, server: null });
  const [installDialog, setInstallDialog] = useState({ open: false, server: null });
  const [sshConfigDialog, setSSHConfigDialog] = useState({ open: false, state: "idle", items: [], error: null });
  const [sshConfigContent, setSSHConfigContent] = useState("");
  const [sshConfigResultSource, setSSHConfigResultSource] = useState(null);
  const [dockerDialog, setDockerDialog] = useState({ open: false, server: null, state: "idle", data: null, error: null });
  const [dockerLogsDialog, setDockerLogsDialog] = useState({ open: false, server: null, container: null, state: "idle", data: null, error: null });
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

  async function scanSSHConfig() {
    setSSHConfigDialog({ open: true, state: "loading", items: [], error: null });
    setSSHConfigResultSource("container");
    try {
      const data = await apiGet("/api/ssh-config/discover");
      setSSHConfigDialog({ open: true, state: "ready", items: data.items || [], error: null });
    } catch (error) {
      setSSHConfigDialog({ open: true, state: "error", items: [], error: error.message });
    }
  }

  async function parseSSHConfigContent(content) {
    setSSHConfigDialog({ open: true, state: "loading", items: [], error: null });
    setSSHConfigResultSource("local");
    try {
      const data = await apiPost("/api/ssh-config/parse", { content });
      setSSHConfigDialog({ open: true, state: "ready", items: data.items || [], error: null });
    } catch (error) {
      setSSHConfigDialog({ open: true, state: "error", items: [], error: error.message });
    }
  }

  async function parseSSHConfigFile(event) {
    const file = event.target.files?.[0];
    if (!file) return;
    try {
      const content = await file.text();
      setSSHConfigContent(content);
      await parseSSHConfigContent(content);
    } catch (error) {
      setSSHConfigDialog({ open: true, state: "error", items: [], error: error.message });
    } finally {
      event.target.value = "";
    }
  }

  function useSSHConfigEntry(entry) {
    setState({ state: "idle", error: null });
    setForm({
      ...emptyForm,
      name: entry.alias || entry.host,
      host: entry.host || entry.alias,
      port: entry.port || 22,
      username: entry.username || "root",
      ssh_key_id: firstKeyID,
      description: "Imported from host config.",
    });
    setSSHConfigDialog({ open: false, state: "idle", items: [], error: null });
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

  async function checkDocker(server) {
    setDockerDialog({ open: true, server, state: "loading", data: null, error: null });
    try {
      const data = await apiPost(`/api/servers/${server.id}/docker-check`, {});
      setDockerDialog({ open: true, server, state: "ready", data, error: null });
    } catch (error) {
      if (showHostKeyApproval(error, { type: "docker", server })) {
        setDockerDialog({ open: false, server: null, state: "idle", data: null, error: null });
        return;
      }
      setDockerDialog({ open: true, server, state: "error", data: null, error: error.message });
    }
  }

  async function readDockerLogs(server, container, tail = 300) {
    setDockerLogsDialog((current) => ({
      open: true,
      server,
      container,
      state: "loading",
      data: current.open && (current.container?.id || current.container?.name) === (container.id || container.name) ? current.data : null,
      error: null,
    }));
    try {
      const data = await apiPost(`/api/servers/${server.id}/docker-logs`, { container_ref: container.id || container.name, tail: Number(tail) || 300 });
      setDockerLogsDialog({ open: true, server, container, state: "ready", data, error: null });
    } catch (error) {
      if (showHostKeyApproval(error, { type: "docker-logs", server, container })) {
        setDockerLogsDialog({ open: false, server: null, container: null, state: "idle", data: null, error: null });
        return;
      }
      setDockerLogsDialog((current) => ({ open: true, server, container, state: "error", data: current.data, error: error.message }));
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
      } else if (action.type === "docker") {
        await checkDocker(action.server);
      } else if (action.type === "docker-logs") {
        await readDockerLogs(action.server, action.container);
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
          <Button type="button" variant="outline" onClick={() => setSSHConfigDialog({ open: true, state: "idle", items: [], error: null })}>
            <FileText className="h-4 w-4" />
            Import hosts
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
              <th className="w-[25%] px-4 py-3 font-semibold">Endpoint</th>
              <th className="w-[18%] px-4 py-3 font-semibold">SSH key</th>
              <th className="w-[15%] px-4 py-3 font-semibold">Status</th>
              <th className="w-[24%] px-4 py-3 text-right font-semibold">Actions</th>
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
                      <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Check Docker" onClick={() => checkDocker(server)}>
                        <Container className="h-4 w-4" />
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

      <SSHConfigDialog
        value={sshConfigDialog}
        hasSSHKeys={sshKeys.data.length > 0}
        onScan={scanSSHConfig}
        onParseFile={parseSSHConfigFile}
        content={sshConfigContent}
        resultSource={sshConfigResultSource}
        onContentChange={setSSHConfigContent}
        onParseContent={() => parseSSHConfigContent(sshConfigContent)}
        onUse={useSSHConfigEntry}
        onClose={() => {
          setSSHConfigDialog({ open: false, state: "idle", items: [], error: null });
          setSSHConfigContent("");
          setSSHConfigResultSource(null);
        }}
      />

      <HostKeyApprovalDialog
        value={hostKeyDialog}
        onApprove={approveHostKey}
        onClose={() => setHostKeyDialog({ open: false, hostKey: null, action: null, state: "idle", error: null })}
      />

      <DockerCheckDialog
        value={dockerDialog}
        onReadLogs={readDockerLogs}
        onClose={() => setDockerDialog({ open: false, server: null, state: "idle", data: null, error: null })}
      />
      <DockerLogsDialog
        value={dockerLogsDialog}
        onRefresh={readDockerLogs}
        onClose={() => setDockerLogsDialog({ open: false, server: null, container: null, state: "idle", data: null, error: null })}
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

function DockerCheckDialog({ value, onReadLogs, onClose }) {
  const data = value.data;
  const [detailContainer, setDetailContainer] = useState(null);
  return (
    <>
      <Dialog
        open={value.open}
        title={value.server ? `${value.server.name} Docker` : "Docker check"}
        description="On-demand Docker status from the selected server."
        onClose={onClose}
        size="xl"
      >
        <div className="grid gap-4">
          {value.state === "loading" ? <Notice>Checking Docker on the server...</Notice> : null}
          {value.state === "error" ? <Notice tone="bad">{value.error}</Notice> : null}
          {data && !data.available ? <Notice tone="warn">Docker is not installed or the docker command is not available on this server.</Notice> : null}
          {data?.available && !data.ok ? (
            <Notice tone="bad">
              Docker is available, but the status command failed. Check Docker daemon access, permissions, or service state on the server.
            </Notice>
          ) : null}
          {data?.available && data.ok ? (
            <div className="grid gap-3">
              <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-stone-200 bg-stone-50 px-3 py-2 text-sm">
                <span className="font-medium text-stone-800">
                  {data.containers?.length || 0} running container{data.containers?.length === 1 ? "" : "s"}
                </span>
                <span className="text-xs text-stone-500">
                  exit {data.exit_code} · {data.duration_ms}ms
                </span>
              </div>
              {data.containers?.length ? (
                <div className="max-h-[min(45vh,360px)] overflow-auto rounded-md border border-stone-200">
                  <table className="w-full table-fixed border-collapse text-left text-sm">
                    <thead className="sticky top-0 z-10 bg-stone-50 text-xs uppercase text-stone-500">
                      <tr>
                        <th className="w-[28%] px-3 py-2 font-semibold">Name</th>
                        <th className="w-[26%] px-3 py-2 font-semibold">Status</th>
                        <th className="w-[30%] px-3 py-2 font-semibold">Ports</th>
                        <th className="w-[16%] px-3 py-2 text-right font-semibold">Actions</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-stone-100">
                      {data.containers.map((container) => (
                        <tr key={container.id || container.name}>
                          <td className="truncate px-3 py-2 font-semibold text-stone-900">{container.name || container.id}</td>
                          <td className="truncate px-3 py-2 text-stone-700">{container.status || container.state}</td>
                          <td className="truncate px-3 py-2 font-mono text-xs text-stone-600">{container.ports || "-"}</td>
                          <td className="px-3 py-2">
                            <div className="flex justify-end gap-2">
                              <Button
                                type="button"
                                variant="outline"
                                className="h-8 w-8 px-0"
                                title="Details"
                                onClick={() => setDetailContainer(container)}
                              >
                                <Info className="h-4 w-4" />
                              </Button>
                              <Button
                                type="button"
                                variant="outline"
                                className="h-8 w-8 px-0"
                                title="Logs"
                                onClick={() => onReadLogs(value.server, container)}
                              >
                                <FileText className="h-4 w-4" />
                              </Button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <Notice>No running Docker containers.</Notice>
              )}
            </div>
          ) : null}
          {data?.stderr ? <TerminalBlock className="max-h-40 p-3">{data.stderr}</TerminalBlock> : null}
        </div>
      </Dialog>
      <DockerContainerDetailDialog container={detailContainer} onClose={() => setDetailContainer(null)} />
    </>
  );
}

function DockerContainerDetailDialog({ container, onClose }) {
  return (
    <Dialog
      open={Boolean(container)}
      title={container ? `${container.name || container.id} details` : "Container details"}
      description="Full Docker status fields from the latest on-demand check."
      onClose={onClose}
      size="wide"
    >
      {container ? (
        <div className="grid max-h-[min(70vh,620px)] gap-3 overflow-auto pr-1 sm:grid-cols-2 xl:grid-cols-3">
          <DockerDetailField label="Name" value={container.name} />
          <DockerDetailField label="ID" value={container.id} mono />
          <DockerDetailField label="Image" value={container.image} mono />
          <DockerDetailField label="State" value={container.state} />
          <DockerDetailField label="Status" value={container.status} />
          <DockerDetailField label="Running for" value={container.running_for} />
          <DockerDetailField label="Created at" value={container.created_at} />
          <DockerDetailField label="Size" value={container.size} />
          <DockerDetailField label="Ports" value={container.ports} mono wide />
          <DockerDetailField label="Command" value={container.command} mono wide />
          <DockerDetailField label="Networks" value={container.networks} mono />
          <DockerDetailField label="Mounts" value={container.mounts} mono />
          <DockerDetailField label="Labels" value={container.labels} mono wide />
        </div>
      ) : null}
    </Dialog>
  );
}

function DockerLogsDialog({ value, onRefresh, onClose }) {
  const [tail, setTail] = useState(300);
  const outputRef = useRef(null);
  const output = [value.data?.stdout, value.data?.stderr].filter(Boolean).join("\n\n");
  const canRefresh = Boolean(value.server && value.container) && value.state !== "loading";

  useEffect(() => {
    if (value.open) {
      setTail(300);
    }
  }, [value.open, value.container?.id, value.container?.name]);

  useEffect(() => {
    if (value.state === "ready" || value.state === "loading") {
      window.setTimeout(() => {
        const node = outputRef.current;
        if (node) node.scrollTop = node.scrollHeight;
      }, 0);
    }
  }, [value.state, output]);

  function refreshLogs(event) {
    event?.preventDefault();
    if (!canRefresh) return;
    const boundedTail = Math.max(1, Math.min(5000, Number(tail) || 300));
    setTail(boundedTail);
    void onRefresh(value.server, value.container, boundedTail);
  }

  return (
    <Dialog
      open={value.open}
      title={value.container ? `${value.container.name || value.container.id} logs` : "Container logs"}
      description={value.server ? `${value.server.name} · Docker logs` : "Latest Docker logs from the selected server."}
      onClose={onClose}
      size="wide"
      className="h-[calc(100vh-120px)] grid-rows-[auto_minmax(0,1fr)]"
      bodyClassName="min-h-0 overflow-hidden"
    >
      <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-3">
        <form className="flex min-h-10 flex-wrap items-center justify-between gap-2 rounded-md border border-stone-200 bg-stone-50 px-3 py-2 text-xs text-stone-500" onSubmit={refreshLogs}>
          <span className="min-w-0 flex-1 truncate">
            {value.state === "loading" ? "Loading logs..." : null}
            {value.state === "error" ? value.error : null}
            {value.state !== "loading" && value.data ? `exit ${value.data.exit_code} · ${value.data.duration_ms}ms${value.data.ok ? "" : " · command failed"}` : null}
            {value.state === "idle" ? "No log request yet." : null}
          </span>
          <div className="flex items-center gap-2">
            <label className="flex items-center gap-2">
              <span>Tail</span>
              <Input
                type="number"
                min="1"
                max="5000"
                step="1"
                value={tail}
                onChange={(event) => setTail(event.target.value)}
                className="h-8 w-24 px-2 text-xs"
              />
            </label>
            <Button type="submit" variant="outline" className="h-8 px-2" disabled={!canRefresh}>
              <RefreshCcw className="h-3.5 w-3.5" />
              Refresh
            </Button>
            <CopyButton value={output || ""} variant="outline" className="h-8 px-2" iconClassName="h-3.5 w-3.5" />
          </div>
        </form>
        <TerminalBlock ref={outputRef} surface="log">
          {output || (value.state === "ready" ? "No logs returned." : "")}
        </TerminalBlock>
      </div>
    </Dialog>
  );
}

function DockerDetailField({ label, value, mono = false, wide = false }) {
  return (
    <div className={`min-w-0 rounded-md border border-stone-200 bg-stone-50 p-3 ${wide ? "sm:col-span-2 xl:col-span-3" : ""}`}>
      <p className="text-xs font-semibold uppercase text-stone-500">{label}</p>
      <p className={`mt-1 break-words text-sm text-stone-900 ${mono ? "font-mono text-xs leading-5" : "font-medium"}`}>{value || "-"}</p>
    </div>
  );
}

function SSHConfigDialog({ value, hasSSHKeys, onScan, onParseFile, content, resultSource, onContentChange, onParseContent, onUse, onClose }) {
  const [activeTab, setActiveTab] = useState("local");
  const tabs = [
    ["local", "Import from this computer"],
    ["container", "Container config"],
  ];
  const showCurrentResult = value.state === "ready" && resultSource === activeTab;

  return (
    <Dialog
      open={value.open}
      title="Import SSH hosts"
      description="Read host, user, and port entries, then choose one to prefill the server form."
      onClose={onClose}
      size="lg"
    >
      <div className="grid gap-4">
        <Notice>
          AIPermission imports host metadata only: alias, hostname, user, port, and identity file path. It does not
          import private keys from this step. Import existing private keys from the SSH Keys page.
        </Notice>

        <div className="grid rounded-md border border-stone-200 bg-stone-100 p-1 dark-soft-panel" style={{ gridTemplateColumns: `repeat(${tabs.length}, minmax(0, 1fr))` }}>
          {tabs.map(([tab, label]) => (
            <button
              key={tab}
              type="button"
              className={`rounded px-3 py-2 text-sm font-semibold transition ${
                activeTab === tab ? "bg-white text-stone-950 shadow-sm dark-card-surface" : "text-stone-500 hover:text-stone-900"
              }`}
              onClick={() => setActiveTab(tab)}
            >
              {label}
            </button>
          ))}
        </div>

        {activeTab === "local" ? (
          <div className="grid gap-4">
            <Notice tone="good">
              On Linux and macOS this is usually <span className="font-mono">~/.ssh/config</span>. On Windows, choose the
              OpenSSH config file from your user profile, for example{" "}
              <span className="font-mono">C:\Users\you\.ssh\config</span>. If the file picker does not show the file,
              paste the config content below.
            </Notice>
            <div className="grid gap-2">
              <Field>
                Paste host config
                <Textarea
                  value={content}
                  onChange={(event) => onContentChange(event.target.value)}
                  rows={5}
                  className="font-mono text-xs"
                  placeholder={"Host worker-1\n  HostName 203.0.113.10\n  User root\n  IdentityFile ~/.ssh/id_ed25519"}
                />
              </Field>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <Button type="button" asChild>
                  <label>
                    <Upload className="h-4 w-4" />
                    Choose host config file
                    <input className="hidden" type="file" onChange={onParseFile} />
                  </label>
                </Button>
                <Button type="button" variant="outline" onClick={onParseContent} disabled={value.state === "loading" || !content.trim()}>
                  Parse pasted hosts
                </Button>
              </div>
            </div>
          </div>
        ) : (
          <div className="grid gap-3">
            <Notice tone="warn">
              This scans the gateway process user's <span className="font-mono">~/.ssh/config</span>. In Docker, that
              means the container filesystem, not your workstation. Use this only if you mounted an SSH config into the
              container or run the gateway natively on your machine.
            </Notice>
            <div className="flex justify-end">
              <Button type="button" variant="outline" onClick={onScan} disabled={value.state === "loading"}>
                <RefreshCcw className="h-4 w-4" />
                {value.state === "loading" ? "Scanning..." : "Scan container config"}
              </Button>
            </div>
          </div>
        )}

        {!hasSSHKeys ? <Notice tone="warn">Create or import an SSH key before saving discovered servers.</Notice> : null}
        {value.state === "error" ? <Notice tone="bad">{value.error}</Notice> : null}
        {showCurrentResult && value.items.length === 0 ? (
          <Notice>
            No server entries were found. Choose your host config file, paste its content above, or add a server manually.
            Blocks like <span className="font-mono">Host *</span> are only defaults, so they are not shown as server
            entries.
          </Notice>
        ) : null}
        {showCurrentResult && value.items.length > 0 ? (
          <div className="max-h-[420px] overflow-y-auto rounded-md border border-stone-200">
            <table className="w-full table-fixed border-collapse text-left text-sm">
              <thead className="sticky top-0 bg-stone-50 text-xs uppercase text-stone-500">
                <tr>
                  <th className="w-[22%] px-3 py-2 font-semibold">Alias</th>
                  <th className="w-[25%] px-3 py-2 font-semibold">Host</th>
                  <th className="w-[18%] px-3 py-2 font-semibold">User</th>
                  <th className="w-[23%] px-3 py-2 font-semibold">Identity</th>
                  <th className="w-[12%] px-3 py-2 text-right font-semibold">Action</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-stone-200">
                {value.items.map((item, index) => (
                  <tr key={`${item.alias}-${index}`} className="align-top">
                    <td className="px-3 py-3 font-semibold">{item.alias}</td>
                    <td className="px-3 py-3">
                      <span className="block truncate font-mono text-xs">
                        {item.host}:{item.port || 22}
                      </span>
                      {item.proxy_jump || item.proxy_command_configured ? (
                        <span className="mt-1 block truncate text-xs text-stone-500">
                          {item.proxy_jump ? `ProxyJump ${item.proxy_jump}` : "ProxyCommand configured"}
                        </span>
                      ) : null}
                    </td>
                    <td className="px-3 py-3">{item.username || <span className="text-stone-400">root default</span>}</td>
                    <td className="px-3 py-3">
                      {item.identity_file ? (
                        <span className="block truncate font-mono text-xs text-stone-500">{item.identity_file}</span>
                      ) : (
                        <span className="text-stone-400">None</span>
                      )}
                      {item.warnings?.length ? <span className="mt-1 block text-xs text-amber-800">{item.warnings[0]}</span> : null}
                    </td>
                    <td className="px-3 py-3 text-right">
                      <Button type="button" variant="outline" className="h-8 px-3" onClick={() => onUse(item)} disabled={!hasSSHKeys}>
                        Use
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </div>
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
      <TerminalBlock className="max-h-44 max-w-full whitespace-pre p-3">{command}</TerminalBlock>
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

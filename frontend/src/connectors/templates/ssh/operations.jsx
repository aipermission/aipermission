import { FileText, Info, RefreshCcw, ShieldCheck } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Dialog } from "../../../components/ui/dialog";
import { Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { apiPost } from "../../../lib/api";
import { InstallCommandPanel } from "../common";
import * as model from "./model";

export function SSHConnectorOperationsTemplate({ value, credentials, onChange, onHostKeyActionComplete }) {
  const operation = value?.connector_kind === "ssh" ? value : { open: false };

  useEffect(() => {
    if (operation.open && operation.type === "docker-check" && (!operation.state || operation.state === "idle")) {
      void checkDocker(operation.target, operation.profile);
    }
  }, [operation.open, operation.type, operation.state, operation.target?.id, operation.profile?.id]);

  function close() {
    onChange({ open: false, connector_kind: "", type: "", state: "idle", error: null });
  }

  async function checkDocker(target = operation.target, profile = operation.profile) {
    if (!target || !profile) return;
    onChange({ open: true, connector_kind: "ssh", type: "docker-check", target, profile, state: "loading", data: null, error: null });
    try {
      const data = await model.checkDocker({ target, profile });
      onChange({ open: true, connector_kind: "ssh", type: "docker-check", target, profile, state: "ready", data, error: null });
    } catch (error) {
      const action = model.hostKeyActionFromError(error, { operation: "docker-check", target, profile });
      if (action) {
        onChange({ open: true, connector_kind: "ssh", type: "host-key", hostKey: error.data.host_key, action, state: "idle", error: null });
        return;
      }
      onChange({ open: true, connector_kind: "ssh", type: "docker-check", target, profile, state: "error", data: null, error: error.message });
    }
  }

  async function readDockerLogs(target, container, tail = 300, profile = operation.profile) {
    if (!target || !profile || !container) return;
    onChange((current) => ({
      open: true,
      connector_kind: "ssh",
      type: "docker-logs",
      target,
      profile,
      container,
      state: "loading",
      data: current?.open && (current.container?.id || current.container?.name) === (container.id || container.name) ? current.data : null,
      error: null,
    }));
    try {
      const data = await model.readDockerLogs({ target, profile, container, tail });
      onChange({ open: true, connector_kind: "ssh", type: "docker-logs", target, profile, container, state: "ready", data, error: null });
    } catch (error) {
      const action = model.hostKeyActionFromError(error, { operation: "docker-logs", target, profile, container });
      if (action) {
        onChange({ open: true, connector_kind: "ssh", type: "host-key", hostKey: error.data.host_key, action, state: "idle", error: null });
        return;
      }
      onChange((current) => ({ open: true, connector_kind: "ssh", type: "docker-logs", target, profile, container, state: "error", data: current?.data, error: error.message }));
    }
  }

  async function approveHostKey() {
    const { hostKey, action } = operation;
    if (!hostKey || !action) return;
    onChange((current) => ({ ...current, state: "approving", error: null }));
    try {
      await apiPost("/api/ssh-host-keys/approve", {
        host: hostKey.host,
        port: hostKey.port,
        public_key: hostKey.public_key,
        replace: Boolean(hostKey.changed),
      });
      if (action.type === "docker-check") {
        await checkDocker(action.target, action.profile);
      } else if (action.type === "docker-logs") {
        await readDockerLogs(action.target, action.container, undefined, action.profile);
      } else {
        const result = await model.resumeHostKeyAction(action);
        await onHostKeyActionComplete?.(result, action);
        close();
      }
    } catch (error) {
      onChange((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  return (
    <>
      <ServerInstallDialog value={operation.type === "install" ? operation : { open: false }} credentials={credentials} onClose={close} />
      <HostKeyApprovalDialog value={operation.type === "host-key" ? operation : { open: false }} onApprove={approveHostKey} onClose={close} />
      <DockerCheckDialog value={operation.type === "docker-check" ? operation : { open: false }} onReadLogs={readDockerLogs} onClose={close} />
      <DockerLogsDialog value={operation.type === "docker-logs" ? operation : { open: false }} onRefresh={readDockerLogs} onClose={close} />
    </>
  );
}

function HostKeyApprovalDialog({ value, onApprove, onClose }) {
  const hostKey = value.hostKey;
  const changed = Boolean(hostKey?.changed);
  return (
    <Dialog
      open={value.open}
      title={changed ? "SSH host fingerprint changed" : "Approve SSH host fingerprint"}
      description={changed ? "The target is sending a different identity than the one previously trusted." : "First SSH connection requires you to trust the target identity."}
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
              Verify this fingerprint from your provider console or from a trusted terminal before approving. AIPermission will reject future changes for this SSH host.
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

function ServerInstallDialog({ value, credentials, onClose }) {
  const target = value.target;
  const profile = value.profile;
  const keyID = profile?.public?.ssh_key_id;
  const key = credentials.find((item) => Number(item.id) === Number(keyID)) || null;
  const username = profile?.public?.username || target?.config?.username || "ssh";
  const host = target?.config?.host || "host";
  const port = target?.config?.port || 22;

  return (
    <Dialog
      open={value.open}
      title={target ? `Install key for ${target.name}` : "Install SSH key"}
      description="Paste this command on the remote target before connecting with aipermission."
      onClose={onClose}
      size="md"
    >
      {target && key ? (
        <InstallCommandPanel
          command={key.install_command}
          title={`${username}@${host}:${port}`}
          description="Copy the command, connect to the target with your own terminal, paste it, then test the connector from aipermission."
        />
      ) : (
        <Notice tone="bad">SSH credential details are not loaded.</Notice>
      )}
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
        title={value.target ? `${value.target.name} Docker` : "Docker check"}
        description="On-demand Docker status from the selected target."
        onClose={onClose}
        size="xl"
      >
        <div className="grid gap-4">
          {value.state === "loading" ? <Notice>Checking Docker on the target...</Notice> : null}
          {value.state === "error" ? <Notice tone="bad">{value.error}</Notice> : null}
          {data && !data.available ? <Notice tone="warn">Docker is not installed or the docker command is not available on this target.</Notice> : null}
          {data?.available && !data.ok ? (
            <Notice tone="bad">
              Docker is available, but the status command failed. Check Docker daemon access, permissions, or service state on the target.
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
                              <Button type="button" variant="outline" className="h-8 w-8 px-0" title="Details" onClick={() => setDetailContainer(container)}>
                                <Info className="h-4 w-4" />
                              </Button>
                              <Button type="button" variant="outline" className="h-8 w-8 px-0" title="Logs" onClick={() => onReadLogs(value.target, container, undefined, value.profile)}>
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
  const canRefresh = Boolean(value.target && value.container) && value.state !== "loading";

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
    void onRefresh(value.target, value.container, boundedTail, value.profile);
  }

  return (
    <Dialog
      open={value.open}
      title={value.container ? `${value.container.name || value.container.id} logs` : "Container logs"}
      description={value.target ? `${value.target.name} · Docker logs` : "Latest Docker logs from the selected target."}
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

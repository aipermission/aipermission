import { useEffect, useRef, useState } from "react";
import { CloudDownload, RefreshCw } from "lucide-react";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { apiPost } from "../lib/api";
import { formatLocalTimestamp, formatRelativeAge } from "../lib/date-time";
import { formatBytes } from "../lib/file-transfer-utils";
import {
  groupBackupVersions,
  remoteCredentialFingerprint,
  remoteRequestIsCurrent,
  shortBackupSourceID,
  shortBackupStreamID,
} from "./remote-restore-helpers";

export function RemoteRestorePanel({ onUnlocked }) {
  const [form, setForm] = useState({ base_url: "", token: "", database_name: "", database_password: "" });
  const [state, setState] = useState({ state: "idle", error: null });
  const [streams, setStreams] = useState([]);
  const [selectedStreamID, setSelectedStreamID] = useState("");
  const [versions, setVersions] = useState([]);
  const [selectedBackupID, setSelectedBackupID] = useState("");
  const [versionCredentialFingerprint, setVersionCredentialFingerprint] = useState("");
  const requestGeneration = useRef(0);
  const formRef = useRef(form);

  useEffect(
    () => () => {
      requestGeneration.current += 1;
    },
    [],
  );

  function updateField(field, value) {
    const nextForm = { ...formRef.current, [field]: value };
    formRef.current = nextForm;
    setForm(nextForm);
    if (field === "base_url" || field === "token") {
      requestGeneration.current += 1;
      const resetForm = { ...nextForm, database_name: "" };
      formRef.current = resetForm;
      setForm(resetForm);
      setStreams([]);
      setSelectedStreamID("");
      setVersions([]);
      setSelectedBackupID("");
      setVersionCredentialFingerprint("");
      setState({ state: "idle", error: null });
    }
  }

  async function loadVersions(streamID, credentials = formRef.current, databaseName = "", generation = ++requestGeneration.current) {
    const fingerprint = remoteCredentialFingerprint(credentials);
    setState({ state: "loading_versions", error: null });
    setSelectedStreamID(streamID);
    if (databaseName) {
      const nextForm = { ...formRef.current, database_name: databaseName };
      formRef.current = nextForm;
      setForm(nextForm);
    }
    setSelectedBackupID("");
    setVersions([]);
    setVersionCredentialFingerprint("");
    try {
      const response = await apiPost("/api/backup/remote/list", {
        base_url: credentials.base_url,
        token: credentials.token,
        stream_id: streamID,
      });
      if (!remoteRequestIsCurrent(requestGeneration, generation, formRef, fingerprint)) return;
      const nextVersions = response?.items?.[0]?.backups || [];
      setVersions(nextVersions);
      setSelectedBackupID(nextVersions[0]?.id || "");
      setVersionCredentialFingerprint(fingerprint);
      setState({ state: "ready", error: null });
    } catch (error) {
      if (!remoteRequestIsCurrent(requestGeneration, generation, formRef, fingerprint)) return;
      setState({ state: "error", error: error.message });
    }
  }

  async function connectService(event) {
    event.preventDefault();
    const credentials = { ...formRef.current };
    const fingerprint = remoteCredentialFingerprint(credentials);
    const generation = ++requestGeneration.current;
    setState({ state: "connecting", error: null });
    setStreams([]);
    setSelectedStreamID("");
    setVersions([]);
    setSelectedBackupID("");
    setVersionCredentialFingerprint("");
    try {
      const response = await apiPost("/api/backup/remote/list", {
        base_url: credentials.base_url,
        token: credentials.token,
      });
      if (!remoteRequestIsCurrent(requestGeneration, generation, formRef, fingerprint)) return;
      const nextStreams = response?.items || [];
      setStreams(nextStreams);
      if (nextStreams.length === 0) {
        setState({ state: "ready", error: null });
        return;
      }
      await loadVersions(nextStreams[0].id, credentials, nextStreams[0].database_name, generation);
    } catch (error) {
      if (!remoteRequestIsCurrent(requestGeneration, generation, formRef, fingerprint)) return;
      setState({ state: "error", error: error.message });
    }
  }

  async function restoreRemoteBackup(event) {
    event.preventDefault();
    const credentials = { ...formRef.current };
    const fingerprint = remoteCredentialFingerprint(credentials);
    if (!selectedStreamID || !selectedBackupID || versionCredentialFingerprint !== fingerprint) return;
    const generation = ++requestGeneration.current;
    setState({ state: "restoring", error: null });
    try {
      await apiPost("/api/backup/remote/restore", {
        base_url: credentials.base_url,
        token: credentials.token,
        stream_id: selectedStreamID,
        backup_id: selectedBackupID,
        database_name: form.database_name,
        database_password: form.database_password,
      });
      if (remoteRequestIsCurrent(requestGeneration, generation, formRef, fingerprint)) {
        const emptyForm = { base_url: "", token: "", database_name: "", database_password: "" };
        formRef.current = emptyForm;
        setForm(emptyForm);
      }
      await onUnlocked();
    } catch (error) {
      if (!remoteRequestIsCurrent(requestGeneration, generation, formRef, fingerprint)) return;
      setState({ state: "error", error: error.message });
    }
  }

  const selectedStream = streams.find((stream) => stream.id === selectedStreamID);
  const selectedVersion = versions.find((version) => version.id === selectedBackupID);
  const versionGroups = groupBackupVersions(versions);

  return (
    <div className="grid gap-4">
      <div>
        <h2 className="text-sm font-semibold text-stone-900">Restore from AIPermission Backup</h2>
        <p className="mt-1 text-sm text-stone-500">
          Connect temporarily, choose a database stream and immutable version, then unlock the restored local copy.
        </p>
      </div>
      <Notice>
        The service stores encrypted <code>.aipdb</code> bytes only. Its token is used for this restore request and is not saved in browser
        storage or a local database.
      </Notice>
      <RemoteServiceForm form={form} state={state.state} hasStreams={streams.length > 0} onChange={updateField} onSubmit={connectService} />

      {streams.length > 0 ? (
        <form className="grid gap-4 border-t border-stone-200 pt-4" onSubmit={restoreRemoteBackup}>
          <div className="grid gap-2 sm:grid-cols-2">
            <label className="grid gap-2 text-sm font-semibold text-stone-800">
              Database stream
              <select
                className="h-10 rounded-md border border-stone-300 bg-white px-3 text-sm font-normal outline-none focus:border-emerald-800"
                value={selectedStreamID}
                onChange={(event) => {
                  const stream = streams.find((item) => item.id === event.target.value);
                  void loadVersions(event.target.value, form, stream?.database_name || "");
                }}
                disabled={state.state === "loading_versions" || state.state === "restoring"}
              >
                {streams.map((stream) => (
                  <option key={stream.id} value={stream.id}>
                    {stream.database_name} · {shortBackupStreamID(stream.id)}
                  </option>
                ))}
              </select>
            </label>
            <label className="grid gap-2 text-sm font-semibold text-stone-800">
              Backup version
              <select
                className="h-10 rounded-md border border-stone-300 bg-white px-3 text-sm font-normal outline-none focus:border-emerald-800"
                value={selectedBackupID}
                onChange={(event) => setSelectedBackupID(event.target.value)}
                disabled={state.state === "loading_versions" || state.state === "restoring" || versions.length === 0}
              >
                {versions.length === 0 ? <option value="">No backups available</option> : null}
                {versionGroups.map((group) => (
                  <optgroup key={group.source} label={`Source ${shortBackupSourceID(group.source)}`}>
                    {group.items.map((version) => (
                      <option key={version.id} value={version.id}>
                        {formatRelativeAge(version.created_at)} · {formatLocalTimestamp(version.created_at)} ·{" "}
                        {formatBytes(version.size_bytes)}
                      </option>
                    ))}
                  </optgroup>
                ))}
              </select>
            </label>
          </div>
          {selectedVersion ? <RemoteBackupDetails version={selectedVersion} /> : null}
          {selectedStream ? (
            <div className="grid gap-2">
              <label htmlFor="remote-backup-database-name" className="text-sm font-semibold text-stone-800">
                New local database name
              </label>
              <Input
                id="remote-backup-database-name"
                value={form.database_name}
                onChange={(event) => updateField("database_name", event.target.value)}
                required
              />
              <p className="text-xs text-stone-500">
                Remote stream {shortBackupStreamID(selectedStream.id)} remains unchanged; this name is only for the restored local copy.
              </p>
            </div>
          ) : null}
          <div className="grid gap-2">
            <label htmlFor="remote-backup-database-password" className="text-sm font-semibold text-stone-800">
              Backup database password
            </label>
            <Input
              id="remote-backup-database-password"
              type="password"
              value={form.database_password}
              onChange={(event) => updateField("database_password", event.target.value)}
              autoComplete="current-password"
              required
            />
          </div>
          <Button
            type="submit"
            disabled={
              !selectedBackupID ||
              !form.database_name.trim() ||
              !form.database_password ||
              state.state === "restoring" ||
              state.state === "loading_versions" ||
              versionCredentialFingerprint !== remoteCredentialFingerprint(form)
            }
          >
            <CloudDownload className="h-4 w-4" />
            {state.state === "restoring" ? "Restoring..." : "Restore encrypted database"}
          </Button>
        </form>
      ) : state.state === "ready" ? (
        <Notice>No backup streams were found for this service token.</Notice>
      ) : null}
      {state.state === "loading_versions" ? <Notice>Loading immutable backup versions...</Notice> : null}
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
    </div>
  );
}

function RemoteServiceForm({ form, state, hasStreams, onChange, onSubmit }) {
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      <div className="grid gap-2">
        <label htmlFor="remote-backup-service-url" className="text-sm font-semibold text-stone-800">
          Backup service URL
        </label>
        <Input
          id="remote-backup-service-url"
          type="url"
          value={form.base_url}
          onChange={(event) => onChange("base_url", event.target.value)}
          placeholder="https://backups.example.com"
          autoComplete="off"
          required
        />
      </div>
      <div className="grid gap-2">
        <label htmlFor="remote-backup-service-token" className="text-sm font-semibold text-stone-800">
          Service token
        </label>
        <Input
          id="remote-backup-service-token"
          type="password"
          value={form.token}
          onChange={(event) => onChange("token", event.target.value)}
          autoComplete="off"
          required
        />
      </div>
      <Button type="submit" variant="outline" disabled={state === "connecting" || state === "loading_versions" || state === "restoring"}>
        <RefreshCw className={`h-4 w-4 ${state === "connecting" ? "animate-spin" : ""}`} />
        {state === "connecting" ? "Connecting..." : hasStreams ? "Refresh remote backups" : "Connect and list backups"}
      </Button>
    </form>
  );
}

function RemoteBackupDetails({ version }) {
  return (
    <div className="grid gap-1 rounded-md border border-stone-200 bg-stone-50 px-3 py-2 text-xs text-stone-600 sm:grid-cols-2">
      <span className="truncate">
        <strong className="text-stone-900">File:</strong> {version.filename}
      </span>
      <span>
        <strong className="text-stone-900">Created:</strong> {formatLocalTimestamp(version.created_at)}
      </span>
      <span className="truncate">
        <strong className="text-stone-900">Source:</strong> {shortBackupSourceID(version.source_installation_id)}
      </span>
      <span>
        <strong className="text-stone-900">Size:</strong> {formatBytes(version.size_bytes)}
      </span>
    </div>
  );
}

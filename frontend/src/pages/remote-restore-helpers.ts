type RemoteCredentials = { base_url?: string; token?: string };
type BackupVersion = { source_installation_id?: string | null };

export function remoteCredentialFingerprint(credentials: RemoteCredentials | null | undefined) {
  return JSON.stringify([String(credentials?.base_url || "").trim(), String(credentials?.token || "")]);
}

export function remoteRequestIsCurrent(
  generationRef: { current: number },
  generation: number,
  formRef: { current: RemoteCredentials | null },
  fingerprint: string,
) {
  return generationRef.current === generation && remoteCredentialFingerprint(formRef.current) === fingerprint;
}

export function shortBackupStreamID(value: unknown) {
  const text = String(value || "");
  return text.length > 12 ? `${text.slice(0, 8)}...${text.slice(-4)}` : text;
}

export function shortBackupSourceID(value: unknown) {
  const text = String(value || "unknown installation");
  return text.length > 18 ? `${text.slice(0, 10)}...${text.slice(-6)}` : text;
}

export function groupBackupVersions<T extends BackupVersion>(versions: T[]) {
  const groups = new Map<string, T[]>();
  for (const version of versions) {
    const source = version.source_installation_id || "unknown installation";
    const group = groups.get(source) || [];
    group.push(version);
    groups.set(source, group);
  }
  return [...groups.entries()].map(([source, items]) => ({ source, items }));
}

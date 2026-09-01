export function remoteCredentialFingerprint(credentials) {
  return JSON.stringify([String(credentials?.base_url || "").trim(), String(credentials?.token || "")]);
}

export function remoteRequestIsCurrent(generationRef, generation, formRef, fingerprint) {
  return generationRef.current === generation && remoteCredentialFingerprint(formRef.current) === fingerprint;
}

export function shortBackupStreamID(value) {
  const text = String(value || "");
  return text.length > 12 ? `${text.slice(0, 8)}...${text.slice(-4)}` : text;
}

export function shortBackupSourceID(value) {
  const text = String(value || "unknown installation");
  return text.length > 18 ? `${text.slice(0, 10)}...${text.slice(-6)}` : text;
}

export function groupBackupVersions(versions) {
  const groups = new Map();
  for (const version of versions) {
    const source = version.source_installation_id || "unknown installation";
    if (!groups.has(source)) groups.set(source, []);
    groups.get(source).push(version);
  }
  return [...groups.entries()].map(([source, items]) => ({ source, items }));
}

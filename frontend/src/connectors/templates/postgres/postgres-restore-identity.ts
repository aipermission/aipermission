import { fileSHA256 } from "../../../lib/file-digest";

export async function postgresRestoreRetryIdentity(path: string, confirmTarget: string, file: File, signal?: AbortSignal) {
  return {
    path,
    body: {
      confirm_target: confirmTarget,
      filename: file.name,
      size: file.size,
      artifact_sha256: await fileSHA256(file, signal),
    },
  };
}

import { fileSHA256 } from "../../../lib/file-digest";

export async function postgresRestoreRetryIdentity(path, confirmTarget, file, signal) {
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

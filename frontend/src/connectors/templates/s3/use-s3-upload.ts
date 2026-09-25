import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { defaultUploadDialog, type S3UploadDialogState, type S3UploadFile } from "./dialogs";
import { fileToBase64, joinObjectKey, normalizeObjectKey } from "./helpers";

const maxInlineUploadBytes = 16 << 20;

type UploadRequest = ReturnType<ReturnType<typeof useRequestGuard>["begin"]>;
type S3UploadOptions = {
  scopeKey: string;
  active: boolean;
  prefix: string;
  runAction: (_request: {
    actionName: "upload_object";
    input: { key: string; content_base64?: string; content_text?: string; content_type: string; overwrite: boolean };
    reason: string;
    busy: string;
  }) => Promise<object | null>;
  refreshObjects: (_options: { reset: boolean }) => Promise<unknown>;
  readObjectMetadata: (_key: string) => Promise<unknown>;
  setState: (_state: { state: string; error: string; message: string }) => void;
};

export function useS3Upload({ scopeKey, active, prefix, runAction, refreshObjects, readObjectMetadata, setState }: S3UploadOptions) {
  const [uploadDialog, setUploadDialog] = useState<S3UploadDialogState>(defaultUploadDialog);
  const requests = useRequestGuard(`s3-upload:${scopeKey}`);

  useEffect(() => setUploadDialog(defaultUploadDialog), [scopeKey]);

  function openUploadDialog() {
    setUploadDialog({ ...defaultUploadDialog, open: true, prefix: prefix || "", textKey: prefix || "" });
  }

  function closeUploadDialog() {
    setUploadDialog((current) => (current.pending ? current : defaultUploadDialog));
  }

  function addUploadFiles(fileList: FileList | null) {
    const files = Array.from(fileList || []);
    if (files.length === 0) return;
    setUploadDialog((current) => ({
      ...current,
      error: "",
      files: [
        ...current.files,
        ...files.map((file) => ({
          id: `${file.name}-${file.size}-${file.lastModified}-${Math.random().toString(36).slice(2)}`,
          file,
          key: joinObjectKey(current.prefix, file.name),
          contentType: file.type || "application/octet-stream",
        })),
      ],
    }));
  }

  function removeUploadFile(id: string) {
    setUploadDialog((current) => ({ ...current, files: current.files.filter((item) => item.id !== id) }));
  }

  function updateUploadFile(id: string, patch: Partial<S3UploadFile>) {
    setUploadDialog((current) => ({
      ...current,
      files: current.files.map((item) => (item.id === id ? { ...item, ...patch } : item)),
    }));
  }

  async function uploadObjects(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!active || uploadDialog.pending) return;
    const preparedFiles = uploadDialog.files.map((item) => ({ ...item, key: normalizeObjectKey(item.key) })).filter((item) => item.key);
    const textKey = normalizeObjectKey(uploadDialog.textKey);
    const fileMode = uploadDialog.mode !== "text";
    const includeText = Boolean(uploadDialog.mode === "text" && textKey && uploadDialog.textContent);
    const validationError = uploadValidationError({ fileMode, preparedFiles, includeText });
    if (validationError) {
      setUploadDialog((current) => ({ ...current, error: validationError }));
      return;
    }
    const request = requests.begin("upload");
    setUploadDialog((current) => ({ ...current, pending: true, error: "", message: "" }));
    try {
      const lastKey = fileMode ? await uploadFiles(preparedFiles, request) : await uploadText(textKey, request);
      if (!request.isCurrent()) return;
      if (!lastKey) {
        setUploadDialog((current) => ({ ...current, pending: false }));
        return;
      }
      setUploadDialog(defaultUploadDialog);
      await refreshObjects({ reset: true });
      if (!request.isCurrent()) return;
      await readObjectMetadata(lastKey);
      if (!request.isCurrent()) return;
      setState({ state: "idle", error: "", message: `Uploaded ${fileMode ? preparedFiles.length : 1} object(s).` });
    } catch (error) {
      if (!request.isCurrent()) return;
      setUploadDialog((current) => ({ ...current, pending: false, error: error instanceof Error ? error.message : "Upload failed." }));
    } finally {
      request.complete();
    }
  }

  async function uploadFiles(preparedFiles: S3UploadFile[], request: UploadRequest) {
    let lastKey = "";
    for (const item of preparedFiles) {
      const contentBase64 = await fileToBase64(item.file, { signal: request.signal });
      if (!request.isCurrent()) return "";
      const uploaded = await runAction({
        actionName: "upload_object",
        input: {
          key: item.key,
          content_base64: contentBase64,
          content_type: item.contentType || item.file.type || "application/octet-stream",
          overwrite: uploadDialog.overwrite,
        },
        reason: "manual S3 browser object upload",
        busy: "uploading",
      });
      if (!request.isCurrent() || !uploaded) return "";
      setUploadDialog((current) => ({ ...current, files: current.files.filter((candidate) => candidate.id !== item.id) }));
      lastKey = item.key;
    }
    return lastKey;
  }

  async function uploadText(textKey: string, request: UploadRequest) {
    if (!request.isCurrent()) return "";
    const uploaded = await runAction({
      actionName: "upload_object",
      input: {
        key: textKey,
        content_text: uploadDialog.textContent,
        content_type: uploadDialog.textContentType || "text/plain",
        overwrite: uploadDialog.overwrite,
      },
      reason: "manual S3 browser object upload",
      busy: "uploading",
    });
    return request.isCurrent() && uploaded ? textKey : "";
  }

  return {
    uploadDialog,
    setUploadDialog,
    openUploadDialog,
    closeUploadDialog,
    addUploadFiles,
    removeUploadFile,
    updateUploadFile,
    uploadObjects,
  };
}

function uploadValidationError({
  fileMode,
  preparedFiles,
  includeText,
}: {
  fileMode: boolean;
  preparedFiles: S3UploadFile[];
  includeText: boolean;
}) {
  if (fileMode && preparedFiles.length === 0) return "Choose one or more files to upload.";
  if (fileMode && preparedFiles.some((item) => item.file.size > maxInlineUploadBytes)) {
    return "Choose files no larger than 16 MiB.";
  }
  if (!fileMode && !includeText) return "Enter an object key and text content.";
  return "";
}

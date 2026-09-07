import { useEffect, useState } from "react";
import { defaultUploadDialog } from "./dialogs";
import { fileToBase64, joinObjectKey, normalizeObjectKey } from "./helpers";

export function useS3Upload({ scopeKey, active, prefix, runAction, refreshObjects, readObjectMetadata, setState }) {
  const [uploadDialog, setUploadDialog] = useState(defaultUploadDialog);

  useEffect(() => setUploadDialog(defaultUploadDialog), [scopeKey]);

  function openUploadDialog() {
    setUploadDialog({ ...defaultUploadDialog, open: true, prefix: prefix || "", textKey: prefix || "" });
  }

  function closeUploadDialog() {
    setUploadDialog((current) => (current.pending ? current : defaultUploadDialog));
  }

  function addUploadFiles(fileList) {
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

  function removeUploadFile(id) {
    setUploadDialog((current) => ({ ...current, files: current.files.filter((item) => item.id !== id) }));
  }

  function updateUploadFile(id, patch) {
    setUploadDialog((current) => ({
      ...current,
      files: current.files.map((item) => (item.id === id ? { ...item, ...patch } : item)),
    }));
  }

  async function uploadObjects(event) {
    event.preventDefault();
    if (!active || uploadDialog.pending) return;
    const preparedFiles = uploadDialog.files.map((item) => ({ ...item, key: normalizeObjectKey(item.key) })).filter((item) => item.key);
    const textKey = normalizeObjectKey(uploadDialog.textKey);
    const fileMode = uploadDialog.mode !== "text";
    const includeText = uploadDialog.mode === "text" && textKey && uploadDialog.textContent;
    const validationError = uploadValidationError({ fileMode, preparedFiles, includeText });
    if (validationError) {
      setUploadDialog((current) => ({ ...current, error: validationError }));
      return;
    }
    setUploadDialog((current) => ({ ...current, pending: true, error: "", message: "" }));
    try {
      const lastKey = fileMode ? await uploadFiles(preparedFiles) : await uploadText(textKey);
      if (!lastKey) {
        setUploadDialog((current) => ({ ...current, pending: false }));
        return;
      }
      setUploadDialog(defaultUploadDialog);
      await refreshObjects({ reset: true });
      await readObjectMetadata(lastKey);
      setState({ state: "idle", error: "", message: `Uploaded ${fileMode ? preparedFiles.length : 1} object(s).` });
    } catch (error) {
      setUploadDialog((current) => ({ ...current, pending: false, error: error.message || "Upload failed." }));
    }
  }

  async function uploadFiles(preparedFiles) {
    let lastKey = "";
    for (const item of preparedFiles) {
      const uploaded = await runAction({
        actionName: "upload_object",
        input: {
          key: item.key,
          content_base64: await fileToBase64(item.file),
          content_type: item.contentType || item.file.type || "application/octet-stream",
          overwrite: uploadDialog.overwrite,
        },
        reason: "manual S3 browser object upload",
        busy: "uploading",
      });
      if (!uploaded) return "";
      lastKey = item.key;
    }
    return lastKey;
  }

  async function uploadText(textKey) {
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
    return uploaded ? textKey : "";
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

function uploadValidationError({ fileMode, preparedFiles, includeText }) {
  if (fileMode && preparedFiles.length === 0) return "Choose one or more files to upload.";
  if (!fileMode && !includeText) return "Enter an object key and text content.";
  return "";
}

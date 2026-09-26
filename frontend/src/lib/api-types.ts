import type { PreparedRetry } from "./local-action-retry/records";

export type APIOptions = { signal?: AbortSignal; timeoutMs?: number; workspaceBinding?: string };
export type DownloadOptions = APIOptions & { picker?: boolean; requireStreaming?: boolean };
export type DownloadResult = { saved: boolean; method: "picker" | "anchor"; canceled?: boolean };
export type NativeFileWriter = WritableStream<Uint8Array> & {
  write: (_data: Blob) => Promise<void>;
  close: () => Promise<void>;
  abort: (_reason?: unknown) => Promise<void>;
};
export type NativeSaveHandle = { createWritable: () => Promise<NativeFileWriter> };
export type SaveFilePicker = (_options: { suggestedName: string }) => Promise<NativeSaveHandle>;
export type PostPolicy = {
  acknowledged?: (_data: unknown) => boolean;
  retireOnError?: (_error: unknown) => boolean;
  invalidResponseMessage: string;
};
export type PreparedPost = PostPolicy & { body: unknown; retry: PreparedRetry | null };

export function objectRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

export function nativeSaveFilePicker(): SaveFilePicker | null {
  if (typeof window === "undefined" || !("showSaveFilePicker" in window) || typeof window.showSaveFilePicker !== "function") return null;
  return window.showSaveFilePicker.bind(window) as SaveFilePicker;
}

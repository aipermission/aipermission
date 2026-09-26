import type { Dispatch, SetStateAction } from "react";
import { apiGet } from "./api";
import { errorMessage } from "./errors";
import type { createRequestGuard } from "./request-guard";

export type GatewayCollection<Item> = { state: string; data: Item[]; error: string | null };

export async function loadGatewayCollection<Item>({
  path,
  channel,
  guard,
  decode,
  setState,
}: {
  path: string;
  channel: string;
  guard: ReturnType<typeof createRequestGuard>;
  decode: (_response: unknown) => Item[];
  setState: Dispatch<SetStateAction<GatewayCollection<Item>>>;
}): Promise<Item[]> {
  const request = guard.begin(channel);
  setState((current) => ({ ...current, state: "loading", error: null }));
  try {
    const response = await apiGet(path, { signal: request.signal });
    if (!request.isCurrent()) return [];
    const items = decode(response);
    setState({ state: "ready", data: items, error: null });
    return items;
  } catch (error) {
    if (request.isCurrent()) setState({ state: "error", data: [], error: errorMessage(error) });
    return [];
  } finally {
    request.complete();
  }
}
